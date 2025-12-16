package tools

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	docsURL       = "https://www.krakend.io/llms-full.txt"
	cacheTTL      = 7 * 24 * time.Hour // 7 days
	maxResults    = 10
	docsFile      = "docs/llms-full.txt"
	cacheMetaFile = "docs/cache.meta"
	indexDir      = "search/index"
)

var (
	dataDir string // Data directory for documentation and search index
)

func init() {
	// Strategy 1: Try user home directory first (standalone installation)
	// This works cross-platform: ~/.krakend-mcp/ on Unix, C:\Users\...\krakend-mcp\ on Windows
	homeDir, err := os.UserHomeDir()
	if err == nil {
		userDataDir := filepath.Join(homeDir, ".krakend-mcp")

		// Check if user data directory exists
		if info, err := os.Stat(userDataDir); err == nil && info.IsDir() {
			dataDir = userDataDir
			log.Printf("✓ Data directory: %s (user home)", dataDir)
			return
		}

		// Try to create it - this is the expected path for standalone installations
		if err := os.MkdirAll(userDataDir, 0755); err == nil {
			// Successfully created, use it
			dataDir = userDataDir
			log.Printf("✓ Data directory created: %s", dataDir)

			// Create subdirectories
			os.MkdirAll(filepath.Join(dataDir, "docs"), 0755)
			os.MkdirAll(filepath.Join(dataDir, "search"), 0755)
			return
		}

		// If creation failed, log warning and try next strategy
		log.Printf("Warning: Could not create user data directory at %s: %v", userDataDir, err)
	} else {
		log.Printf("Warning: Could not determine user home directory: %v", err)
	}

	// Strategy 2: Try relative to executable (development/plugin installation)
	// Binary at: plugin/servers/krakend-mcp-server/krakend-mcp-server
	// Data at:   plugin/data/
	execPath, err := os.Executable()
	if err == nil {
		execDir := filepath.Dir(execPath)
		relativeDataDir := filepath.Join(execDir, "..", "..", "..", "data")

		// Check if data directory exists relative to binary
		if info, err := os.Stat(relativeDataDir); err == nil && info.IsDir() {
			dataDir, _ = filepath.Abs(relativeDataDir)
			log.Printf("✓ Data directory: %s (relative to binary)", dataDir)
			return
		}
	}

	// Strategy 3: Last resort fallback to current working directory
	dataDir = filepath.Join(".", "data")
	log.Printf("⚠️  Data directory (fallback): %s", dataDir)

	// Try to create it
	os.MkdirAll(filepath.Join(dataDir, "docs"), 0755)
	os.MkdirAll(filepath.Join(dataDir, "search"), 0755)
}

// DocChunk represents a documentation chunk in the search index
type DocChunk struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Content  string `json:"content"`
	Category string `json:"category"`
	URL      string `json:"url,omitempty"`
	Section  string `json:"section"`
}

// SearchResult represents a search result with score
type SearchResult struct {
	Chunk DocChunk `json:"chunk"`
	Score float64  `json:"score"`
}

// SearchDocumentationInput defines input for search_documentation tool
type SearchDocumentationInput struct {
	Query      string `json:"query" jsonschema:"Search query for documentation"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"Maximum number of results (optional, defaults to 5)"`
}

// SearchDocumentationOutput defines output for search_documentation tool
type SearchDocumentationOutput struct {
	Results    []SearchResult `json:"results"`
	Query      string         `json:"query"`
	TotalHits  int            `json:"total_hits"`
	SourceURLs []string       `json:"source_urls"`
}

// RefreshDocumentationIndexInput defines input for refresh_documentation_index tool
type RefreshDocumentationIndexInput struct {
	Force bool `json:"force,omitempty" jsonschema:"Force re-download and re-indexing (optional, defaults to false)"`
}

// RefreshDocumentationIndexOutput defines output for refresh_documentation_index tool
type RefreshDocumentationIndexOutput struct {
	Updated       bool      `json:"updated"`
	LastUpdate    time.Time `json:"last_update"`
	ChunksIndexed int       `json:"chunks_indexed"`
	Message       string    `json:"message"`
}

var (
	docIndex bleve.Index
)

