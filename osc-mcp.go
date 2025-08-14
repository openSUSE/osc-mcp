package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openSUSE/osc-mcp/internal/pkg/osc"
)

var httpAddr = flag.String("http", "", "if set, use streamable HTTP at this address, instead of stdin/stdout")
var oscInstance = flag.String("api", "api.opensuse.org", "address of the api of the OBS instance to interact with")
var tempDir = flag.String("workdir", "", "if set, use this directory as temporary directory")
var print_creds = flag.Bool("print-creds", false, "Just print the retreived credentials and exit")

func main() {
	flag.Parse()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "OS software management",
		Version: "0.0.1"}, nil)

	obsCred, err := osc.GetCredentials(*tempDir)
	if *print_creds {
		fmt.Printf("user: %s\npasswd: %s\n", obsCred.Name, obsCred.Passwd)
		os.Exit(0)
	}
	if err != nil {
		slog.Error("failed to get OBS credentials", slog.Any("error", err))
		os.Exit(1)
	}
	if *tempDir == "" {
		defer os.RemoveAll(obsCred.TempDir)
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_packages",
		Description: "Search packages on remote instance.",
	}, obsCred.SearchSrcPkg)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_source_files",
		Description: "List source files of a package.",
	}, obsCred.ListSrcFiles)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "branch_package",
		Description: "Branch a package into a new project.",
	}, obsCred.BranchPackage)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "build_package",
		Description: "Build a package.",
	}, obsCred.Build)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_project_meta",
		Description: "Get the metadata of a project.",
	}, obsCred.GetProjectMeta)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_project_meta",
		Description: "Write project's meta file. Create the project if it doesn't exist.",
	}, obsCred.SetProjectMeta)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_package",
		Description: "Create a new package. Will also create a project if it does not exist.",
	}, obsCred.CreatePackage)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_project",
		Description: "Deletes a specified project and all the packages of this project.",
	}, obsCred.DeleteProject)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_local_packages",
		Description: "List all packages that are locally checked out.",
	}, obsCred.ListLocalPackages)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "checkout_package",
		Description: "Checkout a package to a temporary directory.",
	}, obsCred.CheckoutPackage)
	if *httpAddr != "" {
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
			return server
		}, nil)
		slog.Info("MCP handler listening at", slog.String("address", *httpAddr))
		http.ListenAndServe(*httpAddr, handler)
	} else {
		t := mcp.NewLoggingTransport(mcp.NewStdioTransport(), os.Stdout)
		if err := server.Run(context.Background(), t); err != nil {
			slog.Error("Server failed", slog.Any("error", err))
		}
	}
}
