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
	obsCred, err := osc.UseKeyring(*oscInstance)
	if err != nil {
		slog.Error("failed to get OBS credentials", slog.Any("error", err))
		os.Exit(1)
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        "search_packages",
		Description: "Search packages on remote instance.",
	}, obsCred.SearchSrcPkg)
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