// InitializeDocSearch initializes the documentation search system
// Priority: Local docs (if exist and recent) > Embedded docs (always available)
func InitializeDocSearch() error {
	indexPath := filepath.Join(dataDir, indexDir)

	// Strategy 1: Try to open local index (from previous refresh or embedded extraction)
	if _, err := os.Stat(indexPath); err == nil {
		index, err := bleve.Open(indexPath)
		if err == nil {
			docIndex = index
			count, _ := docIndex.DocCount()
			log.Printf("✓ Documentation search initialized (%d docs, local index)", count)

			// Check if local cache is stale and suggest refresh
			if needsRefresh() {
				log.Printf("ℹ️  Local documentation is >7 days old. Consider using refresh_documentation_index tool to update.")
			}

			return nil
		}

		// Index corrupted, remove it
		log.Printf("Warning: Local index corrupted, removing...")
		os.RemoveAll(indexPath)
	}

	// Strategy 2: Extract embedded index to local storage
	log.Printf("No local index found, extracting embedded documentation...")

	if err := extractEmbeddedIndex(); err != nil {
		return fmt.Errorf("failed to extract embedded index: %w", err)
	}

	// Open the extracted index
	index, err := bleve.Open(indexPath)
	if err != nil {
		return fmt.Errorf("failed to open extracted index: %w", err)
	}

	docIndex = index
	count, _ := docIndex.DocCount()
	log.Printf("✓ Documentation search initialized (%d docs, embedded index)", count)
	log.Printf("ℹ️  Using embedded documentation (build-time). Use refresh_documentation_index to get latest docs.")

	return nil
}

// extractEmbeddedIndex extracts the embedded search index to local storage
func extractEmbeddedIndex() error {
	indexPath := filepath.Join(dataDir, indexDir)

	// Create index directory
	if err := os.MkdirAll(indexPath, 0755); err != nil {
		return fmt.Errorf("failed to create index directory: %w", err)
	}

	// Extract all index files recursively (including store/ subdirectory)
	if err := extractEmbeddedDir("data/search/index", indexPath); err != nil {
		return fmt.Errorf("failed to extract embedded index: %w", err)
	}

	// Also extract embedded docs
	docsPath := filepath.Join(dataDir, "docs")
	os.MkdirAll(docsPath, 0755)

	// Extract llms-full.txt
	if docsData, err := defaultDataProvider.ReadFile("data/docs/llms-full.txt"); err == nil {
		if err := os.WriteFile(filepath.Join(docsPath, "llms-full.txt"), docsData, 0644); err != nil {
			return fmt.Errorf("failed to extract llms-full.txt: %w", err)
		}
	}

	// Extract cache.meta
	if metaData, err := defaultDataProvider.ReadFile("data/docs/cache.meta"); err == nil {
		if err := os.WriteFile(filepath.Join(docsPath, "cache.meta"), metaData, 0644); err != nil {
			return fmt.Errorf("failed to extract cache.meta: %w", err)
		}
	}

	log.Printf("✓ Embedded index and docs extracted to %s", dataDir)

	return nil
}

// extractEmbeddedDir recursively extracts files from embedded FS to local filesystem
func extractEmbeddedDir(embedPath, localPath string) error {
	entries, err := defaultDataProvider.ReadDir(embedPath)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", embedPath, err)
	}

	for _, entry := range entries {
		embeddedFile := filepath.Join(embedPath, entry.Name())
		localFile := filepath.Join(localPath, entry.Name())

		if entry.IsDir() {
			// Create directory and recurse
			if err := os.MkdirAll(localFile, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", localFile, err)
			}
			if err := extractEmbeddedDir(embeddedFile, localFile); err != nil {
				return err
			}
		} else {
			// Extract file
			data, err := defaultDataProvider.ReadFile(embeddedFile)
			if err != nil {
				return fmt.Errorf("failed to read embedded file %s: %w", embeddedFile, err)
			}
			if err := os.WriteFile(localFile, data, 0644); err != nil {
				return fmt.Errorf("failed to write file %s: %w", localFile, err)
			}
		}
	}

	return nil
}

// needsRefresh checks if documentation cache needs refreshing
func needsRefresh() bool {
	metaPath := filepath.Join(dataDir, cacheMetaFile)
	info, err := os.Stat(metaPath)
	if err != nil {
		return true // No cache, needs refresh
	}

	age := time.Since(info.ModTime())
	return age > cacheTTL
}

// downloadDocumentation downloads the full documentation
func downloadDocumentation() error {
	log.Printf("Downloading documentation from %s", docsURL)

	resp, err := http.Get(docsURL)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close() // Close explicitly before early return
		return fmt.Errorf("download failed with status: %d", resp.StatusCode)
	}

	// Ensure docs directory exists
	docsPath := filepath.Join(dataDir, "docs")
	if err := os.MkdirAll(docsPath, 0755); err != nil {
		return fmt.Errorf("failed to create docs directory: %w", err)
	}

	// Write to file
	fullPath := filepath.Join(dataDir, docsFile)
	file, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	// Write cache metadata
	metaPath := filepath.Join(dataDir, cacheMetaFile)
	metaFile, err := os.Create(metaPath)
	if err != nil {
		return fmt.Errorf("failed to create meta file: %w", err)
	}
	defer metaFile.Close()

	fmt.Fprintf(metaFile, "last_update: %s\n", time.Now().Format(time.RFC3339))

	log.Printf("Documentation downloaded successfully")
	return nil
}

