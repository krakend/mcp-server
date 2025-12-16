package tools

import (
	"io/fs"
	"path"
	"strings"
	"time"
)

// MockDataProvider implements DataProvider for testing.
// It uses an in-memory map to simulate file storage without requiring
// actual files or embedded data to be present.
type MockDataProvider struct {
	files map[string][]byte
}

// NewMockDataProvider creates a new mock data provider for testing.
func NewMockDataProvider() *MockDataProvider {
	return &MockDataProvider{
		files: make(map[string][]byte),
	}
}

// AddFile adds a file to the mock provider.
func (m *MockDataProvider) AddFile(name string, content []byte) {
	m.files[name] = content
}

// ReadFile reads a file from the mock storage.
func (m *MockDataProvider) ReadFile(name string) ([]byte, error) {
	content, exists := m.files[name]
	if !exists {
		return nil, fs.ErrNotExist
	}
	return content, nil
}

// ReadDir reads a directory from the mock storage.
// It returns entries for all files that have the directory as a prefix.
func (m *MockDataProvider) ReadDir(name string) ([]fs.DirEntry, error) {
	var entries []fs.DirEntry
	seen := make(map[string]bool)

	for filePath := range m.files {
		// Check if file is in this directory
		dir := path.Dir(filePath)
		if dir == name || (name == "." && dir == ".") {
			baseName := path.Base(filePath)
			if !seen[baseName] {
				entries = append(entries, &mockDirEntry{
					name:  baseName,
					isDir: false,
				})
				seen[baseName] = true
			}
		} else if len(name) < len(dir) && dir[:len(name)] == name {
			// This is a subdirectory
			remaining := dir[len(name):]
			if len(remaining) > 0 && remaining[0] == '/' {
				remaining = remaining[1:]
			}
			parts := strings.Split(remaining, "/")
			if len(parts) > 0 && parts[0] != "" {
				subDir := parts[0]
				if !seen[subDir] {
					entries = append(entries, &mockDirEntry{
						name:  subDir,
						isDir: true,
					})
					seen[subDir] = true
				}
			}
		}
	}

	if len(entries) == 0 {
		return nil, fs.ErrNotExist
	}

	return entries, nil
}

// mockDirEntry implements fs.DirEntry for testing.
type mockDirEntry struct {
	name  string
	isDir bool
}

func (e *mockDirEntry) Name() string {
	return e.name
}

func (e *mockDirEntry) IsDir() bool {
	return e.isDir
}

func (e *mockDirEntry) Type() fs.FileMode {
	if e.isDir {
		return fs.ModeDir
	}
	return 0
}

func (e *mockDirEntry) Info() (fs.FileInfo, error) {
	return &mockFileInfo{
		name:  e.name,
		isDir: e.isDir,
	}, nil
}

// mockFileInfo implements fs.FileInfo for testing.
type mockFileInfo struct {
	name  string
	isDir bool
}

func (i *mockFileInfo) Name() string       { return i.name }
func (i *mockFileInfo) Size() int64        { return 0 }
func (i *mockFileInfo) Mode() fs.FileMode  { return 0 }
func (i *mockFileInfo) ModTime() time.Time { return time.Time{} }
func (i *mockFileInfo) IsDir() bool        { return i.isDir }
func (i *mockFileInfo) Sys() interface{}   { return nil }

// SetDefaultDataProvider sets the default data provider for the package.
// This is useful for testing to inject a mock provider.
func SetDefaultDataProvider(provider DataProvider) {
	defaultDataProvider = provider
}

// ResetDefaultDataProvider resets the default provider to use embedded data.
func ResetDefaultDataProvider() {
	defaultDataProvider = NewEmbeddedDataProvider()
}
