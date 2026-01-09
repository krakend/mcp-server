package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/krakend/mcp-server/tools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	version     = "0.6.3"
	serverName  = "krakend-mcp-server"
	description = "MCP server for KrakenD API Gateway configuration assistance"
)

func main() {
	// Handle version flag
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Printf("%s version %s\n", serverName, version)
		os.Exit(0)
	}

	// Set up logging to stderr (MCP uses stdout for protocol)
	log.SetOutput(os.Stderr)
	log.Printf("%s v%s starting...", serverName, version)

	// Create MCP server
	server := createMCPServer()

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

	log.Printf("✓ Server ready and waiting for connections")

	// Set up cleanup on shutdown
	defer func() {
		if err := tools.CloseDocSearch(); err != nil {
			log.Printf("Error closing doc search: %v", err)
		}
	}()

	// Run server with stdio transport
	ctx := context.Background()
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// createMCPServer initializes the MCP server
func createMCPServer() *mcp.Server {
	server := mcp.NewServer(
		&mcp.Implementation{
			Name:    serverName,
			Version: version,
		},
		nil, // Default options
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

	// Phase 1: Generation tools (4 tools)
	if err := tools.RegisterGenerationTools(server); err != nil {
		return fmt.Errorf("failed to register generation tools: %w", err)
	}
	toolCount += 4

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