// parseDocumentation parses documentation into chunks
func parseDocumentation() ([]DocChunk, error) {
	fullPath := filepath.Join(dataDir, docsFile)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read documentation: %w", err)
	}

	text := string(content)
	lines := strings.Split(text, "\n")

	var chunks []DocChunk
	var currentChunk *DocChunk
	var currentCategory string
	var contentBuilder strings.Builder
	chunkID := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Detect headers (# Header, ## Subheader)
		if strings.HasPrefix(line, "##") {
			// Save previous chunk
			if currentChunk != nil && contentBuilder.Len() > 0 {
				currentChunk.Content = contentBuilder.String()
				chunks = append(chunks, *currentChunk)
				chunkID++
			}

			// Start new chunk
			title := strings.TrimSpace(strings.TrimPrefix(line, "##"))
			currentChunk = &DocChunk{
				ID:       fmt.Sprintf("chunk_%d", chunkID),
				Title:    title,
				Section:  title,
				Category: currentCategory,
			}
			contentBuilder.Reset()
		} else if strings.HasPrefix(line, "#") {
			// Top-level category
			currentCategory = strings.TrimSpace(strings.TrimPrefix(line, "#"))

			// Save previous chunk
			if currentChunk != nil && contentBuilder.Len() > 0 {
				currentChunk.Content = contentBuilder.String()
				chunks = append(chunks, *currentChunk)
				chunkID++
			}

			// Start new chunk for category
			currentChunk = &DocChunk{
				ID:       fmt.Sprintf("chunk_%d", chunkID),
				Title:    currentCategory,
				Section:  currentCategory,
				Category: currentCategory,
			}
			contentBuilder.Reset()
		} else if line != "" && currentChunk != nil {
			// Add content to current chunk using Builder (O(n) instead of O(n²))
			if contentBuilder.Len() > 0 {
				contentBuilder.WriteString("\n")
			}
			contentBuilder.WriteString(line)
		}
	}

	// Save last chunk
	if currentChunk != nil && contentBuilder.Len() > 0 {
		currentChunk.Content = contentBuilder.String()
		chunks = append(chunks, *currentChunk)
	}

	log.Printf("Parsed %d documentation chunks", len(chunks))
	return chunks, nil
}

// indexChunks creates/updates the Bleve search index
func indexChunks(chunks []DocChunk) error {
	indexPath := filepath.Join(dataDir, indexDir)

	// Delete old index if exists
	os.RemoveAll(indexPath)

	// Create directory
	if err := os.MkdirAll(filepath.Dir(indexPath), 0755); err != nil {
		return fmt.Errorf("failed to create index directory: %w", err)
	}

	// Create new index
	mapping := bleve.NewIndexMapping()
	index, err := bleve.New(indexPath, mapping)
	if err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}

	// Index all chunks
	batch := index.NewBatch()
	for i, chunk := range chunks {
		if err := batch.Index(chunk.ID, chunk); err != nil {
			index.Close()
			return fmt.Errorf("failed to add chunk %s to batch: %w", chunk.ID, err)
		}

		// Submit batch every 100 documents
		if i%100 == 0 && i > 0 {
			if err := index.Batch(batch); err != nil {
				index.Close()
				return fmt.Errorf("failed to index batch: %w", err)
			}
			batch = index.NewBatch()
		}
	}

	// Submit remaining
	if batch.Size() > 0 {
		if err := index.Batch(batch); err != nil {
			index.Close()
			return fmt.Errorf("failed to index final batch: %w", err)
		}
	}

	log.Printf("Indexed %d chunks", len(chunks))

	// Close the index explicitly before reopening
	if err := index.Close(); err != nil {
		log.Printf("Warning: Error closing index during creation: %v", err)
	}

	// Reopen global index
	log.Printf("Reopening index for use...")
	docIndex, err = bleve.Open(indexPath)
	if err != nil {
		return fmt.Errorf("failed to reopen index: %w", err)
	}

	log.Printf("✓ Index created successfully and ready for searches")
	return nil
}

