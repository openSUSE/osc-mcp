package osc

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type EditFileParams struct {
	Directory string `json:"directory" jsonschema:"The checkout directory path where the file is located"`
	Filename  string `json:"filename" jsonschema:"The name of the file to edit"`
	Content   string `json:"content" jsonschema:"The new content to write to the file"`
}

type EditFileResult struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
	Size    int    `json:"size"`
	Message string `json:"message,omitempty"`
}

// inWorkdir returns the absolute path of dir if it is the working directory or below it.
func (cred *OSCCredentials) inWorkdir(dir string) (string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve directory path: %w", err)
	}
	absTempDir, err := filepath.Abs(cred.TempDir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve working directory path: %w", err)
	}
	// compare real paths so a symlink cannot lead out of the working directory
	if real, err := filepath.EvalSymlinks(absDir); err == nil {
		absDir = real
	}
	if real, err := filepath.EvalSymlinks(absTempDir); err == nil {
		absTempDir = real
	}
	rel, err := filepath.Rel(absTempDir, absDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("directory must be under the working directory %s", cred.TempDir)
	}
	return absDir, nil
}

func (cred *OSCCredentials) EditFile(ctx context.Context, req *mcp.CallToolRequest, params EditFileParams) (*mcp.CallToolResult, any, error) {
	slog.Debug("mcp tool call: EditFile", "session", req.Session.ID(), "params", params)

	if params.Directory == "" {
		return nil, nil, fmt.Errorf("directory cannot be empty")
	}
	if params.Filename == "" {
		return nil, nil, fmt.Errorf("filename cannot be empty")
	}
	if params.Content == "" {
		return nil, nil, fmt.Errorf("content cannot be empty")
	}
	if params.Filename != filepath.Base(params.Filename) || params.Filename == ".." {
		return nil, nil, fmt.Errorf("filename cannot contain path separators or '..'")
	}

	absDir, err := cred.inWorkdir(params.Directory)
	if err != nil {
		return nil, nil, err
	}
	if _, err := os.Stat(absDir); os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("directory does not exist: %s", params.Directory)
	}

	filePath := filepath.Join(absDir, params.Filename)
	if info, err := os.Lstat(filePath); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("%s is a symlink, not writing through it", params.Filename)
	}
	if err := os.WriteFile(filePath, []byte(params.Content), 0644); err != nil {
		return nil, nil, fmt.Errorf("failed to write file: %w", err)
	}
	slog.Info("file written", "path", filePath, "size", len(params.Content))

	return nil, EditFileResult{
		Success: true,
		Path:    filePath,
		Size:    len(params.Content),
		Message: fmt.Sprintf("Successfully wrote %d bytes to %s", len(params.Content), params.Filename),
	}, nil
}
