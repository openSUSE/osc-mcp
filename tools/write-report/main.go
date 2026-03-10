package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func main() {
	// Command line flags
	pflag.String("http", "", "if set, use streamable HTTP at this address, instead of stdin/stdout")
	pflag.StringP("file", "f", "", "output file path")
	pflag.Bool("overwrite", false, "overwrite the file if it exists")
	pflag.Int("max-size", 100, "maximum file size in kilobytes")
	pflag.String("logfile", "", "if set, log to this file instead of stderr")
	pflag.BoolP("verbose", "v", false, "Enable verbose logging")
	pflag.BoolP("debug", "d", false, "Enable debug logging")
	pflag.Bool("log-json", false, "Output logs in JSON format (machine-readable)")

	pflag.Parse()
	viper.SetEnvPrefix("WRITE_REPORT")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()
	viper.BindPFlags(pflag.CommandLine)

	if viper.GetString("file") == "" {
		slog.Error("file path must be specified with --file")
		os.Exit(1)
	}

	// Logger setup
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

	if viper.GetBool("log-json") {
		logger = slog.New(slog.NewJSONHandler(logOutput, handlerOpts))
	} else {
		logger = slog.New(slog.NewTextHandler(logOutput, handlerOpts))
	}
	slog.SetDefault(logger)

	// MCP Server setup
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "write-report-tool",
		Version: "0.2.1"},
		&mcp.ServerOptions{
			InitializedHandler: func(ctx context.Context, req *mcp.InitializedRequest) {
				slog.Info("Session started", "ID", req.Session.ID())
			},
		})

	// Tool definition
	writeReportTool := &mcp.Tool{
		Name:        "write_report",
		Description: "Writes text content to a file.",
	}

	// Input and Output parameter definition
	type WriteReportParams struct {
		Content string `json:"content" jsonschema:"The text content to write."`
	}
	type WriteReportResult struct {
		Success bool `json:"success"`
	}

	// Tool implementation
	writeReportFunc := func(ctx context.Context, req *mcp.CallToolRequest, params WriteReportParams) (*mcp.CallToolResult, *WriteReportResult, error) {
		filePath := viper.GetString("file")
		overwrite := viper.GetBool("overwrite")
		maxSize := viper.GetInt("max-size") * 1024 // convert to bytes

		if len(params.Content) > maxSize {
			return nil, nil, fmt.Errorf("content size (%d bytes) exceeds the maximum allowed size (%d bytes)", len(params.Content), maxSize)
		}

		// Determine file opening flags
		openFlags := os.O_WRONLY | os.O_CREATE
		if _, err := os.Stat(filePath); err == nil {
			// file exists
			if overwrite {
				openFlags |= os.O_TRUNC // Overwrite (truncate)
			} else {
				openFlags |= os.O_APPEND // Append
			}
		} else if !os.IsNotExist(err) {
			// some other error with stat
			return nil, nil, fmt.Errorf("failed to check file status for '%s': %w", filePath, err)
		}

		file, err := os.OpenFile(filePath, openFlags, 0644)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to open file '%s': %w", filePath, err)
		}
		defer file.Close()

		if _, err := file.Write([]byte(params.Content)); err != nil {
			return nil, nil, fmt.Errorf("failed to write to file '%s': %w", filePath, err)
		}

		return nil, &WriteReportResult{
			Success: true,
		}, nil
	}

	mcp.AddTool(server, writeReportTool, writeReportFunc)

	// Start server
	if viper.GetString("http") != "" {
		handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
			return server
		}, nil)
		slog.Info("MCP handler listening at", slog.String("address", viper.GetString("http")))
		http.ListenAndServe(viper.GetString("http"), handler)
	} else {
		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			slog.Error("Server failed", slog.Any("error", err))
		}
	}
}
