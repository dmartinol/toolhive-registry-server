package app

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/stacklok/toolhive-registry-server/internal/versions"
)

// VersionCmd returns the version command
func VersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Long:  "Print detailed version information about the MCP server",
		Run:   runVersion,
	}

	cmd.Flags().Bool("json", false, "Output version information in JSON format")

	return cmd
}

func runVersion(cmd *cobra.Command, _ []string) {
	info := versions.GetVersionInfo()

	jsonOutput, _ := cmd.Flags().GetBool("json")

	if jsonOutput {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(info); err != nil {
			fmt.Fprintf(os.Stderr, "Error encoding version info: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Printf("ToolHive Registry MCP Server\n")
		fmt.Printf("Version:    %s\n", info.Version)
		fmt.Printf("Commit:     %s\n", info.Commit)
		fmt.Printf("Build Date: %s\n", info.BuildDate)
		fmt.Printf("Go Version: %s\n", info.GoVersion)
		fmt.Printf("Platform:   %s\n", info.Platform)
	}
}
