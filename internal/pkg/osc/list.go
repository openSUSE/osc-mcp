package osc

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/beevik/etree"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var ErrBundleOrProjectNotFound = errors.New("bundle or project not found")

const maxSize = 10240

func commandFiles() []string {
	return []string{".spec", ".kiwi", "Dockerfile", "_service", "_limits"}
}

type ListSrcFilesParam struct {
	ProjectName string `json:"project_name" jsonschema:"Name of the project"`
	PackageName string `json:"package_name" jsonschema:"Name of the bundle or source package"`
	Local       bool   `json:"local,omitempty" jsonschema:"List source files of local bundle"`
	Filename    string `json:"filename,omitempty" jsonschema:"Print content of file instead of all files in bundle."`
}

type FileInfo struct {
	Name    string `json:"name"`
	Size    string `json:"size"`
	MD5     string `json:"md5"`
	MTime   string `json:"mtime"`
	Content string `json:"content,omitempty"`
}

type FileInfoLocal struct {
	Modified  bool `json:"modified,omitempty"`
	LocalOnly bool `json:"localonly,omitempty"`
	FileInfo
}

type ReturnedInfo struct {
	ProjectName string `json:"project_name" jsonschema:"Name of the project"`
	PackageName string `json:"package_name" jsonschema:"Name of the bundle or source package"`
}

type ReturnedInfoRemote struct {
	ReturnedInfo
	Files []FileInfo `json:"files" jsonschema:"List of files"`
}

type ReturnedInfoLocal struct {
	ReturnedInfo
	Local     bool            `json:"local" jsonschema:"Is local package"`
	LocalOnly bool            `json:"local_only" jsonschema:"File is only in the local repo"`
	Files     []FileInfoLocal `json:"files" jsonschema:"List of files"`
}

func (cred *OSCCredentials) getRemoteList(ctx context.Context, projectName string, packageName string) ([]FileInfo, error) {
	path := fmt.Sprintf("source/%s/%s", projectName, packageName)
	resp, err := cred.apiGetRequest(ctx, path, map[string]string{"Accept": "application/xml; charset=utf-8"})
	if err != nil {
		return nil, fmt.Errorf("failed to get remote file list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return nil, ErrBundleOrProjectNotFound
		}
		return nil, fmt.Errorf("api request failed with status: %s", resp.Status)
	}

	doc := etree.NewDocument()
	if _, err := doc.ReadFrom(resp.Body); err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var files []FileInfo
	for _, entry := range doc.FindElements("//entry") {
		f := FileInfo{
			Name:  entry.SelectAttrValue("name", ""),
			Size:  entry.SelectAttrValue("size", ""),
			MD5:   entry.SelectAttrValue("md5", ""),
			MTime: entry.SelectAttrValue("mtime", ""),
		}
		files = append(files, f)
	}
	return files, nil
}

func (cred *OSCCredentials) getRemoteFileContent(ctx context.Context, projectName, packageName, fileName string) ([]byte, error) {
	path := fmt.Sprintf("source/%s/%s/%s", projectName, packageName, fileName)
	resp, err := cred.apiGetRequest(ctx, path, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get remote file content: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api request failed with status: %s", resp.Status)
	}

	return io.ReadAll(resp.Body)
}

