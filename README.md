# MCP server for Open Build Service
This project aims to offer a MCP service for the [Open Build Service](https://openbuildservice.org/) (OBS). The Model Context Protocol (MCP) is an open-source framework designed to standardize how artificial intelligence systems, such as large language models (LLMs), connect and share data with external tools and data sources.

An example conversation is [here](example.md)

>[!NOTE]
>The project is in a very early stage, so things may break.

>[!CAUTION]
>It uses the credentials found in your configuration and keyring if not configured per command line in another way!

## Building

Build the project with
```
  go build .
```

## Usage

Start a server with

```
go run osc-mcp.go --http localhost:8666 --workdir /tmp/mcp/osc-mcp/ --clean-workdir

```
which uses preset temporary working directory.

You can now use `gemini-cli` or `mcphost` to access this server

## Reference prompts

Following prompts can be used and give you an example of what this tool can do

### Self packaging

```
Check at first https://github.com/openSUSE/osc-mcp online for the actual version and used language. Then package it as osc-mcp in home:mslacken:ml. Download the source via a service and make sure that files are                
correctly included in the spec file.                                                                                    
Build the package to confirm success. Between builds, check if services correctly donwloaded. If a download fails check online in the github repo if the version is correct.
After success commit the package.
```

You can see the result at [science:machinelearning:mcp/osc-mcp](https://build.opensuse.org/package/show/science:machinelearning:mcp/osc-mcp)

### [gemini-cli](https://github.com/google-gemini/gemini-cli)

Add the following configuration to `~/.gemini/settings.json`
```
  "mcpServers": {
    "osc-mcp": {
      "httpUrl": "http://localhost:8666"
    }
  },
  "include-directories": ["/tmp/mcp/osc-mcp" ]
  
```
and change to preset temporary directory `/tmp/mcp` as then `gemini-cli` can modify checked out sources.

```
  cd /tmp/mcp
  npx https://github.com/google-gemini/gemini-cli
```

Check the [example](example.md) for how a conversation looks like.

### [mcphost](https://github.com/f/mcptools)

Create a configuration file `~/.mcphost.yml` and add the following lines to add the server
```
     osc-mcp:
       command: /home/chris/programming/github/openSUSE/osc-mcp/osc-mcp
       args: ["-workdir","/tmp/osc-mcp/","-clean-workdir"]
  
```

# Available tools for the LLM

- **search_bundle**: Search bundles on remote OBS instance or local bundles.
- **list_source_files**: List source files of given bundle in local or remote location.
- **branch_bundle**: Branch a bundle and check it out as a local bundle.
- **build_bundle**: Build a source bundle.
- **get_project_meta**: Get the metadata of a project.
- **set_project_meta**: Set the metadata for the project.
- **create_bundle**: Create a new local bundle.
- **checkout_bundle**: Checkout a package from the online repository.
- **get_build_log**: Get the remote or local build log of a package.
- **search_packages**: Search the available packages for a remote repository.
- **commit**: Commits changed files.

# Useful tools

## [mctool](https://github.com/f/mcptools)

This program can be used to check the available tools.


# Logging

The MCP server includes comprehensive logging to help monitor operations and troubleshoot issues.

## Log Levels

Use the following flags to control logging verbosity:

- **Default (Warn)**: Only warnings and errors are logged
- **`-v` or `--verbose`**: Enable info-level logging for important operational events
- **`-d` or `--debug`**: Enable debug-level logging for detailed diagnostic information

## Log Output Options

### Standard Output (stderr)
By default, logs are written to stderr:
```bash
go run osc-mcp.go -v
```

### File Output
Direct logs to a file for persistent logging:
```bash
go run osc-mcp.go -v --logfile /var/log/osc-mcp.log
```

### JSON Format
For machine-readable logs (useful for log aggregation systems):
```bash
go run osc-mcp.go -v --log-json
```

JSON logging can be combined with file output:
```bash
go run osc-mcp.go -v --log-json --logfile /var/log/osc-mcp.json
```

## What Gets Logged

- **Info level** (`-v`): Major operations like builds, commits, checkouts, file uploads, and their durations
- **Debug level** (`-d`): API requests/responses, command execution details, file operations, and internal state
- **Warn level** (default): Non-fatal issues like missing configs or failed progress notifications
- **Error level** (always): Fatal errors with full context

## Example Usage

Monitor build operations with timing information:
```bash
go run osc-mcp.go -v --http localhost:8666 --workdir /tmp/mcp/osc-mcp/
```

Full debugging with JSON output to a file:
```bash
go run osc-mcp.go -d --log-json --logfile debug.json --http localhost:8666
```

# Other functionality

For all options use

```
go build osc-mcp.go --help
```

This project includes a parser for a build log and can output some more structured information. It can be built with
```
  go build tools/parse_log.go
```
and be used like
```
  osc lbl | parse_log
```
which analyzes/parses the last build log.
The parser can also retrieve remote build logs with
```
  parse_log -k dolly -p home:mslacken:p
```
