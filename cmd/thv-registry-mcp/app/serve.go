package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stacklok/toolhive/pkg/logger"

	"github.com/stacklok/toolhive-registry-server/internal/config"
	"github.com/stacklok/toolhive-registry-server/internal/mcp"
	"github.com/stacklok/toolhive-registry-server/internal/service"
	"github.com/stacklok/toolhive-registry-server/internal/service/inmemory"
	"github.com/stacklok/toolhive-registry-server/internal/sources"
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

The server requires a configuration file (--config) that specifies the registry
data source (Git, API, or File). See examples/ directory for sample configurations.

Transport modes:
- http: Standard HTTP JSON-RPC (default)
- stdio: Standard input/output for direct MCP client connections`,
		RunE: runServe,
	}

	// Define flags
	cmd.Flags().String("address", ":8081", "Address to listen on (HTTP mode)")
	cmd.Flags().String("config", "", "Path to configuration file (YAML format, required)")
	cmd.Flags().String("transport", defaultTransport, "Transport mode: http or stdio")

	// Bind flags to viper
	_ = viper.BindPFlag("mcp.address", cmd.Flags().Lookup("address"))
	_ = viper.BindPFlag("config", cmd.Flags().Lookup("config"))
	_ = viper.BindPFlag("mcp.transport", cmd.Flags().Lookup("transport"))

	// Mark config as required
	_ = cmd.MarkFlagRequired("config")

	return cmd
}

func runServe(_ *cobra.Command, _ []string) error {
	ctx := context.Background()

	// Load and validate configuration
	configPath := viper.GetString("config")
	cfg, err := config.LoadConfig(
		config.WithConfigPath(configPath),
	)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	logger.Infof("Loaded configuration from %s (registry: %s, source: %s)",
		configPath, cfg.GetRegistryName(), cfg.Source.Type)

	// Initialize registry service
	registryService, err := initializeRegistryService(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize registry service: %w", err)
	}

	// Create MCP server
	mcpServer := mcp.NewServer(registryService)
	transport := mcp.NewTransport(mcpServer)

	// Get transport mode
	transportMode := viper.GetString("mcp.transport")

	switch transportMode {
	case "stdio":
		return runStdioMode(ctx, transport)
	case "http":
		return runHTTPMode(ctx, transport)
	default:
		return fmt.Errorf("unsupported transport mode: %s (use 'http' or 'stdio')", transportMode)
	}
}

func initializeRegistryService(ctx context.Context, cfg *config.Config) (service.RegistryService, error) {
	// Create data directory if needed
	dataDir := "./data"
	if err := os.MkdirAll(dataDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	// Create storage manager
	storageManager := sources.NewFileStorageManager(dataDir)

	// Create registry provider using factory
	factory := service.NewRegistryProviderFactory(storageManager)
	provider, err := factory.CreateProvider(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry provider: %w", err)
	}

	// Create service
	svc, err := inmemory.New(ctx, provider)
	if err != nil {
		return nil, fmt.Errorf("failed to create registry service: %w", err)
	}

	return svc, nil
}

func runStdioMode(ctx context.Context, transport *mcp.Transport) error {
	logger.Info("Starting MCP server in stdio mode")

	// Create a cancellable context for graceful shutdown
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle interrupt signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Run stdio transport in a goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- transport.ServeStdio(ctx, os.Stdin, os.Stdout)
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

func runHTTPMode(ctx context.Context, transport *mcp.Transport) error {
	address := viper.GetString("mcp.address")
	logger.Infof("Starting MCP server in HTTP mode on %s", address)

	// Create HTTP router
	r := chi.NewRouter()

	// Add middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)

	// MCP endpoints
	r.Post("/", transport.ServeHTTP)
	r.Get("/sse", transport.ServeSSE)
	r.Post("/jsonrpc", transport.ServeJSONRPC)

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	})

	// Create HTTP server
	server := &http.Server{
		Addr:              address,
		Handler:           r,
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

