package tools

import (
	"embed"
)

// Embed static data files into the binary
// This ensures the MCP server works standalone without requiring
// external data files to be present on the filesystem.
// Works cross-platform: macOS, Linux, Windows
//
// Embedded files:
// - Features catalog (required for feature discovery)
// - Edition matrix (required for CE/EE detection)
// - KrakenD documentation (offline documentation search)
// - Bleve search index (pre-built for instant search)

//go:embed data/features/catalog.json
//go:embed data/editions/matrix.json
//go:embed data/docs/*
//go:embed data/search/index/*
var embeddedData embed.FS
