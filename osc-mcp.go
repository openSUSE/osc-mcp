package main

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/openSUSE/mcp-archive/archive"
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
	pflag.Bool("log-json", false, "Output logs in JSON format (machine-readable)")
	pflag.Bool("list-tools", false, "List all available tools and exit")
	pflag.StringSlice("enabled-tools", nil, "A list of tools to enable. Defaults to all tools.")

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
	logOutput := os.Stderr
	if viper.GetString("logfile") != "" {
		f, err := os.OpenFile(viper.GetString("logfile"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
		if err != nil {
			slog.Error("failed to open log file", "error", err)
			os.Exit(1)
		}
		defer f.Close()
		logOutput = f
	}

	// Choose handler based on format preference
	if viper.GetBool("log-json") {
		logger = slog.New(slog.NewJSONHandler(logOutput, handlerOpts))
	} else {
		logger = slog.New(slog.NewTextHandler(logOutput, handlerOpts))
	}
	slog.SetDefault(logger)

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "OSC LLM bridge",
		Version: "0.2.1"},
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

	archiver, err := archive.New(obsCred.TempDir)
	if err != nil {
		slog.Error("failed to create archiver", "error", err)
		os.Exit(1)
	}

	tools := []struct {
		Tool     *mcp.Tool
		Register func(server *mcp.Server, tool *mcp.Tool)
	}{
		{
			Tool: &mcp.Tool{
				Name:        "search_bundle",
				Description: fmt.Sprintf("Search bundles on remote open build (OBS) instance %s or local bundles. A bundle is also known as source package. Use the project name 'local' to list local packages. If project and bundle name is empty local packages will be listed. A bundle must be built to create installable packages.", obsCred.Apiaddr),
			},
			Register: func(server *mcp.Server, tool *mcp.Tool) {
				mcp.AddTool(server, tool, obsCred.SearchSrcBundle)
			},
		},
		{
			Tool: &mcp.Tool{
				Name:        "list_source_files",
				Description: "List source files of given bundle in local or remote location. Also returns basic information of the files and if they are modified locally. The content of small files is returned and also the content of all relevant control files which are files with .spec and .kiwi suffix. Prefer this tool read command file before checking them out. If a file name is given only the requested file is shown, regardless it's size.",
			},
			Register: func(server *mcp.Server, tool *mcp.Tool) {
				mcp.AddTool(server, tool, obsCred.ListSrcFiles)
			},
		},
		{
			Tool: &mcp.Tool{
				Name:        "branch_bundle",
				Description: fmt.Sprintf("Branch a bundle and check it out as local bundle under the path %s", obsCred.TempDir),
			},
			Register: func(server *mcp.Server, tool *mcp.Tool) {
				mcp.AddTool(server, tool, obsCred.BranchBundle)
			},
		},
		{
			Tool: &mcp.Tool{
				Name:        "run_build",
				Description: "Build a source bundle also known as source package. A build is awlays local and withoout any online connection. All source files and software has to be downloaded and provided in advance.",
			},
			Register: func(server *mcp.Server, tool *mcp.Tool) {
				mcp.AddTool(server, tool, obsCred.Build)
			},
		},
		{
			Tool: &mcp.Tool{
				Name:        "run_services",
				Description: "Run OBS source services on a specified project and bundle. Important services are: download_files: downloads the source files reference via an URI in the spec file with the pattern https://github.com/foo/baar/v%{version}.tar.gz#./%{name}-%{version}.tar.gz, go_modules: which creates a vendor directory for go files if the source has the same name as the project.",
			},
			Register: func(server *mcp.Server, tool *mcp.Tool) {
				mcp.AddTool(server, tool, obsCred.RunServices)
			},
		},
		{
			Tool: &mcp.Tool{
				Name:        "get_project_meta",
				Description: "Get the metadata of a project. The metadata defines for which project a source bundle can be built the bundles inside the project. The subprojects of the projects are also listed. Project and sub project names are separated with colons.",
			},
			Register: func(server *mcp.Server, tool *mcp.Tool) {
				mcp.AddTool(server, tool, obsCred.GetProjectMeta)
			},
		},
		{
			Tool: &mcp.Tool{
				Name:        "set_project_meta",
				Description: "Set the metadata for the project. Create the project if it doesn't exist.",
			},
			Register: func(server *mcp.Server, tool *mcp.Tool) {
				mcp.AddTool(server, tool, obsCred.SetProjectMeta)
			},
		},
		{
			Tool: &mcp.Tool{
				Name:        "create",
				Description: "Create a new local bundle or _service/.spec file. Will also create a project or bundle if it does not exist. Before commit this package can't be checked out. Prefer creating _service files with this tool.",
				InputSchema: osc.CreateBundleInputSchema(),
			},
			Register: func(server *mcp.Server, tool *mcp.Tool) {
				mcp.AddTool(server, tool, obsCred.Create)
			},
		},
		/*
			mcp.AddTool(server, &mcp.Tool{
				Name:        "delete_project",
				Description: "Deletes a remote project and all the packages of this project.",
			}, obsCred.DeleteProject)
		*/
		{
			Tool: &mcp.Tool{
				Name:        "checkout_bundle",
				Description: fmt.Sprintf("Checkout a bundle from the online repository. After this step the package is available as local package under %s. Check out a single package instead of the complete repository if possible,", obsCred.TempDir),
			},
			Register: func(server *mcp.Server, tool *mcp.Tool) {
				mcp.AddTool(server, tool, obsCred.CheckoutBundle)
			},
		},
		{
			Tool: &mcp.Tool{
				Name:        "get_build_log",
				Description: "Get the remote or local build log of a package.",
			},
			Register: func(server *mcp.Server, tool *mcp.Tool) {
				mcp.AddTool(server, tool, obsCred.BuildLog)
			},
		},
		{
			Tool: &mcp.Tool{
				Name:        "search_packages",
				Description: "Search the available packages for a remote repository. This are the already built packages and are required by bundles or source packages for building.",
			},
			Register: func(server *mcp.Server, tool *mcp.Tool) {
				mcp.AddTool(server, tool, obsCred.SearchPackages)
			},
		},
		{
			Tool: &mcp.Tool{
				Name:        "commit",
				Description: "Commits changed files. If a .spec file is staged, the corresponding .changes file will be updated or created accordingly to input.",
			},
			Register: func(server *mcp.Server, tool *mcp.Tool) {
				mcp.AddTool(server, tool, obsCred.Commit)
			},
		},
		{
			Tool: &mcp.Tool{
				Name:        "list_requests",
				Description: fmt.Sprintf("Get a list of requests. Need to set one of the following: user, group, project, package, state, reviewstates, types, ids. If not package group or ids ist set %s will be set for user.", obsCred.Name),
			},
			Register: func(server *mcp.Server, tool *mcp.Tool) {
				mcp.AddTool(server, tool, obsCred.ListRequests)
			},
		},
		{
			Tool: &mcp.Tool{
				Name:        "get_request",
				Description: "Get a single request by its ID. Includes a diff to what has changed in that request.",
			},
			Register: func(server *mcp.Server, tool *mcp.Tool) {
				mcp.AddTool(server, tool, obsCred.GetRequest)
			},
		},
		{
			Tool: &mcp.Tool{
				Name:        "list_archive_files",
				Description: "Content of an archive. Supported formats are cpio, tar.gz, tar.bz2, tar.xz and zip",
			},
			Register: func(server *mcp.Server, tool *mcp.Tool) {
				mcp.AddTool(server, tool, archiver.ListArchiveFiles)
			},
		},
		{
			Tool: &mcp.Tool{
				Name:        "extract_archive_files",
				Description: "Extract files from a cpio, tar.gz, tar.bz2, tar.xz or zip archive. If no files are given the complete archive is extracted",
			},
			Register: func(server *mcp.Server, tool *mcp.Tool) {
				mcp.AddTool(server, tool, archiver.ExtractArchiveFiles)
			},
		},
	}
	var allTools []string
	for _, tool := range tools {
		allTools = append(allTools, tool.Tool.Name)
	}
	if viper.GetBool("list-tools") {
		fmt.Printf("%s", strings.Join(allTools, ","))
		os.Exit(0)
	}
	var enabledTools []string
	if !pflag.CommandLine.Changed("enabled-tools") {
		enabledTools = allTools
	} else {
		enabledTools = viper.GetStringSlice("enabled-tools")
	}
	// register the enabled tools
	for _, tool := range tools {
		if slices.Contains(enabledTools, tool.Tool.Name) {
			tool.Register(server, tool.Tool)
		}
	}

	server.AddPrompt(&mcp.Prompt{
		Name:        "basic_information",
		Description: "Basic information about the tools and how they are used for the OpenBuild Server.",
	}, obsCred.PromptOSC)
	server.AddPrompt(&mcp.Prompt{
		Name:        "package_missing",
		Description: "Steps on what to do when a build failed because of a missing package.",
	}, obsCred.PromptPackage)
	server.AddPrompt(&mcp.Prompt{
		Name:        "service_usage",
		Description: "How to use OBS source services.",
	}, obsCred.Service)
	server.AddResource(&mcp.Resource{
		Name:        "spdx_licenses",
		MIMEType:    "text/plain",
		URI:         "SPDX",
		Description: "List of SPDX licenses which can be used a identifier.",
	}, licenses.GetLicenseIdentifiers)
	defaults, err := osc.ReadDefaults()
	if err != nil {
		slog.Warn("couldn't get defaults", "error", err)
	}
	for flavor, spec := range defaults.Specs {
		server.AddResource(&mcp.Resource{
			Name:        fmt.Sprintf("%s_spec", flavor),
			MIMEType:    "text/plain",
			URI:         fmt.Sprintf("spec/%s", flavor),
			Description: fmt.Sprintf("best practice rpm spec file for %s", flavor),
		}, func(ctx context.Context, rrr *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{
					{
						URI:      fmt.Sprintf("spec/%s", flavor),
						Text:     spec,
						MIMEType: "application/json",
					},
				},
			}, nil
		})
	}

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
