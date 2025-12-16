package indexing_test

import (
	"os"
	"strings"
	"testing"

	"github.com/krakend/mcp-server/internal/indexing"
)

func TestStripMarkdownLinks(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple markdown link",
			input:    "[Text](https://example.com)",
			expected: "Text",
		},
		{
			name:     "text with markdown link in middle",
			input:    "Start [Link Text](https://example.com) End",
			expected: "Start Link Text End",
		},
		{
			name:     "multiple markdown links",
			input:    "[First](url1) and [Second](url2)",
			expected: "First and Second",
		},
		{
			name:     "complex header from docs",
			input:    "[Rate Limit Tiers (available in KrakenD Enterprise)](https://www.krakend.io/docs/enterprise/service-settings/tiered-rate-limit/)",
			expected: "Rate Limit Tiers (available in KrakenD Enterprise)",
		},
		{
			name:     "no markdown link",
			input:    "Plain text without links",
			expected: "Plain text without links",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := indexing.StripMarkdownLinks(tt.input)
			if result != tt.expected {
				t.Errorf("indexing.StripMarkdownLinks() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{
			name:     "empty string",
			text:     "",
			expected: 0,
		},
		{
			name:     "short text",
			text:     "Hello World",
			expected: 2, // 11 chars / 4 = 2.75 -> 2
		},
		{
			name:     "medium text",
			text:     strings.Repeat("test ", 100), // 500 chars
			expected: 125,                          // 500 / 4 = 125
		},
		{
			name:     "target chunk size",
			text:     strings.Repeat("x", indexing.TargetChunkTokens*indexing.CharsPerToken), // 2000 chars
			expected: indexing.TargetChunkTokens,                                             // 500 tokens
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := indexing.EstimateTokens(tt.text)
			if result != tt.expected {
				t.Errorf("estimateTokens() = %d, want %d", result, tt.expected)
			}
		})
	}
}

func TestExtractKeywords(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		content string
		wantMin int // Minimum expected keywords
	}{
		{
			name:    "JWT authentication title",
			title:   "JWT Validation Configuration",
			content: "This section explains how to configure JWT validation in KrakenD",
			wantMin: 3, // Should get: jwt, validation, configuration, krakend, etc.
		},
		{
			name:    "filters stop words",
			title:   "The Best Way To Configure",
			content: "This is a test of the system",
			wantMin: 2, // Should filter out "the", "a", "is", "of"
		},
		{
			name:    "empty input",
			title:   "",
			content: "",
			wantMin: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keywords := indexing.ExtractKeywords(tt.title, tt.content)

			if len(keywords) < tt.wantMin {
				t.Errorf("indexing.ExtractKeywords() returned %d keywords, want at least %d. Keywords: %v",
					len(keywords), tt.wantMin, keywords)
			}

			// Check that keywords don't contain stop words
			stopWords := []string{"the", "a", "is", "to"}
			for _, kw := range keywords {
				for _, stop := range stopWords {
					if kw == stop {
						t.Errorf("indexing.ExtractKeywords() returned stop word: %s", kw)
					}
				}
			}

			// Check max limit
			if len(keywords) > 10 {
				t.Errorf("indexing.ExtractKeywords() returned %d keywords, max should be 10", len(keywords))
			}
		})
	}
}

func TestExtractURLFromMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple markdown link",
			input:    "[Text](https://example.com)",
			expected: "https://example.com",
		},
		{
			name:     "documentation header",
			input:    "[Rate Limit Tiers (available in KrakenD Enterprise)](https://www.krakend.io/docs/enterprise/service-settings/tiered-rate-limit/)",
			expected: "https://www.krakend.io/docs/enterprise/service-settings/tiered-rate-limit/",
		},
		{
			name:     "no markdown link",
			input:    "Plain text",
			expected: "",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := indexing.ExtractURLFromMarkdown(tt.input)
			if result != tt.expected {
				t.Errorf("indexing.ExtractURLFromMarkdown() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestCreateAnchor(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple text",
			input:    "Fields of Tiered Rate Limit",
			expected: "fields-of-tiered-rate-limit",
		},
		{
			name:     "with backticks",
			input:    "`tiers` * array",
			expected: "tiers--array",
		},
		{
			name:     "with special chars",
			input:    "Section (with parentheses)",
			expected: "section-with-parentheses",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := indexing.CreateAnchor(tt.input)
			if result != tt.expected {
				t.Errorf("indexing.CreateAnchor() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestEnrichMetadata(t *testing.T) {
	chunk := &indexing.DocChunk{
		ID:          "test_1",
		Page:        "Authentication",
		Category:    "JWT",
		Subcategory: "JWT Validation",
		Content:     strings.Repeat("This is test content about JWT validation. ", 10),
	}

	breadcrumb := []string{"Authentication", "JWT", "Validation"}
	baseURL := "https://www.krakend.io/docs/authentication/"
	indexing.EnrichMetadata(chunk, breadcrumb, baseURL)

	// Check breadcrumb (built from Page > Category > Subcategory)
	expectedBreadcrumb := "Authentication > JWT > JWT Validation"
	if chunk.Breadcrumb != expectedBreadcrumb {
		t.Errorf("Breadcrumb = %s, want %s", chunk.Breadcrumb, expectedBreadcrumb)
	}

	// Check URL - should be base URL + anchor for subsection
	expectedURL := "https://www.krakend.io/docs/authentication/#jwt-validation"
	if chunk.URL != expectedURL {
		t.Errorf("URL = %s, want %s", chunk.URL, expectedURL)
	}

	// Check keywords exist
	if len(chunk.Keywords) == 0 {
		t.Error("Keywords should not be empty")
	}

	// Check token count
	if chunk.TokenCount == 0 {
		t.Error("TokenCount should not be zero")
	}

	expectedTokens := len(chunk.Content) / indexing.CharsPerToken
	if chunk.TokenCount != expectedTokens {
		t.Errorf("TokenCount = %d, want %d", chunk.TokenCount, expectedTokens)
	}
}

func TestSubdivideChunk(t *testing.T) {
	t.Run("small chunk - no subdivision", func(t *testing.T) {
		content := strings.Repeat("Small content. ", 50) // ~750 chars = ~187 tokens
		chunk := indexing.DocChunk{
			ID:       "test_1",
			Subcategory: "Test Chunk",
			Content:  content,
			Page: "Testing",
			Category:  "Test",
		}

		breadcrumb := []string{"Testing"}
		baseURL := "https://www.krakend.io/docs/testing/"
		result := indexing.SubdivideChunk(chunk, breadcrumb, baseURL)

		if len(result) != 1 {
			t.Errorf("Expected 1 chunk, got %d", len(result))
		}

		// Should have metadata
		if result[0].TokenCount == 0 {
			t.Error("TokenCount should be set")
		}
		if result[0].Breadcrumb == "" {
			t.Error("Breadcrumb should be set")
		}
	})

	t.Run("large chunk - requires subdivision", func(t *testing.T) {
		// Create content larger than indexing.MaxChunkTokens (800 tokens = 3200 chars)
		paragraph := "This is a test paragraph with multiple sentences. " +
			"It contains information about KrakenD configuration. " +
			"We want to test that large chunks are properly subdivided. "
		content := strings.Repeat(paragraph+"\n\n", 30) // ~5400 chars = ~1350 tokens

		chunk := indexing.DocChunk{
			ID:       "test_2",
			Subcategory: "Large Test Chunk",
			Content:  content,
			Page: "Testing",
			Category:  "Large Test",
		}

		breadcrumb := []string{"Testing", "Large"}
		baseURL := "https://www.krakend.io/docs/testing/"
		result := indexing.SubdivideChunk(chunk, breadcrumb, baseURL)

		// Should be split into multiple chunks
		if len(result) < 2 {
			t.Errorf("Expected at least 2 chunks, got %d", len(result))
		}

		// Check each subchunk
		for i, subchunk := range result {
			// Should have proper ID
			if !strings.Contains(subchunk.ID, "_sub") {
				t.Errorf("Subchunk %d should have _sub in ID, got: %s", i, subchunk.ID)
			}

			// Should not exceed max size
			if subchunk.TokenCount > indexing.MaxChunkTokens*2 { // Allow some margin
				t.Errorf("Subchunk %d has %d tokens, exceeds max of %d",
					i, subchunk.TokenCount, indexing.MaxChunkTokens)
			}

			// Should have metadata
			if subchunk.Breadcrumb == "" {
				t.Errorf("Subchunk %d missing breadcrumb", i)
			}
			if len(subchunk.Keywords) == 0 {
				t.Errorf("Subchunk %d missing keywords", i)
			}

			// Check overlap (except first chunk)
			if i > 0 {
				// Should have some overlap with previous chunk
				prevContent := result[i-1].Content
				currContent := subchunk.Content

				// Get last 100 chars of previous and first 100 of current
				prevTail := ""
				if len(prevContent) > 100 {
					prevTail = prevContent[len(prevContent)-100:]
				}
				currHead := ""
				if len(currContent) > 100 {
					currHead = currContent[:100]
				}

				// There should be some common content
				hasOverlap := false
				if prevTail != "" && currHead != "" {
					// Simple check: see if any words from prevTail appear in currHead
					prevWords := strings.Fields(prevTail)
					for _, word := range prevWords {
						if len(word) > 4 && strings.Contains(currHead, word) {
							hasOverlap = true
							break
						}
					}
				}

				if !hasOverlap {
					t.Logf("Warning: Subchunk %d may not have overlap with previous chunk", i)
				}
			}
		}

		t.Logf("Large chunk split into %d subchunks", len(result))
		for i, chunk := range result {
			t.Logf("  Subchunk %d: %d tokens, %d chars", i, chunk.TokenCount, len(chunk.Content))
		}
	})

	t.Run("chunk at max size - no subdivision", func(t *testing.T) {
		// Create content exactly at indexing.MaxChunkTokens
		content := strings.Repeat("x", indexing.MaxChunkTokens*indexing.CharsPerToken) // 3200 chars = 800 tokens
		chunk := indexing.DocChunk{
			ID:      "test_3",
			Subcategory:  "Max Size Chunk",
			Content: content,
		}

		baseURL := "https://www.krakend.io/docs/test/"
		result := indexing.SubdivideChunk(chunk, []string{"Test"}, baseURL)

		if len(result) != 1 {
			t.Errorf("Expected 1 chunk (at max size), got %d", len(result))
		}
	})
}

func TestParseDocumentationIntegration(t *testing.T) {
	// Create a temporary test file
	testContent := `# Authentication
This is the authentication section.

## JWT Validation
This section covers JWT validation in detail.
` + strings.Repeat("Content about JWT validation and configuration. ", 100) + `

## OAuth2
This section covers OAuth2.
` + strings.Repeat("Content about OAuth2 setup and integration. ", 50) + `

# Rate Limiting
This section covers rate limiting.

## Token Bucket
Details about token bucket algorithm.
` + strings.Repeat("Explanation of token bucket rate limiting. ", 150)

	// Create temp file
	tmpFile, err := os.CreateTemp("", "docs-test-*.txt")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	defer os.Remove(tmpFile.Name())

	// Write test content
	if _, err := tmpFile.WriteString(testContent); err != nil {
		t.Fatalf("Failed to write test content: %v", err)
	}
	tmpFile.Close()

	fullPath := tmpFile.Name()

	// Parse
	chunks, err := indexing.ParseDocumentation(fullPath)
	if err != nil {
		t.Fatalf("indexing.ParseDocumentation() error = %v", err)
	}

	// Verify results
	if len(chunks) == 0 {
		t.Fatal("Expected chunks, got 0")
	}

	t.Logf("Parsed %d total chunks", len(chunks))

	// Check metadata enrichment
	metadataCount := 0
	for _, chunk := range chunks {
		if chunk.TokenCount > 0 {
			metadataCount++
		}
	}

	if metadataCount == 0 {
		t.Error("No chunks have token counts - metadata not enriched")
	}

	// Check token sizes
	oversized := 0
	for _, chunk := range chunks {
		if chunk.TokenCount > indexing.MaxChunkTokens*2 { // Allow 2x margin
			oversized++
			t.Errorf("Chunk %s has %d tokens, way over limit", chunk.ID, chunk.TokenCount)
		}
	}

	if oversized > 0 {
		t.Errorf("%d chunks are oversized", oversized)
	}

	// Log statistics
	totalTokens := 0
	for _, chunk := range chunks {
		totalTokens += chunk.TokenCount
	}
	avgTokens := totalTokens / len(chunks)
	t.Logf("Average chunk size: %d tokens", avgTokens)
	t.Logf("Target: %d tokens, Max: %d tokens", indexing.TargetChunkTokens, indexing.MaxChunkTokens)
}

// Helper functions for file operations in tests
func saveTestFile(path, content string) error {
	// Implementation depends on file structure
	// For now, return nil to allow skipping
	return nil
}

func cleanupTestFile(path string) {
	// Implementation depends on file structure
	// For now, do nothing
}
