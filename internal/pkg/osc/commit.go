package osc

import (
	"bufio"
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
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/hbollon/go-edlib"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CommitCmd struct {
	Message             string   `json:"message" jsonschema:"Commit message"`
	AddedFiles          []string `json:"added_files,omitempty" jsonschema:"Files to add before committing"`
	RemovedFiles        []string `json:"removed_files,omitempty" jsonschema:"Files to remove before committing"`
	Directory           string   `json:"directory" jsonschema:"Directory of the package to commit"`
	ProjectName         string   `json:"project_name,omitempty" jsonschema:"Project name. If not provided, it will be derived from the directory path."`
	BundleName          string   `json:"bundle_name,omitempty" jsonschema:"Bundle name also known as source package name. If not provided, it will be derived from the directory path."`
	SkipChangesCreation bool     `json:"skip_changes,omitempty" jsonschema:"Skip the automatic update of the changes file."`
}

type CommitResult struct {
	Revision string `json:"revision"`
	Warning  string `json:"warning,omitempty"`
}

type Revision struct {
	XMLName xml.Name `xml:"revision"`
	Rev     string   `xml:"rev,attr"`
}

type LinkFile struct {
	XMLName xml.Name `xml:"link"`
	Project string   `xml:"project,attr"`
	BaseRev string   `xml:"baserev,attr"`
	Patches struct {
		XMLName xml.Name `xml:"patches"`
		Branch  struct {
			XMLName xml.Name `xml:"branch"`
		} `xml:"branch"`
	} `xml:"patches"`
}

type LinkInfo struct {
	XMLName xml.Name `xml:"linkinfo"`
	Project string   `xml:"project,attr"`
	Package string   `xml:"package,attr"`
	SrcMd5  string   `xml:"srcmd5,attr"`
	BaseRev string   `xml:"baserev,attr"`
	XSrcMd5 string   `xml:"xsrcmd5,attr,omitempty"`
	LSrcMd5 string   `xml:"lsrcmd5,attr,omitempty"`
}

