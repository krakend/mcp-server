package tools

import (
	"io/fs"
	"testing"
)

func TestMockDataProvider_ReadFile(t *testing.T) {
	mock := NewMockDataProvider()

	// Add a test file
	mock.AddFile("data/test.txt", []byte("test content"))

	// Read existing file
	content, err := mock.ReadFile("data/test.txt")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if string(content) != "test content" {
		t.Errorf("Expected 'test content', got: %s", string(content))
	}

	// Try to read non-existent file
	_, err = mock.ReadFile("data/missing.txt")
	if err != fs.ErrNotExist {
		t.Errorf("Expected fs.ErrNotExist, got: %v", err)
	}
}

func TestMockDataProvider_ReadDir(t *testing.T) {
	mock := NewMockDataProvider()

	// Add files in a directory
	mock.AddFile("data/docs/file1.txt", []byte("content1"))
	mock.AddFile("data/docs/file2.txt", []byte("content2"))

	// Read directory
	entries, err := mock.ReadDir("data/docs")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(entries) != 2 {
		t.Errorf("Expected 2 entries, got: %d", len(entries))
	}

	// Try to read non-existent directory
	_, err = mock.ReadDir("data/missing")
	if err != fs.ErrNotExist {
		t.Errorf("Expected fs.ErrNotExist, got: %v", err)
	}
}

func TestMockDataProvider_SetAndReset(t *testing.T) {
	// Create mock provider
	mock := NewMockDataProvider()
	mock.AddFile("data/test.json", []byte(`{"test": true}`))

	// Set as default
	originalProvider := defaultDataProvider
	defer func() {
		defaultDataProvider = originalProvider
	}()

	SetDefaultDataProvider(mock)

	// Verify it's being used
	content, err := defaultDataProvider.ReadFile("data/test.json")
	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}
	if string(content) != `{"test": true}` {
		t.Errorf("Expected test JSON, got: %s", string(content))
	}

	// Reset to default
	ResetDefaultDataProvider()

	// Verify reset worked (defaultDataProvider should be different now)
	if defaultDataProvider == mock {
		t.Error("Expected defaultDataProvider to be reset")
	}
}

func TestMockDirEntry(t *testing.T) {
	entry := &mockDirEntry{
		name:  "test.txt",
		isDir: false,
	}

	if entry.Name() != "test.txt" {
		t.Errorf("Expected name 'test.txt', got: %s", entry.Name())
	}

	if entry.IsDir() {
		t.Error("Expected file, got directory")
	}

	if entry.Type() == fs.ModeDir {
		t.Error("Expected file type, got directory type")
	}

	info, err := entry.Info()
	if err != nil {
		t.Fatalf("Expected no error from Info(), got: %v", err)
	}

	if info.Name() != "test.txt" {
		t.Errorf("Expected info name 'test.txt', got: %s", info.Name())
	}
}

func TestMockFileInfo(t *testing.T) {
	info := &mockFileInfo{
		name:  "test.txt",
		isDir: false,
	}

	if info.Name() != "test.txt" {
		t.Errorf("Expected name 'test.txt', got: %s", info.Name())
	}

	if info.IsDir() {
		t.Error("Expected file, got directory")
	}

	if info.Size() != 0 {
		t.Errorf("Expected size 0, got: %d", info.Size())
	}

	if info.Mode() != 0 {
		t.Errorf("Expected mode 0, got: %d", info.Mode())
	}

	if info.Sys() != nil {
		t.Error("Expected Sys() to return nil")
	}

	modTime := info.ModTime()
	if !modTime.IsZero() {
		t.Errorf("Expected zero time, got: %v", modTime)
	}
}
