package main

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openSUSE/osc-mcp/internal/pkg/licenses"
	"github.com/openSUSE/osc-mcp/internal/pkg/osc"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

//go:embed data/defaults.yaml
var defaultsYaml []byte

//go:embed data/licenses.json
var licensesJson []byte

func main() {
	osc.SetDefaultsYaml(defaultsYaml)
	licenses.SetLicensesJson(licensesJson)

	// DO NOT SET DEFAULTS HERE
	pflag.String("http", "", "if set, use streamable HTTP at this address, instead of stdin/stdout")
	pflag.String("api", "", "address of the api of the OBS instance to interact with")
	pflag.String("workdir", "", "if set, use this directory as temporary directory")
	pflag.String("user", "", "OBS username")
	pflag.String("email", "", "user's email address")
	pflag.String("password", "", "OBS password")
	pflag.Bool("print-creds", false, "Just print the retrieved credentials and exit")
	pflag.Bool("clean-workdir", false, "Cleans the workdir before usage")
	pflag.String("logfile", "", "if set, log to this file instead of stderr")
	pflag.BoolP("verbose", "v", false, "Enable verbose logging")
	pflag.BoolP("debug", "d", false, "Enable debug logging")

	pflag.Parse()
	viper.SetEnvPrefix("OSC_MCP")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	viper.BindPFlags(pflag.CommandLine)

	logLevel := slog.LevelWarn
	if viper.GetBool("verbose") {
		logLevel = slog.LevelInfo
	}
	if viper.GetBool("debug") {
		logLevel = slog.LevelDebug
	}
	handlerOpts := &slog.HandlerOptions{
		Level: logLevel,
	}
	var logger *slog.Logger
	if viper.GetString("logfile") != "" {
		f, err := os.OpenFile(viper.GetString("logfile"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			slog.Error("failed to open log file", "error", err)
			os.Exit(1)
		}
		defer f.Close()
		logger = slog.New(slog.NewTextHandler(f, handlerOpts))
	} else {
		logger = slog.New(slog.NewTextHandler(os.Stderr, handlerOpts))
	}
	slog.SetDefault(logger)
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "OSC LLM bridge",
		Version: "0.0.1"},
		&mcp.ServerOptions{
			InitializedHandler: func(ctx context.Context, req *mcp.InitializedRequest) {
				slog.Info("Session started", "ID", req.Session.ID())
			},
		})
	noTempClean := true
	obsCred, err := osc.GetCredentials()
	if err != nil {
		slog.Error("failed to get credentials", "error", err)
		os.Exit(1)
	}

	if viper.GetBool("clean-workdir") {
		if err = os.RemoveAll(obsCred.TempDir); err != nil {
			slog.Error("failed to clean up workdir", "error", err)
		}
	}
	if err := os.MkdirAll(obsCred.TempDir, 0755); err != nil {
		slog.Error("failed to create temporary directory", "path", obsCred.TempDir, "error", err)
		os.Exit(1)
	}
	if !noTempClean {
		defer os.RemoveAll(obsCred.TempDir)
	}

	if err != nil {
		slog.Error("failed to get OBS credentials", slog.Any("error", err))
		os.Exit(1)
	}
	if viper.GetBool("print-creds") {
		fmt.Printf("user: %s\npasswd: %s\napi: %s\n", obsCred.Name, obsCred.Passwd, obsCred.Apiaddr)
		os.Exit(0)
	}

	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_bundle",
		Description: fmt.Sprintf("Search bundles on remote open build (OBS) instance %s or local bundles. A bundle is also known as source package. Use the project name 'local' to list local packages. If project and bundle name is empty local packages will be listed. A bundle must be built to create installable packages.", obsCred.Apiaddr),
	}, obsCred.SearchSrcBundle)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_source_files",
		Description: "List source files of given bundle in local or remote location. Also returns basic information of the files and if they are modified locally. The content of small files is returned and also the content of all relevant control files which are files with .spec and .kiwi suffix. Prefer this tool read command file before checking them out. If a file name is given only the requested file is shown, regardless it's size.",
	}, obsCred.ListSrcFiles)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "branch_bundle",
		Description: fmt.Sprintf("Branch a bundle and check it out as local bundle under the path %s", obsCred.TempDir),
	}, obsCred.BranchBundle)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "build_bundle",
		Description: "Build a source bundle also known as source package.",
	}, obsCred.Build)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_project_meta",
		Description: "Get the metadata of a project. The metadata defines for which project a source bundle can be built",
	}, obsCred.GetProjectMeta)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_project_meta",
		Description: "Set the metadata for the project. Create the project if it doesn't exist.",
	}, obsCred.SetProjectMeta)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "create_bundle",
		Description: "Create a new local bundle. Will also create a project if it does not exist. Before commit this package can't be checked out.",
		InputSchema: osc.CreateBundleInputSchema(),
	}, obsCred.CreateBundle)
	// /*
	// 	mcp.AddTool(server, &mcp.Tool{
	// 		Name:        "delete_project",
	// 		Description: "Deletes a remote project and all the packages of this project.",
	// 	}, obsCred.DeleteProject)
	// */
	mcp.AddTool(server, &mcp.Tool{
		Name:        "checkout_bundle",
		Description: fmt.Sprintf("Checkout a package from the online repository. After this step the package is available as local package under %s", obsCred.TempDir),
	}, obsCred.CheckoutBundle)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_build_log",
		Description: "Get the remote or local build log of a package.",
	}, obsCred.BuildLog)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_packages",
		Description: "Search the available packages for a remote repository. This are the allreadu built packages and are required by bundles or source packages for building.",
	}, obsCred.SearchPackages)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "commit",
		Description: "Commits changed files. If a .spec file is staged, the corresponding .changes file will be updated or created accordingly to input.",
	}, obsCred.Commit)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_requests",
		Description: fmt.Sprintf("Get a list of requests. Need to set one of the following: user, group, project, package, state, reviewstates, types, ids. If not package group or ids ist set %s will be set for user.", obsCred.Name),
	}, obsCred.ListRequests)
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_request",
		Description: "Get a single request by its ID. Includes a diff to what has changed in that request.",
	}, obsCred.GetRequest)
	server.AddPrompt(&mcp.Prompt{
		Name:        "basic_information",
		Description: "Basic information about the tools and how they are used for the OpenBuild Server.",
	}, obsCred.PromptOSC)
	server.AddPrompt(&mcp.Prompt{
		Name:        "package_missing",
		Description: "Steps on what to do when a build failed because of a missing package.",
	}, obsCred.PromptPackage)
	server.AddResource(&mcp.Resource{
		Name:        "spdx_licenses",
		Description: "List of SPDX licenses which can be used a identifier.",
	}, licenses.GetLicenseIdentifiers)
	if viper.GetString("http") != "" {
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
			return server
		}, nil)
		slog.Info("MCP handler listening at", slog.String("address", viper.GetString("http")))
		http.ListenAndServe(viper.GetString("http"), handler)
	} else {
		slog.Info("New client has connected via stdin/stdout")

		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			slog.Error("Server failed", slog.Any("error", err))
		}
	}
}