// refreshDocumentationIndex downloads and re-indexes documentation
func refreshDocumentationIndex(force bool) error {
	if !force && !needsRefresh() {
		return nil // Cache is fresh
	}

	// Download documentation
	if err := downloadDocumentation(); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	// Parse into chunks
	chunks, err := parseDocumentation()
	if err != nil {
		return fmt.Errorf("parse failed: %w", err)
	}

	// Create search index
	if err := indexChunks(chunks); err != nil {
		return fmt.Errorf("indexing failed: %w", err)
	}

	return nil
}

// SearchDocumentation searches through KrakenD documentation
func SearchDocumentation(ctx context.Context, req *mcp.CallToolRequest, input SearchDocumentationInput) (*mcp.CallToolResult, SearchDocumentationOutput, error) {
	// If index not initialized, try to initialize it now
	if docIndex == nil {
		log.Printf("Doc index not initialized, initializing now...")
		if err := InitializeDocSearch(); err != nil {
			return nil, SearchDocumentationOutput{}, fmt.Errorf("failed to initialize documentation index: %w", err)
		}
	}

	maxResults := input.MaxResults
	if maxResults == 0 || maxResults > 20 {
		maxResults = 10
	}

	// Create search query
	query := bleve.NewMatchQuery(input.Query)
	search := bleve.NewSearchRequest(query)
	search.Size = maxResults
	search.Fields = []string{"*"}

	// Execute search
	searchResults, err := docIndex.Search(search)
	if err != nil {
		return nil, SearchDocumentationOutput{}, fmt.Errorf("search failed: %w", err)
	}

	// Convert to output format
	results := make([]SearchResult, 0, len(searchResults.Hits))
	for _, hit := range searchResults.Hits {
		chunk := DocChunk{
			ID: hit.ID,
		}

		if title, ok := hit.Fields["title"].(string); ok {
			chunk.Title = title
		}
		if content, ok := hit.Fields["content"].(string); ok {
			chunk.Content = content
		}
		if category, ok := hit.Fields["category"].(string); ok {
			chunk.Category = category
		}
		if section, ok := hit.Fields["section"].(string); ok {
			chunk.Section = section
		}
		if url, ok := hit.Fields["url"].(string); ok {
			chunk.URL = url
		}

		results = append(results, SearchResult{
			Chunk: chunk,
			Score: hit.Score,
		})
	}

	output := SearchDocumentationOutput{
		Results:    results,
		Query:      input.Query,
		TotalHits:  int(searchResults.Total),
		SourceURLs: []string{"https://www.krakend.io/docs/"},
	}

	return nil, output, nil
}

// RefreshDocumentationIndex forces refresh of documentation index
func RefreshDocumentationIndex(ctx context.Context, req *mcp.CallToolRequest, input RefreshDocumentationIndexInput) (*mcp.CallToolResult, RefreshDocumentationIndexOutput, error) {
	output := RefreshDocumentationIndexOutput{
		Updated: false,
	}

	// Check if refresh needed
	if !input.Force && !needsRefresh() {
		metaPath := filepath.Join(dataDir, cacheMetaFile)
		if info, err := os.Stat(metaPath); err == nil {
			output.LastUpdate = info.ModTime()
			output.Message = fmt.Sprintf("Cache is fresh (last updated: %s)", info.ModTime().Format(time.RFC3339))
			return nil, output, nil
		}
	}

	// Perform refresh
	if err := refreshDocumentationIndex(input.Force); err != nil {
		return nil, output, fmt.Errorf("refresh failed: %w", err)
	}

	// Count chunks
	if docIndex != nil {
		count, _ := docIndex.DocCount()
		output.ChunksIndexed = int(count)
	}

	output.Updated = true
	output.LastUpdate = time.Now()
	output.Message = fmt.Sprintf("Documentation refreshed successfully, %d chunks indexed", output.ChunksIndexed)

	return nil, output, nil
}

// RegisterDocSearchTools registers documentation search tools
func RegisterDocSearchTools(server *mcp.Server) error {
	// Initialize doc search synchronously
	if err := InitializeDocSearch(); err != nil {
		log.Printf("Warning: Documentation search initialization failed: %v", err)
		log.Printf("Documentation search will attempt to initialize on first use")
	}

	// Tool 18: search_documentation
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "search_documentation",
			Description: "Search through KrakenD documentation using full-text search. Returns top relevant chunks with context.",
		},
		SearchDocumentation,
	)

	// Tool 20: refresh_documentation_index
	mcp.AddTool(server,
		&mcp.Tool{
			Name:        "refresh_documentation_index",
			Description: "Force re-download and re-index of KrakenD documentation (auto-runs if cache > 7 days old)",
		},
		RefreshDocumentationIndex,
	)

	return nil
}

// CloseDocSearch closes the documentation search index
func CloseDocSearch() error {
	if docIndex != nil {
		return docIndex.Close()
	}
	return nil
}
