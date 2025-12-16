package tools

import (
	"embed"
	"io/fs"
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
var embeddedFS embed.FS

// embeddedDataProvider implements DataProvider using embed.FS.
// This is the production implementation that uses actual embedded files.
type embeddedDataProvider struct {
	fs embed.FS
}

// NewEmbeddedDataProvider creates a production DataProvider that uses embedded files.
func NewEmbeddedDataProvider() DataProvider {
	return &embeddedDataProvider{fs: embeddedFS}
}

// ReadFile reads the named file from the embedded filesystem.
func (p *embeddedDataProvider) ReadFile(name string) ([]byte, error) {
	return p.fs.ReadFile(name)
}

// ReadDir reads the named directory from the embedded filesystem.
func (p *embeddedDataProvider) ReadDir(name string) ([]fs.DirEntry, error) {
	return p.fs.ReadDir(name)
}

// Default provider used by package-level functions (for backward compatibility)
var defaultDataProvider DataProvider = NewEmbeddedDataProvider()
