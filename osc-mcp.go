package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/jaevor/go-nanoid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openSUSE/osc-mcp/internal/pkg/osc"
)

var httpAddr = flag.String("http", "", "if set, use streamable HTTP at this address, instead of stdin/stdout")
var oscInstance = flag.String("api", "api.opensuse.org", "address of the api of the OBS instance to interact with")
var workDir = flag.String("workdir", "", "if set, use this directory as temporary directory")
var print_creds = flag.Bool("print-creds", false, "Just print the retreived credentials and exit")
var clean_temp = flag.Bool("clean-workdir", false, "Cleans the workdir before usage")

func main() {
	flag.Parse()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "OS software management",
		Version: "0.0.1"},
		nil)

	noTempClean := true
	id, err := nanoid.Canonic()
	if err != nil {
		slog.Error("failed to generate nano id", "error", err)
		os.Exit(1)
	}
	id_str := id()
	if *workDir == "" {
		workdir_str := filepath.Join(os.TempDir(), id_str)
		workDir = &workdir_str
		noTempClean = false
	}

	if *clean_temp {
		if err = os.RemoveAll(*workDir); err != nil {
			slog.Error("failed to clean up workdir", "error", err)
		}
	}
	if err := os.MkdirAll(*workDir, 0755); err != nil {
		slog.Error("failed to create temporary directory", "path", *workDir, "error", err)
		os.Exit(1)
	}
	if !noTempClean {
		defer os.RemoveAll(*workDir)
	}

	obsCred, err := osc.GetCredentials(*workDir, id_str)
	if err != nil {
		slog.Error("failed to get OBS credentials", slog.Any("error", err))
		os.Exit(1)
	}
	if *print_creds {
		fmt.Printf("user: %s\npasswd: %s\n", obsCred.Name, obsCred.Passwd)
		os.Exit(0)
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_packages",
		Description: "Search packages on remote instance.",
	}, obsCred.SearchSrcPkg)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_source_files",
		Description: "List source files of a remote package.",
	}, obsCred.ListSrcFiles)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "branch_package",
		Description: fmt.Sprintf("Branch a package and check it out as local package under the path %s", *workDir),
	}, obsCred.BranchPackage)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "build_package",
		Description: "Build a package.",
	}, obsCred.Build)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_project_meta",
		Description: "Get the metadata of a remote project.",
	}, obsCred.GetProjectMeta)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_project_meta",
		Description: "Write project's meta file. Create the project if it doesn't exist.",
	}, obsCred.SetProjectMeta)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_package",
		Description: "Create a new local package. Will also create a project if it does not exist. Before commit this package can't be checked out.",
	}, obsCred.CreatePackage)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "delete_project",
		Description: "Deletes a remote project and all the packages of this project.",
	}, obsCred.DeleteProject)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_local_packages",
		Description: fmt.Sprintf("List all local packages which are located under the path %s", *workDir),
	}, obsCred.ListLocalPackages)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "checkout_package",
		Description: fmt.Sprintf("Checkout a package from the online repostory. After this step the package is available as local package under %s", *workDir),
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
