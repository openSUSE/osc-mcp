package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"

	"github.com/olekukonko/tablewriter"
	"github.com/openSUSE/osc-mcp/internal/pkg/buildlog"
	"github.com/spf13/cobra"
)

var jsonOutput bool

var rootCmd = &cobra.Command{
	Use:   "parse_log [file]",
	Short: "Parse a build log and display a summary.",
	Long:  `Parses a build log from a file or stdin and displays a summary of the build phases.`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var reader io.Reader
		var err error

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

		content, err := io.ReadAll(reader)
		if err != nil {
			slog.Error("couldn't read content", "error", err)
			os.Exit(1)
		}

		log, err := buildlog.ParseLog(string(content))
		if err != nil {
			slog.Error("failed to parse log", "error", err)
			os.Exit(1)
		}

		if jsonOutput {
			jsonResult, err := json.MarshalIndent(log.FormatJson(), "", "  ")
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
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
