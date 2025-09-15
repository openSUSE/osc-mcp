package osc

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CommitCmd struct {
	Message     string `json:"message" jsonschema:"Commit message"`
	Directory   string `json:"directory" jsonschema:"Directory of the package to commit"`
	ProjectName string `json:"project_name" jsonschema:"(Optional) Project name. If not provided, it will be derived from the directory path."`
	PackageName string `json:"package_name" jsonschema:"(Optional) Package name. If not provided, it will be derived from the directory path."`
}

type CommitResult struct {
	Revision string `json:"revision"`
}

type Directory struct {
	XMLName xml.Name `xml:"directory"`
	Name    string   `xml:"name,attr"`
	Project string   `xml:"project,attr"`
	Entries []Entry  `xml:"entry"`
}

type Entry struct {
	XMLName xml.Name `xml:"entry"`
	Name    string   `xml:"name,attr"`
	Md5     string   `xml:"md5,attr"`
	Size    string   `xml:"size,attr"`
	Mtime   string   `xml:"mtime,attr"`
	Rev     string   `xml:"rev,attr"`
}

func (cred *OSCCredentials) Commit(ctx context.Context, req *mcp.CallToolRequest, params CommitCmd) (*mcp.CallToolResult, CommitResult, error) {
	slog.Debug("mcp tool call: Commit", "params", params)
	if params.Message == "" {
		return nil, CommitResult{}, fmt.Errorf("commit message must be specified")
	}
	if params.Directory == "" {
		return nil, CommitResult{}, fmt.Errorf("directory must be specified")
	}

	projectName := params.ProjectName
	packageName := params.PackageName
	if projectName == "" || packageName == "" {
		packageName = filepath.Base(params.Directory)
		projectName = filepath.Base(filepath.Dir(params.Directory))
	}
	if projectName == "" || packageName == "" {
		return nil, CommitResult{}, fmt.Errorf("could not determine project and package name from directory: %s", params.Directory)
	}
	slog.Info("Committing package", "project", projectName, "package", packageName)

	remoteFiles, err := cred.getRemoteFileList(ctx, projectName, packageName)
	if err != nil {
		return nil, CommitResult{}, fmt.Errorf("failed to get remote file list: %w", err)
	}
	remoteFileMap := make(map[string]Entry)
	for _, entry := range remoteFiles.Entries {
		remoteFileMap[entry.Name] = entry
	}

	localFiles, err := os.ReadDir(params.Directory)
	if err != nil {
		return nil, CommitResult{}, fmt.Errorf("failed to read local directory: %w", err)
	}

	var specFile string
	var changedFiles []string
	var newFiles []string
	localFileMap := make(map[string]bool)

	for _, file := range localFiles {
		if file.IsDir() {
			continue
		}
		fileName := file.Name()
		if strings.HasPrefix(fileName, ".") {
			continue // Ignore hidden files like .osc
		}
		localFileMap[fileName] = true
		filePath := filepath.Join(params.Directory, fileName)

		hash, err := fileMD5(filePath)
		if err != nil {
			return nil, CommitResult{}, fmt.Errorf("failed to calculate md5 for %s: %w", fileName, err)
		}

		remoteEntry, exists := remoteFileMap[fileName]
		if !exists {
			newFiles = append(newFiles, fileName)
			if strings.HasSuffix(fileName, ".spec") {
				specFile = fileName
			}
		} else if remoteEntry.Md5 != hash {
			changedFiles = append(changedFiles, fileName)
			if strings.HasSuffix(fileName, ".spec") {
				specFile = fileName
			}
		}
	}

	if specFile != "" {
		changesFile := strings.TrimSuffix(specFile, ".spec") + ".changes"
		slog.Info("Found changed .spec file, updating .changes file", "spec", specFile, "changes", changesFile)

		newEntry := createChangesEntry(params.Message, cred.Name, cred.Name+"@users.noreply.opensuse.org")

		changesFilePath := filepath.Join(params.Directory, changesFile)
		existingContent, err := os.ReadFile(changesFilePath)
		if err != nil && !os.IsNotExist(err) {
			return nil, CommitResult{}, fmt.Errorf("failed to read changes file: %w", err)
		}

		newContent := append([]byte(newEntry), existingContent...)
		if err := os.WriteFile(changesFilePath, newContent, 0644); err != nil {
			return nil, CommitResult{}, fmt.Errorf("failed to write to changes file: %w", err)
		}

		hash, err := fileMD5(changesFilePath)
		if err != nil {
			return nil, CommitResult{}, fmt.Errorf("failed to calculate md5 for %s: %w", changesFile, err)
		}

		isAlreadyListed := false
		for i, f := range newFiles {
			if f == changesFile {
				isAlreadyListed = true
				newFiles = append(newFiles[:i], newFiles[i+1:]...)
				break
			}
		}
		if !isAlreadyListed {
			for i, f := range changedFiles {
				if f == changesFile {
					isAlreadyListed = true
					changedFiles = append(changedFiles[:i], changedFiles[i+1:]...)
					break
				}
			}
		}

		remoteEntry, exists := remoteFileMap[changesFile]
		if !exists {
			newFiles = append(newFiles, changesFile)
		} else if remoteEntry.Md5 != hash {
			changedFiles = append(changedFiles, changesFile)
		} else {
			if !isAlreadyListed {
				changedFiles = append(changedFiles, changesFile)
			}
		}
	}

	filesToUpload := append(newFiles, changedFiles...)
	if len(filesToUpload) > 0 {
		slog.Info("Uploading changed files", "files", filesToUpload)
		for _, fileName := range filesToUpload {
			filePath := filepath.Join(params.Directory, fileName)
			err := cred.uploadFile(ctx, projectName, packageName, fileName, filePath)
			if err != nil {
				return nil, CommitResult{}, fmt.Errorf("failed to upload file %s: %w", fileName, err)
			}
		}
	} else {
		slog.Info("No changed files to upload")
	}

	allLocalFiles, err := os.ReadDir(params.Directory)
	if err != nil {
		return nil, CommitResult{}, fmt.Errorf("failed to re-read local directory: %w", err)
	}

	commitDir := Directory{
		Name:    packageName,
		Project: projectName,
	}
	for _, file := range allLocalFiles {
		if file.IsDir() {
			continue
		}
		fileName := file.Name()
		if strings.HasPrefix(fileName, ".") {
			continue
		}
		filePath := filepath.Join(params.Directory, fileName)
		info, err := file.Info()
		if err != nil {
			return nil, CommitResult{}, fmt.Errorf("failed to get file info for %s: %w", fileName, err)
		}
		hash, err := fileMD5(filePath)
		if err != nil {
			return nil, CommitResult{}, fmt.Errorf("failed to calculate md5 for %s: %w", fileName, err)
		}
		commitDir.Entries = append(commitDir.Entries, Entry{
			Name:  fileName,
			Md5:   hash,
			Size:  fmt.Sprintf("%d", info.Size()),
			Mtime: fmt.Sprintf("%d", info.ModTime().Unix()),
		})
	}

	xmlData, err := xml.MarshalIndent(commitDir, "", "  ")
	if err != nil {
		return nil, CommitResult{}, fmt.Errorf("failed to marshal commit xml: %w", err)
	}

	err = cred.commitFiles(ctx, projectName, packageName, params.Message, xmlData)
	if err != nil {
		return nil, CommitResult{}, fmt.Errorf("failed to commit changes: %w", err)
	}

	finalRemoteFiles, err := cred.getRemoteFileList(ctx, projectName, packageName)
	if err != nil {
		slog.Warn("failed to get remote file list after commit to determine new revision", "error", err)
		return nil, CommitResult{Revision: "unknown"}, nil
	}

	var revision string
	if len(finalRemoteFiles.Entries) > 0 {
		revision = finalRemoteFiles.Entries[0].Rev
	}

	return nil, CommitResult{Revision: revision}, nil
}

