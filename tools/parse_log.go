package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"

	"github.com/olekukonko/tablewriter"
	"github.com/openSUSE/osc-mcp/internal/pkg/buildlog"
	"github.com/openSUSE/osc-mcp/internal/pkg/osc"
	"github.com/spf13/cobra"
)

var jsonOutput bool
var project, pkg, arch, distro string
var nrLines int
var printSucceeded bool

var rootCmd = &cobra.Command{
	Use:   "parse_log [file]",
	Short: "Parse a build log and display a summary.",
	Long:  `Parses a build log from a file or stdin and displays a summary of the build phases.`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var content []byte
		var err error

		if project != "" && pkg != "" {
			creds, err := osc.GetCredentials()
			if err != nil {
				slog.Error("couldn't get osc credentials", "error", err)
				os.Exit(1)
			}
			logContent, err := creds.GetBuildLogRaw(context.Background(), project, distro, arch, pkg)
			if err != nil {
				slog.Error("couldn't fetch remote build log", "error", err)
				os.Exit(1)
			}
			content = []byte(logContent)
		} else {
			var reader io.Reader
			if len(args) < 1 {
				reader = os.Stdin
			} else {
				file, err := os.Open(args[0])
				if err != nil {
					slog.Error("couldn't read input file", "error", err)
					os.Exit(1)
				}
				defer file.Close()
				reader = file
			}

			content, err = io.ReadAll(reader)
			if err != nil {
				slog.Error("couldn't read content", "error", err)
				os.Exit(1)
			}
		}

		log := buildlog.Parse(string(content))
		if err != nil {
			slog.Error("failed to parse log", "error", err)
			os.Exit(1)
		}

		if jsonOutput {
			jsonResult, err := json.MarshalIndent(log.FormatJson(nrLines, printSucceeded), "", "  ")
			if err != nil {
				slog.Error("failed to marshal to json", "error", err)
				os.Exit(1)
			}
			fmt.Println(string(jsonResult))
		} else {
			fmt.Printf("Parsed build log for %s/%s on %s/%s\n", log.Project, log.Name, log.Distro, log.Arch)
			table := tablewriter.NewWriter(os.Stdout)
			table.SetHeader([]string{"Phase", "Duration (s)", "Lines"})

			for phase, phaseDetails := range log.Phases {
				table.Append([]string{
					phase.String(),
					strconv.Itoa(phaseDetails.Duration),
					strconv.Itoa(len(phaseDetails.Lines)),
				})
			}
			table.Render()
		}
	},
}

func init() {
	rootCmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "output in JSON format")
	rootCmd.Flags().StringVarP(&project, "project", "p", "", "project to fetch build log from")
	rootCmd.Flags().StringVarP(&pkg, "package", "k", "", "package to fetch build log from")
	rootCmd.Flags().StringVarP(&arch, "arch", "a", "x86_64", "architecture to fetch build log for")
	rootCmd.Flags().StringVarP(&distro, "distro", "d", "openSUSE_Tumbleweed", "distribution to fetch build log for")
	rootCmd.Flags().IntVarP(&nrLines, "lines", "l", 100, "Number of log lines to print")
	rootCmd.Flags().BoolVarP(&printSucceeded, "succeeded", "s", false, "print also the lines of succeeded phases")
}

func main() {
	if err := rootCmd.Execute(); err != nil {

		fmt.Println(err)
		os.Exit(1)
	}
}
