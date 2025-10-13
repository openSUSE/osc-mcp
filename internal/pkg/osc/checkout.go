package osc

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CheckoutPackageCmd struct {
	Project string `json:"project_name" jsonschema:"Name of the project"`
	Package string `json:"package_name" jsonschema:"Name of the package"`
}

type CheckoutPackageResult struct {
	Path        string `json:"path"`
	PackageName string `json:"package_name"`
	ProjectName string `json:"project_name"`
}

func (cred *OSCCredentials) CheckoutBundle(ctx context.Context, req *mcp.CallToolRequest, params CheckoutPackageCmd) (*mcp.CallToolResult, CheckoutPackageResult, error) {
	slog.Debug("mcp tool call: CheckoutBundle", "session", req.Session.ID(), "params", params)
	if params.Project == "" || params.Package == "" {
		return nil, CheckoutPackageResult{}, fmt.Errorf("project and package must be specified")
	}

	cmdline := []string{"osc"}
	configFile, err := cred.writeTempOscConfig()
	if err != nil {
		slog.Warn("failed to write osc config", "error", err)
	} else {
		defer os.Remove(configFile)
		cmdline = append(cmdline, "--config", configFile)
	}
	cmdline = append(cmdline, "checkout", params.Project, params.Package)
	slog.Debug("running osc command", "command", cmdline)
	oscCmd := exec.CommandContext(ctx, cmdline[0], cmdline[1:]...)
	oscCmd.Dir = cred.TempDir
	var out bytes.Buffer
	oscCmd.Stdout = &out
	oscCmd.Stderr = &out
	slog.Info("Checking out bundle", "project", params.Project, "package", params.Package)
	if err := oscCmd.Run(); err != nil {
		slog.Error("failed to run osc checkout", slog.String("command", oscCmd.String()), slog.String("output", out.String()))
		return nil, CheckoutPackageResult{}, fmt.Errorf("failed to run osc checkout command `%s`: %w\nOutput:\n%s", oscCmd.String(), err, out.String())
	}

	checkoutPath := path.Join(cred.TempDir, params.Project, params.Package)
	slog.Info("Bundle checked out successfully", "path", checkoutPath)
	return nil, CheckoutPackageResult{
		Path:        checkoutPath,
		PackageName: params.Package,
		ProjectName: params.Project,
	}, nil
}
