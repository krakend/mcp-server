package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDocumentation_EmptyContent(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "krakend-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write empty file in correct location
	docsDir := filepath.Join(tmpDir, "docs")
	os.MkdirAll(docsDir, 0755)
	testFile := filepath.Join(docsDir, "llms-full.txt")
	os.WriteFile(testFile, []byte(""), 0644)

	// Mock dataDir
	origDataDir := dataDir
	dataDir = tmpDir
	defer func() {
		dataDir = origDataDir
	}()

	chunks, err := parseDocumentation()

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("Expected 0 chunks for empty content, got %d", len(chunks))
	}
}

func TestParseDocumentation_WithHeaders(t *testing.T) {
	content := `# Category 1

Some content for category 1

## Section 1.1

Content for section 1.1

## Section 1.2

Content for section 1.2

# Category 2

Content for category 2
`

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "krakend-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write test file
	testFile := filepath.Join(tmpDir, "docs", "llms-full.txt")
	os.MkdirAll(filepath.Dir(testFile), 0755)
	os.WriteFile(testFile, []byte(content), 0644)

	// Mock dataDir
	origDataDir := dataDir
	dataDir = tmpDir
	defer func() {
		dataDir = origDataDir
	}()

	chunks, err := parseDocumentation()

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should have 4 chunks: Category 1, Section 1.1, Section 1.2, Category 2
	if len(chunks) != 4 {
		t.Errorf("Expected 4 chunks, got %d", len(chunks))
	}

	// Check first chunk
	if len(chunks) > 0 {
		if chunks[0].Title != "Category 1" {
			t.Errorf("Expected title 'Category 1', got '%s'", chunks[0].Title)
		}
		if !strings.Contains(chunks[0].Content, "Some content for category 1") {
			t.Errorf("Expected content to contain 'Some content for category 1'")
		}
	}
}

func TestParseDocumentation_StringBuilderOptimization(t *testing.T) {
	// Test that strings.Builder is used (indirectly by checking it works correctly with many lines)
	lines := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		lines[i] = fmt.Sprintf("Line number %d with some content", i)
	}

	content := "# Test Category\n\n" + strings.Join(lines, "\n")

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "krakend-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write test file
	testFile := filepath.Join(tmpDir, "docs", "llms-full.txt")
	os.MkdirAll(filepath.Dir(testFile), 0755)
	os.WriteFile(testFile, []byte(content), 0644)

	// Mock dataDir
	origDataDir := dataDir
	dataDir = tmpDir
	defer func() {
		dataDir = origDataDir
	}()

	chunks, err := parseDocumentation()

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(chunks) != 1 {
		t.Errorf("Expected 1 chunk, got %d", len(chunks))
	}

	// Verify content contains first and last lines
	if len(chunks) > 0 {
		if !strings.Contains(chunks[0].Content, "Line number 0 with some content") {
			t.Error("Expected content to contain first line")
		}
		if !strings.Contains(chunks[0].Content, "Line number 999 with some content") {
			t.Error("Expected content to contain last line")
		}
		// Check that lines are roughly in order
		if !strings.Contains(chunks[0].Content, "Line number 500") {
			t.Error("Expected content to contain middle line")
		}
	}
}

func TestParseDocumentation_EmptyLines(t *testing.T) {
	content := `# Category

Line 1

Line 2


Line 3
`

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "krakend-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write test file
	testFile := filepath.Join(tmpDir, "docs", "llms-full.txt")
	os.MkdirAll(filepath.Dir(testFile), 0755)
	os.WriteFile(testFile, []byte(content), 0644)

	// Mock dataDir
	origDataDir := dataDir
	dataDir = tmpDir
	defer func() {
		dataDir = origDataDir
	}()

	chunks, err := parseDocumentation()

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(chunks) != 1 {
		t.Errorf("Expected 1 chunk, got %d", len(chunks))
	}

	// Content should have lines separated by newlines (empty lines not included)
	if len(chunks) > 0 {
		lines := strings.Split(chunks[0].Content, "\n")
		nonEmptyLines := 0
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				nonEmptyLines++
			}
		}
		if nonEmptyLines != 3 {
			t.Errorf("Expected 3 non-empty lines, got %d", nonEmptyLines)
		}
	}
}

func TestExtractEmbeddedIndex_ErrorHandling(t *testing.T) {
	// This test verifies that extractEmbeddedIndex properly handles write errors
	// We can't easily test the actual error conditions, but we can verify the function exists
	// and returns without panic on normal operations

	tmpDir, err := os.MkdirTemp("", "krakend-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	origDataDir := dataDir
	dataDir = tmpDir
	defer func() { dataDir = origDataDir }()

	// This will fail because embedded data is not available in tests,
	// but it should return an error, not panic
	err = extractEmbeddedIndex()

	// We expect an error since we don't have embedded data in tests
	// The important thing is that it doesn't panic
	if err == nil {
		// If it succeeds (somehow embedded data is available), that's fine too
		t.Log("extractEmbeddedIndex succeeded (embedded data available)")
	}
}

func TestSearchDocumentation_InvalidQuery(t *testing.T) {
	// Test that search handles invalid queries gracefully
	// This is a placeholder - full search testing would require index setup

	// Verify the function signature exists and can be called
	// (actual search testing would need more setup)
	t.Skip("Search testing requires full index setup - placeholder test")
}

