package tools

import (
	"io/fs"
)

// DataProvider defines the interface for accessing embedded data files.
// This abstraction allows for dependency injection and makes the code testable
// without requiring actual embedded files to be present.
//
// Implementations:
//   - embeddedDataProvider: Uses embed.FS for production (real embedded files)
//   - mockDataProvider: Uses in-memory map for testing
type DataProvider interface {
	// ReadFile reads the named file and returns its contents.
	// The name is relative to the data root (e.g., "data/docs/llms-full.txt").
	ReadFile(name string) ([]byte, error)

	// ReadDir reads the named directory and returns its entries.
	// The name is relative to the data root (e.g., "data/search/index").
	ReadDir(name string) ([]fs.DirEntry, error)
}