func (cred *OSCCredentials) ListSrcFiles(ctx context.Context, req *mcp.CallToolRequest, params ListSrcFilesParam) (*mcp.CallToolResult, any, error) {
	if params.ProjectName == "" {
		return nil, nil, fmt.Errorf("project name cannot be empty")
	}
	if params.PackageName == "" {
		return nil, nil, fmt.Errorf("package name cannot be empty")
	}

	if params.Filename != "" {
		if params.Local {
			filePath := filepath.Join(cred.TempDir, params.ProjectName, params.PackageName, params.Filename)
			content, err := os.ReadFile(filePath)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to read local file %s: %w", params.Filename, err)
			}

			// Check for binary file (look for null bytes in the first 1024 bytes)
			checkLen := 1024
			if len(content) < checkLen {
				checkLen = len(content)
			}
			for i := 0; i < checkLen; i++ {
				if content[i] == 0 {
					return nil, nil, fmt.Errorf("file %s is a binary file", params.Filename)
				}
			}

			info, err := os.Stat(filePath)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to get file info for %s: %w", params.Filename, err)
			}

			hash := md5.New()
			hash.Write(content)
			md5sum := hex.EncodeToString(hash.Sum(nil))

			f := FileInfoLocal{
				FileInfo: FileInfo{
					Name:    params.Filename,
					Size:    fmt.Sprintf("%d", info.Size()),
					MD5:     md5sum,
					MTime:   fmt.Sprintf("%d", info.ModTime().Unix()),
					Content: string(content),
				},
			}

			remoteFiles, err := cred.getRemoteList(ctx, params.ProjectName, params.PackageName)
			isLocalOnlyPackage := false
			if err != nil {
				isLocalOnlyPackage = true
				if errors.Is(err, ErrBundleOrProjectNotFound) {
				} else {
					slog.Warn("error when trying to get remote file", "error", err)
				}
			}

			if isLocalOnlyPackage {
				f.LocalOnly = true
			} else {
				remoteFileFound := false
				for _, remoteFile := range remoteFiles {
					if remoteFile.Name == f.Name {
						if remoteFile.MD5 != f.MD5 {
							f.Modified = true
						}
						remoteFileFound = true
						break
					}
				}
				if !remoteFileFound {
					f.LocalOnly = true
				}
			}

			return nil, ReturnedInfoLocal{
				ReturnedInfo: ReturnedInfo{
					ProjectName: params.ProjectName,
					PackageName: params.PackageName,
				},
				Files:     []FileInfoLocal{f},
				Local:     true,
				LocalOnly: isLocalOnlyPackage,
			}, nil
		}

		// Remote file
		content, err := cred.getRemoteFileContent(ctx, params.ProjectName, params.PackageName, params.Filename)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get remote file content: %w", err)
		}

		// Check for binary file (look for null bytes in the first 1024 bytes)
		checkLen := 1024
		if len(content) < checkLen {
			checkLen = len(content)
		}
		for i := 0; i < checkLen; i++ {
			if content[i] == 0 {
				return nil, nil, fmt.Errorf("file %s is a binary file", params.Filename)
			}
		}

		files, err := cred.getRemoteList(ctx, params.ProjectName, params.PackageName)
		if err != nil {
			return nil, nil, err
		}

		var fileInfo FileInfo
		found := false
		for _, f := range files {
			if f.Name == params.Filename {
				fileInfo = f
				found = true
				break
			}
		}

		if !found {
			return nil, nil, fmt.Errorf("file %s not found in remote package", params.Filename)
		}

		fileInfo.Content = string(content)

		return nil, ReturnedInfoRemote{
			ReturnedInfo: ReturnedInfo{
				ProjectName: params.ProjectName,
				PackageName: params.PackageName,
			},
			Files: []FileInfo{fileInfo},
		}, nil
	}

	if params.Local {
		remoteFiles, err := cred.getRemoteList(ctx, params.ProjectName, params.PackageName)
		if err != nil {
			remoteFiles = []FileInfo{}
			if !errors.Is(err, ErrBundleOrProjectNotFound) {
				slog.Warn("error when getting reote file", "error", err)
			}
		}
		remoteFilesMap := make(map[string]FileInfo)
		for _, rf := range remoteFiles {
			remoteFilesMap[rf.Name] = rf
		}

		packagePath := filepath.Join(cred.TempDir, params.ProjectName, params.PackageName)
		entries, err := os.ReadDir(packagePath)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read local package directory %s: %w", packagePath, err)
		}

		var files []FileInfoLocal
		isLocalOnlyPackage := len(remoteFiles) == 0

		for _, entry := range entries {
			isIgnored := false
			for _, ignoredDir := range IgnoredDirs() {
				if entry.Name() == ignoredDir {
					isIgnored = true
					break
				}
			}
			if isIgnored || entry.IsDir() {
				continue
			}

			filePath := filepath.Join(packagePath, entry.Name())
			info, err := entry.Info()
			if err != nil {
				continue
			}

			file, err := os.Open(filePath)
			if err != nil {
				continue
			}
			hash := md5.New()
			_, err = io.Copy(hash, file)
			file.Close()
			if err != nil {
				continue
			}
			md5sum := hex.EncodeToString(hash.Sum(nil))

			f := FileInfoLocal{
				FileInfo: FileInfo{
					Name:  entry.Name(),
					Size:  fmt.Sprintf("%d", info.Size()),
					MD5:   md5sum,
					MTime: fmt.Sprintf("%d", info.ModTime().Unix()),
				},
			}
			if info.Size() < maxSize {
				fileName := entry.Name()
				isCmdFile := false
				for _, cmdFile := range commandFiles() {
					if strings.HasSuffix(fileName, cmdFile) {
						isCmdFile = true
						break
					}
				}

				if isCmdFile {
					content, err := os.ReadFile(filePath)
					if err == nil {
						f.Content = string(content)
					}
				}
			}
			if isLocalOnlyPackage {
				f.LocalOnly = true
			} else {
				if remoteFile, ok := remoteFilesMap[f.Name]; ok {
					if remoteFile.MD5 != f.MD5 {
						f.Modified = true
					}
				} else {
					f.LocalOnly = true
				}
			}
			files = append(files, f)
		}

		return nil, ReturnedInfoLocal{
			ReturnedInfo: ReturnedInfo{
				ProjectName: params.ProjectName,
				PackageName: params.PackageName,
			},
			Files:     files,
			Local:     true,
			LocalOnly: isLocalOnlyPackage,
		}, nil
	}

	files, err := cred.getRemoteList(ctx, params.ProjectName, params.PackageName)
	if err != nil {
		return nil, nil, err
	}

	for i := range files {
		file := &files[i]
		size, err := strconv.ParseInt(file.Size, 10, 64)
		if err != nil {
			continue
		}
		isCmdFile := false
		for _, cmdFile := range commandFiles() {
			if strings.HasSuffix(file.Name, cmdFile) {
				isCmdFile = true
				break
			}
		}
		if isCmdFile || size < maxSize {
			content, err := cred.getRemoteFileContent(ctx, params.ProjectName, params.PackageName, file.Name)
			if err == nil {
				file.Content = string(content)
			}
		}
	}

	return nil, ReturnedInfoRemote{
		ReturnedInfo: ReturnedInfo{
			ProjectName: params.ProjectName,
			PackageName: params.PackageName,
		},
		Files: files,
	}, nil
}

type ListLocalParams struct {
	Number int `json:"number,omitempty" jsonschema:"number of packages to display"`
}

type LocalPackage struct {
	PackageName string `json:"package_name"`
	ProjectName string `json:"project_name"`
	Path        string `json:"path"`
}