type Directory struct {
	XMLName xml.Name  `xml:"directory"`
	Name    string    `xml:"name,attr"`
	Project string    `xml:"project,attr,omitempty"`
	Rev     string    `xml:"rev,attr,omitempty"`
	VRev    string    `xml:"vrev,attr,omitempty"`
	SrcMd5  string    `xml:"srcmd5,attr,omitempty"`
	Link    *LinkInfo `xml:"linkinfo,omitempty"`
	Entries []Entry   `xml:"entry"`
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
	slog.Debug("mcp tool call: Commit", "session", req.Session.ID(), "params", params)
	if params.Message == "" {
		return nil, CommitResult{}, fmt.Errorf("commit message must be specified")
	}
	if params.Directory == "" {
		return nil, CommitResult{}, fmt.Errorf("directory must be specified")
	}
	progressToken := req.Params.GetProgressToken()

	if !cred.useInternalCommit {
		baseCmdline := []string{"osc"}
		configFile, err := cred.writeTempOscConfig()
		if err != nil {
			slog.Warn("failed to write osc config", "error", err)
		} else {
			defer os.Remove(configFile)
			baseCmdline = append(baseCmdline, "--config", configFile)
		}

		statusCmdline := append(baseCmdline, "status")
		oscStatusCmd := exec.CommandContext(ctx, statusCmdline[0], statusCmdline[1:]...)
		oscStatusCmd.Dir = params.Directory
		slog.Debug("running osc status", slog.String("command", oscStatusCmd.String()), slog.String("dir", params.Directory))
		statusOutput, err := oscStatusCmd.CombinedOutput()
		if err != nil {
			slog.Error("failed to run osc status", slog.String("command", oscStatusCmd.String()), "error", err, "output", string(statusOutput))
			return nil, CommitResult{}, fmt.Errorf("failed to run osc status: %w\nOutput:\n%s", err, string(statusOutput))
		}
		slog.Debug("osc status finished successfully", slog.String("command", oscStatusCmd.String()), "output", string(statusOutput))

		var filesToAdd []string
		var filesToRemove []string
		statusScanner := bufio.NewScanner(bytes.NewReader(statusOutput))
		for statusScanner.Scan() {
			line := statusScanner.Text()
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}
			status := parts[0]
			fileName := strings.Join(parts[1:], " ")
			switch status {
			case "?":
				filesToAdd = append(filesToAdd, fileName)
			case "D":
				filesToRemove = append(filesToRemove, fileName)
			}
		}

		if len(filesToAdd) > 0 {
			addCmdline := append(baseCmdline, "add")
			addCmdline = append(addCmdline, filesToAdd...)
			oscAddCmd := exec.CommandContext(ctx, addCmdline[0], addCmdline[1:]...)
			oscAddCmd.Dir = params.Directory
			slog.Debug("running osc add", slog.String("command", oscAddCmd.String()), slog.String("dir", params.Directory))
			output, err := oscAddCmd.CombinedOutput()
			if err != nil {
				slog.Error("failed to run osc add", slog.String("command", oscAddCmd.String()), "error", err, "output", string(output))
				return nil, CommitResult{}, fmt.Errorf("failed to run osc add: %w\nOutput:\n%s", err, string(output))
			}
			slog.Debug("osc add finished successfully", slog.String("command", oscAddCmd.String()), "output", string(output))
		}

		if len(filesToRemove) > 0 {
			deleteCmdline := append(baseCmdline, "remove", "-f")
			deleteCmdline = append(deleteCmdline, filesToRemove...)
			oscDeleteCmd := exec.CommandContext(ctx, deleteCmdline[0], deleteCmdline[1:]...)
			oscDeleteCmd.Dir = params.Directory
			slog.Debug("running osc remove", slog.String("command", oscDeleteCmd.String()), slog.String("dir", params.Directory))
			output, err := oscDeleteCmd.CombinedOutput()
			if err != nil {
				slog.Error("failed to run osc remove", slog.String("command", oscDeleteCmd.String()), "error", err, "output", string(output))
				return nil, CommitResult{}, fmt.Errorf("failed to run osc remove: %w\nOutput:\n%s", err, string(output))
			}
			slog.Debug("osc remove finished successfully", slog.String("command", oscDeleteCmd.String()), "output", string(output))
		}

		cmdline := append(baseCmdline, "commit", "-m", params.Message)

		oscCmd := exec.CommandContext(ctx, cmdline[0], cmdline[1:]...)
		oscCmd.Dir = params.Directory

		stdout, err := oscCmd.StdoutPipe()
		if err != nil {
			return nil, CommitResult{}, fmt.Errorf("failed to get stdout pipe: %w", err)
		}
		oscCmd.Stderr = oscCmd.Stdout

		slog.Debug("starting osc commit", slog.String("command", oscCmd.String()), slog.String("dir", params.Directory))
		if err := oscCmd.Start(); err != nil {
			slog.Error("failed to start osc commit", "error", err)
			return nil, CommitResult{}, fmt.Errorf("failed to start osc commit: %w", err)
		}

		var out bytes.Buffer
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			out.WriteString(line)
			out.WriteString("\n")
			if progressToken != nil {
				if err := req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
					ProgressToken: progressToken,
					Message:       line,
				}); err != nil {
					slog.Warn("failed to send progress notification", "error", err)
				}
			}
		}

		err = oscCmd.Wait()
		if err != nil {
			slog.Error("failed to run osc commit", slog.String("command", oscCmd.String()), "error", err, "output", out.String())
			return nil, CommitResult{}, fmt.Errorf("failed to run osc commit: %w\nOutput:\n%s", err, out.String())
		}

		slog.Debug("osc commit finished successfully", slog.String("command", oscCmd.String()))

		// Parse output to find revision.
		// A typical output is "Committed revision 123."
		// Or on obs.group.suse.de "New revision 123."
		output := out.String()
		rev := ""
		if strings.Contains(output, "Committed revision") {
			parts := strings.Split(output, "Committed revision ")
			if len(parts) > 1 {
				rev = strings.TrimRight(strings.Fields(parts[1])[0], ".")
			}
		} else if strings.Contains(output, "New revision") {
			parts := strings.Split(output, "New revision ")
			if len(parts) > 1 {
				rev = strings.TrimRight(strings.Fields(parts[1])[0], ".")
			}
		}

		return nil, CommitResult{Revision: rev}, nil
	}

	projectName := params.ProjectName
	bundleName := params.BundleName
	if projectName == "" {
		projectName = filepath.Base(filepath.Dir(params.Directory))
	}
	if bundleName == "" {
		bundleName = filepath.Base(params.Directory)
	}
	if projectName == "" || bundleName == "" {
		return nil, CommitResult{}, fmt.Errorf("could not determine project and package name from directory: %s", params.Directory)
	}
	if !params.SkipChangesCreation {
		var changesFile string
		if changesFiles, _ := filepath.Glob(path.Join(params.Directory, "*changes")); len(changesFiles) > 0 {
			// only create a changes file if we find a spec file, ergo it's a rpm
			// do some funky math to find the best matching changes file of pkg
			if len(changesFiles) > 1 {
				changesFile, _ = edlib.FuzzySearch(bundleName, changesFiles, edlib.Levenshtein)
			} else {
				changesFile = changesFiles[0]
			}
			// no changes file, let's create one based on a spec files
			if changesFile == "" {
				if specFiles, _ := filepath.Glob(path.Join(params.Directory, "*spec")); len(specFiles) > 0 {
					if len(specFiles) > 1 {
						changesFile, _ = edlib.FuzzySearch(bundleName, specFiles, edlib.Levenshtein)
					} else {
						changesFile = specFiles[0]
					}
					changesFile = strings.TrimSuffix(changesFile, ".spec") + ".changes"
				}
			}
		}
		if changesFile != "" {

			changesEntry := createChangesEntry(params.Message, cred.Name+"-mcpbot", cred.EMail)

			content, err := os.ReadFile(changesFile)
			if err != nil {
				if !os.IsNotExist(err) {
					return nil, CommitResult{}, fmt.Errorf("failed to read changes file %s: %w", changesFile, err)
				}
				content = []byte{}
			}

			newContent := append([]byte(changesEntry), content...)
			err = os.WriteFile(changesFile, newContent, 0644)
			if err != nil {
				return nil, CommitResult{}, fmt.Errorf("failed to write to changes file %s: %w", changesFile, err)
			}
		}
	}
	// get the remote files so that we know what to commit
	if progressToken != nil {
		if err := req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
			ProgressToken: progressToken,
			Message:       "Getting remote file list...",
		}); err != nil {
			slog.Warn("failed to send progress notification", "error", err)
		}
	}
	remoteFiles, err := cred.getRemoteFileList(ctx, projectName, bundleName)
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

	if progressToken != nil {
		if err := req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
			ProgressToken: progressToken,
			Message:       "Comparing local and remote files...",
		}); err != nil {
			slog.Warn("failed to send progress notification", "error", err)
		}
	}

	var changedFiles []string
	var newFiles []string
	var deletedFiles []string
	localFileMap := make(map[string]bool)
	removedFileMap := make(map[string]bool)
	for _, f := range params.RemovedFiles {
		removedFileMap[f] = true
	}

	for _, file := range localFiles {
		if file.IsDir() {
			continue
		}
		fileName := file.Name()
		if strings.HasPrefix(fileName, ".") {
			continue // Ignore hidden files like .osc
		}
		if _, isRemoved := removedFileMap[fileName]; isRemoved {
			continue
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
		} else if remoteEntry.Md5 != hash {
			changedFiles = append(changedFiles, fileName)
		}
	}

	for _, entry := range remoteFiles.Entries {
		if _, exists := localFileMap[entry.Name]; !exists && !strings.HasPrefix(entry.Name, "_service:") {
			deletedFiles = append(deletedFiles, entry.Name)
		}
	}

	filesToUpload := append(newFiles, changedFiles...)
	if len(filesToUpload) > 0 {
		slog.Debug("Uploading changed files", "files", filesToUpload)
		for _, fileName := range filesToUpload {
			if progressToken != nil {
				if err := req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
					ProgressToken: progressToken,
					Message:       "Uploading " + fileName,
				}); err != nil {
					slog.Warn("failed to send progress notification", "error", err)
				}
			}
			filePath := filepath.Join(params.Directory, fileName)
			err := cred.uploadFile(ctx, projectName, bundleName, fileName, filePath)
			if err != nil {
				return nil, CommitResult{}, fmt.Errorf("failed to upload file %s: %w", fileName, err)
			}
		}
	} else {
		slog.Debug("No changed files to upload")
	}

	if len(deletedFiles) > 0 {
		slog.Debug("Deleting remote files", "files", deletedFiles)
	}

	allLocalFiles, err := os.ReadDir(params.Directory)
	if err != nil {
		return nil, CommitResult{}, fmt.Errorf("failed to re-read local directory: %w", err)
	}

	commitDir := Directory{
		Name:    bundleName,
		Project: projectName,
		Link:    remoteFiles.Link,
	}
	for _, file := range allLocalFiles {
		if file.IsDir() {
			continue
		}
		fileName := file.Name()
		if _, isRemoved := removedFileMap[fileName]; isRemoved {
			continue
		}
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

	for _, entry := range remoteFiles.Entries {
		if strings.HasPrefix(entry.Name, "_service:") || entry.Name == "_link" {
			commitDir.Entries = append(commitDir.Entries, entry)
		}
	}

	xmlData, err := xml.MarshalIndent(commitDir, "", "  ")
	if err != nil {
		return nil, CommitResult{}, fmt.Errorf("failed to marshal commit xml: %w", err)
	}

	if progressToken != nil {
		if err := req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
			ProgressToken: progressToken,
			Message:       "Committing changes to OBS...",
		}); err != nil {
			slog.Warn("failed to send progress notification", "error", err)
		}
	}
	revision, err := cred.commitFiles(ctx, projectName, bundleName, params.Message, xmlData)
	if err != nil {
		return nil, CommitResult{}, fmt.Errorf("failed to commit changes: %w", err)
	}

	// Update .osc/_files cache
	oscDir := filepath.Join(params.Directory, ".osc")
	if _, err := os.Stat(oscDir); !os.IsNotExist(err) {
		if progressToken != nil {
			if err := req.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{
				ProgressToken: progressToken,
				Message:       "Updating .osc cache...",
			}); err != nil {
				slog.Warn("failed to send progress notification", "error", err)
			}
		}
		slog.Debug("Updating .osc/_files cache")
		newRemoteFiles, err := cred.getRemoteFileList(ctx, projectName, bundleName)
		if err != nil {
			// Don't fail the whole commit, just warn. The cache can be updated later.
			slog.Warn("failed to get updated remote file list, .osc/_files not updated", "error", err)
		} else {
			filesCachePath := filepath.Join(oscDir, "_files")
			xmlData, err := xml.MarshalIndent(newRemoteFiles, "", "  ")
			if err != nil {
				slog.Warn("failed to marshal updated file list, .osc/_files not updated", "error", err)
			} else {
				// we need to add the XML header
				fullFileContent := append([]byte(xml.Header), xmlData...)
				err := os.WriteFile(filesCachePath, fullFileContent, 0644)
				if err != nil {
					slog.Warn("failed to write to .osc/_files", "error", err)
				} else {
					slog.Debug("Successfully updated .osc/_files")
				}
			}
			sourcesDir := filepath.Join(oscDir, "sources")
			if _, err := os.Stat(sourcesDir); os.IsNotExist(err) {
				os.MkdirAll(sourcesDir, 0755)
			}
			// Create _link file
			if newRemoteFiles.Link != nil {
				linkFilePath := filepath.Join(sourcesDir, "_link")
				linkFileContent := LinkFile{
					Project: newRemoteFiles.Link.Project,
					BaseRev: newRemoteFiles.Link.BaseRev,
				}
				linkFileContent.Patches.Branch = struct {
					XMLName xml.Name `xml:"branch"`
				}{}

				xmlData, err := xml.MarshalIndent(linkFileContent, "", "  ")
				if err != nil {
					slog.Warn("failed to marshal _link file content", "error", err)
				} else {
					fullFileContent := append(xmlData, '\n')
					err := os.WriteFile(linkFilePath, fullFileContent, 0644)
					if err != nil {
						slog.Warn("failed to write _link file", "error", err)
					} else {
						slog.Debug("Successfully created/updated .osc/sources/_link")
					}
				}
			}

			// Synchronize local sources cache and working directory with the new remote state
			for _, entry := range newRemoteFiles.Entries {
				if entry.Name == "_link" {
					continue
				}

				sourceWdPath := filepath.Join(params.Directory, entry.Name)
				sourceCachePath := filepath.Join(sourcesDir, entry.Name)

				if _, err := os.Stat(sourceWdPath); !os.IsNotExist(err) {
					// File exists in working dir, copy it to .osc/sources
					slog.Debug("Copying file to .osc/sources", "file", entry.Name)
					if err := copyFile(sourceWdPath, sourceCachePath); err != nil {
						slog.Warn("failed to copy file to .osc/sources", "file", entry.Name, "error", err)
					}
				} else {
					// File does not exist in working dir, it was generated on the server. Download it.
					slog.Debug("Downloading new server-generated file", "file", entry.Name)
					// Download to working directory
					err := cred.downloadFile(ctx, projectName, bundleName, entry.Name, sourceWdPath)
					if err != nil {
						slog.Warn("failed to download new file", "file", entry.Name, "error", err)
						continue // Don't try to copy if download failed
					}
					// Copy from working directory to .osc/sources
					if err := copyFile(sourceWdPath, sourceCachePath); err != nil {
						slog.Warn("failed to copy new file to .osc/sources", "file", entry.Name, "error", err)
					}
				}
			}
		}
	}

	return nil, CommitResult{Revision: revision.Rev}, nil
}

