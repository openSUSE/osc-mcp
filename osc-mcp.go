package main

import (
	"context"
	"flag"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openSUSE/osc-mcp/internal/pkg/osc"
	"log/slog"
	"net/http"
	"os"
)

var httpAddr = flag.String("http", "", "if set, use streamable HTTP at this address, instead of stdin/stdout")
var oscInstance = flag.String("api", "api.opensuse.org", "address of the api of the OBS instance to interact with")

func main() {
	flag.Parse()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "OS software management",
		Version: "0.0.1"}, nil)
	obsCred, err := osc.GetCredentials()
	if err != nil {
		slog.Error("failed to get OBS credentials", slog.Any("error", err))
		os.Exit(1)
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
