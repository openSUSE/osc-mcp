package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaevor/go-nanoid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openSUSE/osc-mcp/internal/pkg/osc"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func main() {
	pflag.String("http", "", "if set, use streamable HTTP at this address, instead of stdin/stdout")
	pflag.String("api", "https://api.opensuse.org", "address of the api of the OBS instance to interact with")
	pflag.String("workdir", "", "if set, use this directory as temporary directory")
	pflag.String("user", "", "OBS username")
	pflag.String("password", "", "OBS password")
	pflag.Bool("print-creds", false, "Just print the retreived credentials and exit")
	pflag.Bool("clean-workdir", false, "Cleans the workdir before usage")
	pflag.String("logfile", "", "if set, log to this file instead of stderr")

	pflag.Parse()
	viper.SetEnvPrefix("OSC_MCP")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	viper.BindPFlags(pflag.CommandLine)

	var logger *slog.Logger
	if viper.GetString("logfile") != "" {
		f, err := os.OpenFile(viper.GetString("logfile"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			slog.Error("failed to open log file", "error", err)
			os.Exit(1)
		}
		defer f.Close()
		logger = slog.New(slog.NewTextHandler(f, nil))
	} else {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
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
	workDir := viper.GetString("workdir")
	if workDir == "" {
		workDir = filepath.Join(os.TempDir(), id_str)
		noTempClean = false
	}

	if viper.GetBool("clean-workdir") {
		if err = os.RemoveAll(workDir); err != nil {
			slog.Error("failed to clean up workdir", "error", err)
		}
	}
	if err := os.MkdirAll(workDir, 0755); err != nil {
		slog.Error("failed to create temporary directory", "path", workDir, "error", err)
		os.Exit(1)
	}
	if !noTempClean {
		defer os.RemoveAll(workDir)
	}

	obsCred, err := osc.GetCredentials(workDir, id_str)
	if err != nil {
		slog.Error("failed to get OBS credentials", slog.Any("error", err))
		os.Exit(1)
	}
	if viper.GetBool("print-creds") {
		fmt.Printf("user: %s\npasswd: %s\napi: %s\n", obsCred.Name, obsCred.Passwd, obsCred.Apiaddr)
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
		Description: fmt.Sprintf("Branch a package and check it out as local package under the path %s", workDir),
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
		Description: fmt.Sprintf("List all local packages which are located under the path %s", workDir),
	}, obsCred.ListLocalPackages)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "checkout_package",
		Description: fmt.Sprintf("Checkout a package from the online repostory. After this step the package is available as local package under %s", workDir),
	}, obsCred.CheckoutPackage)
	buildLogSchema, err := osc.GetBuildLogSchema()
	if err != nil {
		slog.Error("failed to get input schema for BuildLog", "error", err)
		os.Exit(1)
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_build_log",
		Description: "Get the remote or local build log of a package.",
		InputSchema: buildLogSchema,
	}, obsCred.BuildLog)
	server.AddPrompt(&mcp.Prompt{
		Name:        "basic_information",
		Description: "Basic information about the tools and how they are used for the OpenBuild Server.",
	}, obsCred.PromptOSC)
	if viper.GetString("http") != "" {
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
			return server
		}, nil)
		slog.Info("MCP handler listening at", slog.String("address", viper.GetString("http")))
		http.ListenAndServe(viper.GetString("http"), handler)
	} else {
		t := mcp.NewLoggingTransport(mcp.NewStdioTransport(), os.Stdout)
		if err := server.Run(context.Background(), t); err != nil {
			slog.Error("Server failed", slog.Any("error", err))
		}
	}
}