func (cred *OSCCredentials) getRemoteFileList(ctx context.Context, project, pkg string) (*Directory, error) {
	url := fmt.Sprintf("%s/source/%s/%s", cred.GetAPiAddr(), project, pkg)
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

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func (cred *OSCCredentials) uploadFile(ctx context.Context, project, pkg, fileName, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	fileInfo, _ := file.Stat()
	url := fmt.Sprintf("%s/source/%s/%s/%s", cred.GetAPiAddr(), project, pkg, fileName)
	slog.Debug("Uploading file", "file", fileName, "size", fileInfo.Size(), "project", project, "package", pkg)

	req, err := cred.buildRequest(ctx, "PUT", url, file)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("File upload failed", "file", fileName, "error", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("File upload rejected by server", "file", fileName, "status", resp.StatusCode)
		return fmt.Errorf("failed to upload file: status %s, body: %s", resp.Status, string(body))
	}
	slog.Info("File uploaded successfully", "file", fileName)
	return nil
}

func (cred *OSCCredentials) downloadFile(ctx context.Context, project, pkg, fileName, destinationPath string) error {
	url := fmt.Sprintf("%s/source/%s/%s/%s", cred.GetAPiAddr(), project, pkg, fileName)
	req, err := cred.buildRequest(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to download file: status %s, body: %s", resp.Status, string(body))
	}

	outFile, err := os.Create(destinationPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	_, err = io.Copy(outFile, resp.Body)
	return err
}

func (cred *OSCCredentials) commitFiles(ctx context.Context, project, pkg, message string, xmlData []byte) (*Revision, error) {
	escapedMessage := url.QueryEscape(message)
	url := fmt.Sprintf("%s/source/%s/%s?cmd=commit&comment=%s", cred.GetAPiAddr(), project, pkg, escapedMessage)
	slog.Debug("Committing to OBS", "url", url)
	slog.Info("Committing changes", "project", project, "package", pkg)

	req, err := cred.buildRequest(ctx, "POST", url, bytes.NewReader(xmlData))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/xml")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("Commit request failed", "project", project, "package", pkg, "error", err)
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		slog.Error("Commit rejected by server", "project", project, "package", pkg, "status", resp.StatusCode)
		return nil, fmt.Errorf("failed to commit: status %s, body: %s", resp.Status, string(body))
	}
	var revision Revision
	if err := xml.NewDecoder(resp.Body).Decode(&revision); err != nil {
		return nil, err
	}
	slog.Info("Changes committed successfully", "project", project, "package", pkg, "revision", revision.Rev)
	return &revision, nil
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
