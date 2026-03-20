package tools

import (
	"embed"
	"io/fs"
)

// Embed static data files into the binary.
// Works cross-platform: macOS, Linux, Windows
//
// Embedded files:
// - KrakenD documentation (offline documentation search)
// - Bleve search index (pre-built for instant search)
// - Feature matrix YAML (offline feature discovery; downloaded by build.sh)

//go:embed data/docs/*
//go:embed data/search/index/*
//go:embed all:data/features
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
