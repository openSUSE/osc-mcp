# MCP server for openBuild service
This project aims to offer a MCP service for the openBuild service. The Model Context Protocol (MCP) is an open-source framework designed to standardize how artificial intelligence systems, such as large language models (LLMs), connect and share data with external tools and data sources.

>[!NOTE]
>The project is in a very early stage, so things may break.
>It uses the credentials found in you configuration and keyring, so it acts under your name!

## Usage

Build the project with
```
  go build .
```

### [mctool](https://github.com/f/mcptools)

Use this tool for check the available tools.

### [mcphost](https://github.com/f/mcptools)

Create a configuration file `~/.mcphost.yml` and add the following lines to add the server
```
     osc-mcp:
       command: /home/chris/programming/github/openSUSE/osc-mcp/osc-mcp
       args: ["-workdir","/tmp/osc-mcp/","-clean-workdir"]
  
```

### [gemini-cli](https://github.com/google-gemini/gemini-cli)

Add following configuration to `~/.gemini/settings.json`
```
  "mcpServers": {
    "osc-mcp": {
      "command": "/home/chris/programming/github/openSUSE/osc-mcp/osc-mcp",
      "args": ["-workdir", "/tmp/osc-mcp","-clean-workdir"]
    }
  },
  "include-directories": ["/tmp/osc-mcp" ]
  
```


