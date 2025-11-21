// Package main is the entry point for the MCP server CLI application
package main

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/stacklok/toolhive/pkg/logger"

	"github.com/stacklok/toolhive-registry-server/cmd/thv-registry-mcp/app"
	"github.com/stacklok/toolhive-registry-server/internal/versions"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "thv-registry-mcp",
		Short: "ToolHive Registry MCP Server",
		Long: `ToolHive Registry MCP Server provides an MCP (Model Context Protocol) interface 
to the ToolHive Registry, enabling AI assistants to discover and query MCP servers.`,
		Version: versions.GetVersionInfo().Version,
	}

	// Add subcommands
	rootCmd.AddCommand(app.ServeCmd())
	rootCmd.AddCommand(app.VersionCmd())

	if err := rootCmd.Execute(); err != nil {
		logger.Errorf("Command failed: %v", err)
		os.Exit(1)
	}
}
