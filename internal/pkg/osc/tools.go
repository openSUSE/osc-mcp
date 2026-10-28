package osc

import (
	"fmt"
)

type ToolDef struct {
	Name        string
	Description string
	InputSchema any
	Handler     any
}

func (c *OSCCredentials) GetTools() []ToolDef {
	return []ToolDef{
		{
			Name:        "search_bundle",
			Description: fmt.Sprintf("Search bundles on remote open build (OBS) instance %s or local bundles. A bundle is also known as source package. Use the project name 'local' to list local packages. If project and bundle name is empty local packages will be listed. A bundle must be built to create installable packages.", c.Apiaddr),
			Handler:     c.SearchSrcBundle,
		},
		{
			Name:        "list_source_files",
			Description: "List source files of given bundle in local or remote location. Also returns basic information of the files and if they are modified locally. The content of small files is returned and also the content of all relevant control files which are files with .spec and .kiwi suffix. Prefer this tool read command file before checking them out. If a file name is given only the requested file is shown, regardless it's size.",
			Handler:     c.ListSrcFiles,
		},
		{
			Name:        "branch_bundle",
			Description: fmt.Sprintf("Branch a bundle and check it out as local bundle under the path %s", c.TempDir),
			Handler:     c.BranchBundle,
		},
		{
			Name:        "run_build",
			Description: "Build a source bundle also known as source package. A build is awlays local and withoout any online connection. All source files and software has to be downloaded and provided in advance.",
			Handler:     c.Build,
		},
		{
			Name:        "run_services",
			Description: "Run OBS source services on a specified project and bundle. Important services are: download_files: downloads the source files reference via an URI in the spec file with the pattern https://github.com/foo/baar/v%{version}.tar.gz#./%{name}-%{version}.tar.gz, go_modules: which creates a vendor directory for go files if the source has the same name as the project.",
			Handler:     c.RunServices,
		},
		{
			Name:        "get_project_meta",
			Description: "Get the metadata of a project. The metadata defines for which project a source bundle can be built the bundles inside the project. The subprojects of the projects are also listed. Project and sub project names are separated with colons.",
			Handler:     c.GetProjectMeta,
		},
		{
			Name:        "set_project_meta",
			Description: "Set the metadata for the project. Create the project if it doesn't exist.",
			Handler:     c.SetProjectMeta,
		},
		{
			Name:        "create",
			Description: "Create a new local bundle or _service/.spec file. Will also create a project or bundle if it does not exist. Before commit this package can't be checked out. Prefer creating _service files with this tool.",
			InputSchema: CreateBundleInputSchema(),
			Handler:     c.Create,
		},
		{
			Name:        "checkout_bundle",
			Description: fmt.Sprintf("Checkout a bundle from the online repository. After this step the package is available as local package under %s. Check out a single package instead of the complete repository if possible,", c.TempDir),
			Handler:     c.CheckoutBundle,
		},
		{
			Name:        "get_build_log",
			Description: "Get the remote or local build log of a package.",
			Handler:     c.BuildLog,
		},
		{
			Name:        "search_packages",
			Description: "Search the available packages for a remote repository. This are the already built packages and are required by bundles or source packages for building.",
			Handler:     c.SearchPackages,
		},
		{
			Name:        "commit",
			Description: "Commits changed files. If a .spec file is staged, the corresponding .changes file will be updated or created accordingly to input.",
			Handler:     c.Commit,
		},
		{
			Name:        "list_requests",
			Description: fmt.Sprintf("Get a list of requests. Need to set one of the following: user, group, project, package, state, reviewstates, types, ids. If not package group or ids ist set %s will be set for user.", c.Name),
			Handler:     c.ListRequests,
		},
		{
			Name:        "get_request",
			Description: "Get a single request by its ID. Includes a diff to what has changed in that request.",
			Handler:     c.GetRequest,
		},
		{
			Name:        "edit_file",
			Description: fmt.Sprintf("Write or update a file in a checked out package directory. The directory must be under %s. Use this to modify spec files or other source files after branching.", c.TempDir),
			Handler:     c.EditFile,
		},
	}
}
