package osc

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ReplaceFileParams struct {
	Directory string `json:"directory" jsonschema:"The checkout directory path where the file is located"`
	Filename  string `json:"filename" jsonschema:"The name of the file to replace or create"`
	Content   string `json:"content" jsonschema:"The complete new content of the file"`
}

type ReplaceFileResult struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
	Size    int    `json:"size"`
	Message string `json:"message,omitempty"`
}

func (cred *OSCCredentials) ReplaceFile(ctx context.Context, req *mcp.CallToolRequest, params ReplaceFileParams) (*mcp.CallToolResult, any, error) {
	slog.Debug("mcp tool call: ReplaceFile", "session", req.Session.ID(), "params", params)

	if params.Directory == "" {
		return nil, nil, fmt.Errorf("directory cannot be empty")
	}
	if params.Filename == "" {
		return nil, nil, fmt.Errorf("filename cannot be empty")
	}
	if params.Content == "" {
		return nil, nil, fmt.Errorf("content cannot be empty")
	}
	if params.Filename != filepath.Base(params.Filename) {
		return nil, nil, fmt.Errorf("filename cannot contain path separators")
	}

	absDir, err := filepath.Abs(params.Directory)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve directory path: %w", err)
	}
	absTempDir, err := filepath.Abs(cred.TempDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to resolve working directory path: %w", err)
	}
	relDir, err := filepath.Rel(absTempDir, absDir)
	if err != nil {
		return nil, nil, fmt.Errorf("directory must be under the working directory %s", cred.TempDir)
	}

	// os.Root refuses any path, '..' or symlink, that leads out of the working directory
	root, err := os.OpenRoot(absTempDir)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open working directory: %w", err)
	}
	defer root.Close()

	if _, err := root.Stat(relDir); os.IsNotExist(err) {
		return nil, nil, fmt.Errorf("directory does not exist: %s", params.Directory)
	} else if err != nil {
		return nil, nil, fmt.Errorf("directory must be under the working directory %s: %w", cred.TempDir, err)
	}
	relFile := filepath.Join(relDir, params.Filename)
	if info, err := root.Lstat(relFile); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return nil, nil, fmt.Errorf("%s is a symlink, not writing through it", params.Filename)
	}
	if err := root.WriteFile(relFile, []byte(params.Content), 0644); err != nil {
		return nil, nil, fmt.Errorf("failed to write file: %w", err)
	}
	filePath := filepath.Join(absDir, params.Filename)
	slog.Info("file written", "path", filePath, "size", len(params.Content))

	return nil, ReplaceFileResult{
		Success: true,
		Path:    filePath,
		Size:    len(params.Content),
		Message: fmt.Sprintf("Successfully wrote %d bytes to %s", len(params.Content), params.Filename),
	}, nil
}
