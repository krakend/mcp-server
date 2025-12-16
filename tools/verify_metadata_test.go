package tools

import (
	"os"
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2"
)

// TestVerifyMetadataClean verifies that markdown links are stripped from real docs
func TestVerifyMetadataClean(t *testing.T) {
	// Use the embedded index
	indexPath := "../tools/data/search/index"
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		t.Skip("Index not found - run build.sh first")
	}

	// Open index
	index, err := bleve.Open(indexPath)
	if err != nil {
		t.Fatalf("Failed to open index: %v", err)
	}
	defer index.Close()

	// Search for "rate limit tiers" which we know has markdown links in headers
	query := bleve.NewMatchQuery("rate limit tiers")
	searchRequest := bleve.NewSearchRequest(query)
	searchRequest.Size = 5
	searchRequest.Fields = []string{"*"}

	searchResults, err := index.Search(searchRequest)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if searchResults.Total == 0 {
		t.Fatal("Expected results for 'rate limit tiers'")
	}

	t.Logf("Found %d results", searchResults.Total)

	for i, hit := range searchResults.Hits {
		category := getString(hit.Fields["category"])
		title := getString(hit.Fields["title"])
		section := getString(hit.Fields["section"])
		url := getString(hit.Fields["url"])

		t.Logf("\nResult %d (ID: %s):", i+1, hit.ID)
		t.Logf("  Category: %s", category)
		t.Logf("  Title:    %s", title)
		t.Logf("  Section:  %s", section)
		t.Logf("  URL:      %s", url)

		// Verify no markdown link syntax [text](url)
		if strings.Contains(category, "](") {
			t.Errorf("Category contains markdown link syntax: %s", category)
		}
		if strings.Contains(title, "](") {
			t.Errorf("Title contains markdown link syntax: %s", title)
		}
		if strings.Contains(section, "](") {
			t.Errorf("Section contains markdown link syntax: %s", section)
		}
		if strings.Contains(url, "](") {
			t.Errorf("URL contains markdown link syntax: %s", url)
		}

		// Verify no hash symbols in title/section (from H2-H5 headers)
		if strings.Contains(title, "#") {
			t.Errorf("Title contains hash symbols: %s", title)
		}
		if strings.Contains(section, "#") {
			t.Errorf("Section contains hash symbols: %s", section)
		}

		// Verify URL is well-formed
		if !strings.HasPrefix(url, "https://www.krakend.io/docs/") {
			t.Errorf("URL doesn't have expected prefix: %s", url)
		}
	}
}

// TestVerifyEnterpriseHeaders verifies enterprise edition headers are clean
func TestVerifyEnterpriseHeaders(t *testing.T) {
	indexPath := "../tools/data/search/index"
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		t.Skip("Index not found - run build.sh first")
	}

	index, err := bleve.Open(indexPath)
	if err != nil {
		t.Fatalf("Failed to open index: %v", err)
	}
	defer index.Close()

	// Search for enterprise features
	query := bleve.NewMatchQuery("KrakenD Enterprise Overview")
	searchRequest := bleve.NewSearchRequest(query)
	searchRequest.Size = 3
	searchRequest.Fields = []string{"*"}

	searchResults, err := index.Search(searchRequest)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if searchResults.Total == 0 {
		t.Fatal("Expected results for 'KrakenD Enterprise Overview'")
	}

	for _, hit := range searchResults.Hits {
		category := getString(hit.Fields["category"])

		// Verify category doesn't start with [ (markdown link)
		if len(category) > 0 && category[0] == '[' {
			t.Errorf("Category starts with markdown bracket: %s", category)
		}

		// Verify the typical enterprise header is clean
		if strings.Contains(category, "available in KrakenD Enterprise") {
			if strings.Contains(category, "](") {
				t.Errorf("Enterprise category has markdown: %s", category)
			}
			t.Logf("✓ Clean enterprise category: %s", category)
		}
	}
}

func getString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