func (cred *OSCCredentials) getRemoteFileList(ctx context.Context, project, pkg string) (*Directory, error) {
	url := fmt.Sprintf("%s/source/%s/%s", cred.Apiaddr, project, pkg)
	req, err := cred.buildRequest(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return &Directory{}, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get remote file list: status %s, body: %s", resp.Status, string(body))
	}

	var dir Directory
	if err := xml.NewDecoder(resp.Body).Decode(&dir); err != nil {
		return nil, err
	}
	return &dir, nil
}

func fileMD5(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func (cred *OSCCredentials) uploadFile(ctx context.Context, project, pkg, fileName, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	url := fmt.Sprintf("%s/source/%s/%s/%s", cred.Apiaddr, project, pkg, fileName)
	req, err := cred.buildRequest(ctx, "PUT", url, file)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to upload file: status %s, body: %s", resp.Status, string(body))
	}
	return nil
}

func (cred *OSCCredentials) commitFiles(ctx context.Context, project, pkg, message string, xmlData []byte) error {
	escapedMessage := url.QueryEscape(message)
	url := fmt.Sprintf("%s/source/%s/%s?cmd=commit&comment=%s", cred.Apiaddr, project, pkg, escapedMessage)
	req, err := cred.buildRequest(ctx, "POST", url, bytes.NewReader(xmlData))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/xml")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to commit: status %s, body: %s", resp.Status, string(body))
	}
	return nil
}

func createChangesEntry(message, userName, userEmail string) string {
	var b strings.Builder
	b.WriteString("-------------------------------------------------------------------\n")
	b.WriteString(time.Now().UTC().Format("Mon Jan 02 15:04:05 MST 2006"))
	b.WriteString(fmt.Sprintf(" - %s <%s>\n\n", userName, userEmail))

	lines := strings.Split(message, "\n")
	for _, line := range lines {
		if trimmedLine := strings.TrimSpace(line); trimmedLine != "" {
			b.WriteString(fmt.Sprintf("- %s\n", trimmedLine))
		}
	}
	b.WriteString("\n")
	return b.String()
}