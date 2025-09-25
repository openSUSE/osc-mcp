package osc

import (
	"context"
	"log/slog"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (cred *OSCCredentials) PromptOSC(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	slog.Debug("PromptOSC was called")
	return &mcp.GetPromptResult{
		Description: "Source Package management in the OpenBuild Service. Source packages are bundles",
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: `Build and manage software in OpenBuild Service.
After a build a package can have several binary packages as a result.
Package builds happen offline, no software can be installed during package build.
A project can contain serveral source packages.
Project names most likely contains colons.
The remote home project name is "home:` + cred.Name + `
A package must be checked out before it can be compiled.
Packages and projects are checked out to ` + cred.TempDir + `
Packages must be checked out before they can be built.
Check remote log first for build failues, only built a package after it was modified.
`}},
		},
	}, nil
}

func (cred *OSCCredentials) PromptPackage(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	slog.Debug("PromptPackage was called")
	return &mcp.GetPromptResult{
		Description: "Error package not found.",
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: `If a package wasn't found check the log for which this error happends. Now the distributions can be searched for matching packages. At these packages to requires.`}},
		},
	}, nil
}
func (cred *OSCCredentials) Service(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
	slog.Debug("PromptPackage was called")
	return &mcp.GetPromptResult{
		Description: "Service usage",
		Messages: []*mcp.PromptMessage{
			{Role: "user", Content: &mcp.TextContent{Text: `Source services are defined in an XML file named _service.
This file contains a <services> root element, which holds one or more <service> elements.
Each <service> element specifies a name (referencing a script in /usr/lib/obs/service), a mode, and optional <param> elements for parameters.

Example _service file structure:
<services>
 <service name="MY_SCRIPT" mode="MODE">
  <param name="PARAMETER1">PARAMETER1_VALUE</param>
 </service>
</services>

If a Source: line in a spec file matches the pattern Source: https://github.com/foo/baar/v%{version}.tar.gz#./%{name}-%{version}.tar.gz, the download_files service can be used even without a _service file to download the source file. The go_modules service can also be run this way, and it downloads the vendor directory if a source archive is found in the directory.
`}},
		},
	}, nil
}
