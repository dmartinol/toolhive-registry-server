// Package app provides the CLI application commands for the MCP server
package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stacklok/toolhive/pkg/logger"

	"github.com/stacklok/toolhive-registry-server/internal/mcp"
)

const (
	defaultGracefulTimeout = 30 * time.Second
	defaultTransport       = "http"
)

// ServeCmd returns the serve command for the MCP server
func ServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server",
		Long: `Start the MCP (Model Context Protocol) server to provide AI assistants 
with access to the ToolHive Registry through MCP tools.

The server connects to an existing Registry API server (--registry-url) and acts
as a stateless MCP-to-REST bridge. The Registry API server must be running and
accessible at the specified URL.

Transport modes:
- http: Standard HTTP JSON-RPC (default)
- stdio: Standard input/output for direct MCP client connections`,
		RunE: runServe,
	}

	// Define flags
	cmd.Flags().String("registry-url", "", "URL of the Registry API server (required)")
	cmd.Flags().String("address", ":8081", "Address to listen on (HTTP mode)")
	cmd.Flags().String("transport", defaultTransport, "Transport mode: http or stdio")

	// Bind flags to viper
	_ = viper.BindPFlag("registry.url", cmd.Flags().Lookup("registry-url"))
	_ = viper.BindPFlag("mcp.address", cmd.Flags().Lookup("address"))
	_ = viper.BindPFlag("mcp.transport", cmd.Flags().Lookup("transport"))

	// Mark registry-url as required
	_ = cmd.MarkFlagRequired("registry-url")

	return cmd
}

func runServe(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	// Get registry URL
	registryURL := viper.GetString("registry.url")
	if registryURL == "" {
		return fmt.Errorf("registry URL is required (use --registry-url)")
	}

	logger.Infof("Connecting to Registry API at %s", registryURL)

	// Verify Registry API is accessible
	if err := verifyRegistryAPI(ctx, registryURL); err != nil {
		return fmt.Errorf("failed to connect to Registry API: %w", err)
	}

	logger.Info("Successfully connected to Registry API")

	// Create MCP server using SDK with Registry API client
	mcpServer := mcp.NewServer(registryURL)
	sdkServer := mcpServer.GetSDKServer()

	// Get transport mode
	transportMode := viper.GetString("mcp.transport")

	switch transportMode {
	case "stdio":
		return runStdioMode(ctx, sdkServer)
	case "http":
		return runHTTPMode(ctx, sdkServer)
	default:
		return fmt.Errorf("unsupported transport mode: %s (use 'http' or 'stdio')", transportMode)
	}
}

// verifyRegistryAPI checks if the Registry API is accessible
func verifyRegistryAPI(ctx context.Context, registryURL string) error {
	client := &http.Client{Timeout: 5 * time.Second}

	// Try to fetch the servers list to verify connectivity
	req, err := http.NewRequestWithContext(ctx, "GET", registryURL+"/v0/servers?limit=1", nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect: %w (is the Registry API server running?)", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

func runStdioMode(ctx context.Context, sdkServer *sdkmcp.Server) error {
	logger.Info("Starting MCP server in stdio mode")

	// Create a cancellable context for graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle interrupt signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Run SDK stdio transport in a goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- sdkServer.Run(ctx, &sdkmcp.StdioTransport{})
	}()

	// Wait for either completion or interrupt
	select {
	case err := <-errChan:
		if err != nil {
			return fmt.Errorf("stdio transport error: %w", err)
		}
		return nil
	case sig := <-sigChan:
		logger.Infof("Received signal %v, shutting down", sig)
		cancel()
		// Wait for graceful shutdown
		select {
		case err := <-errChan:
			return err
		case <-time.After(defaultGracefulTimeout):
			return fmt.Errorf("shutdown timeout exceeded")
		}
	}
}

func runHTTPMode(ctx context.Context, sdkServer *sdkmcp.Server) error {
	address := viper.GetString("mcp.address")
	logger.Infof("Starting MCP server in HTTP mode on %s", address)

	// Create SDK StreamableHTTPHandler
	handler := sdkmcp.NewStreamableHTTPHandler(func(_ *http.Request) *sdkmcp.Server {
		return sdkServer
	}, nil)

	// Create HTTP server
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Start server in goroutine
	serverErrors := make(chan error, 1)
	go func() {
		logger.Infof("MCP server listening on %s", address)
		serverErrors <- server.ListenAndServe()
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		return fmt.Errorf("server error: %w", err)
	case sig := <-quit:
		logger.Infof("Received signal %v, shutting down gracefully", sig)

		// Create shutdown context with timeout
		shutdownCtx, cancel := context.WithTimeout(ctx, defaultGracefulTimeout)
		defer cancel()

		// Attempt graceful shutdown
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown failed: %w", err)
		}

		logger.Info("MCP server stopped gracefully")
		return nil
	}
}
