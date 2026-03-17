package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/krakend/mcp-server/internal/usage"
	"github.com/krakend/mcp-server/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	version         = "0.6.3"
	defaultHttpPort = "8090"
	serverName      = "krakend-mcp-server"
	description     = "MCP server for KrakenD API Gateway configuration assistance"
)

func main() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		select {
		case sig := <-sigs:
			log.Println("Signal intercepted:", sig)
			cancel()
		case <-ctx.Done():
		}
	}()

	serveMode := false
	if len(os.Args) > 1 {
		if os.Args[1] == "--version" {
			fmt.Printf("%s version %s\n", serverName, version)
			os.Exit(0)
		}

		serveMode = os.Args[1] == "--http"
	}

	// Set up logging to stderr (MCP uses stdout for protocol)
	log.SetOutput(os.Stderr)
	log.Printf("%s v%s starting...", serverName, version)

	var reporter usage.Reporter
	if os.Getenv("USAGE_DISABLE") == "1" {
		reporter = usage.NewNoopReporter()
	} else {
		r, err := usage.NewReporter(serverName, version, os.Getenv("USAGE_URL"))
		if err != nil {
			r = usage.NewNoopReporter()
			log.Printf("Failed to create usage reporter: %v", err)
		}
		reporter = r
	}

	// Create MCP server
	server := createMCPServer()

	server.AddReceivingMiddleware(usage.NewUsageMethodHandlerFactory(ctx, reporter))

	// Register all tools, resources, and prompts
	if err := registerTools(server); err != nil {
		log.Fatalf("Failed to register tools: %v", err)
	}
	if err := registerResources(server); err != nil {
		log.Fatalf("Failed to register resources: %v", err)
	}
	if err := registerPrompts(server); err != nil {
		log.Fatalf("Failed to register prompts: %v", err)
	}

	// Set up cleanup on shutdown
	defer func() {
		if err := tools.CloseDocSearch(); err != nil {
			log.Printf("Error closing doc search: %v", err)
		}
	}()

	if !serveMode {
		log.Printf("✓ Running in stdio mode")
		if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
			if err == context.Canceled {
				log.Printf("Server gracefully stopped")
				os.Exit(0)
			}
			log.Fatalf("Server run error: %v", err)
		}
		os.Exit(0)
	}

	mcpHandler := mcp.NewStreamableHTTPHandler(
		func(_ *http.Request) *mcp.Server {
			return server
		},
		&mcp.StreamableHTTPOptions{
			Stateless:    false,
			JSONResponse: true,
		},
	)

	httpHandler := http.HandlerFunc(mcpHandler.ServeHTTP)

	mux := http.NewServeMux()

	// Using old router matcher to pass all methods to MCP handler
	mux.HandleFunc("/", httpHandler)

	s := &http.Server{
		Addr:    ":" + defaultHttpPort,
		Handler: mux,
	}

	go func() {
		log.Printf("✓ Starting server on %s", s.Addr)
		s.ListenAndServe()
	}()

	<-ctx.Done()
	log.Printf("Shutting down server...")
	s.Shutdown(ctx)
	log.Printf("Server gracefully stopped")
}

// createMCPServer initializes the MCP server
func createMCPServer() *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    serverName,
			Title:   description,
			Version: version,
		},
		nil,
	)

	log.Printf("Server initialized: %s v%s", serverName, version)
	return server
}

// registerTools registers all MCP tools
func registerTools(server *mcp.Server) error {
	toolCount := 0

	// Phase 1: Core validation tools (2 tools)
	if err := tools.RegisterValidationTools(server); err != nil {
		return fmt.Errorf("failed to register validation tools: %w", err)
	}
	toolCount += 2

	// Phase 1: Runtime detection tool (1 tool)
	tools.RegisterRuntimeTools(server)
	toolCount++

	// Phase 1: Documentation search tools (2 tools)
	if err := tools.RegisterDocSearchTools(server); err != nil {
		log.Printf("Warning: Failed to register doc search tools: %v", err)
		log.Printf("Documentation search will be unavailable")
	} else {
		toolCount += 2
	}

	// Phase 1: Feature detection tools (2 tools)
	if err := tools.RegisterFeatureTools(server); err != nil {
		return fmt.Errorf("failed to register feature tools: %w", err)
	}
	toolCount += 2

	log.Printf("✓ All tools registered: %d tools (validation + runtime + features + generation + doc search)", toolCount)
	return nil
}

// registerResources registers all MCP resources
func registerResources(server *mcp.Server) error {
	// TODO: Register schema resources
	// TODO: Register feature catalog resources
	// TODO: Register examples resources
	// TODO: Register best practices resources
	// TODO: Register migration guides resources

	log.Printf("Resources registered: 0 (TODO - implementation pending)")
	return nil
}

// registerPrompts registers all MCP prompts
func registerPrompts(server *mcp.Server) error {
	// TODO: Register validation workflow prompts
	// TODO: Register creation workflow prompts
	// TODO: Register feature addition prompts
	// TODO: Register migration prompts
	// TODO: Register optimization prompts
	// TODO: Register security audit prompts

	log.Printf("Prompts registered: 0 (TODO - implementation pending)")
	return nil
}
