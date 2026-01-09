package tools

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/krakend/mcp-server/internal/indexing"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	docsURL       = "https://www.krakend.io/llms-full.txt"
	cacheTTL      = 7 * 24 * time.Hour // 7 days
	maxResults    = 10
	docsFile      = "docs/llms-full.txt"
	cacheMetaFile = "docs/cache.meta"
	indexDir      = "search/index"
	lockFile      = "search/index.lock"
	lockTimeout   = 5 * time.Second // Max time to wait for lock
	lockRetryWait = 500 * time.Millisecond

	indexVersionFile = "search/.index_version"
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

// isProcessRunning is implemented in platform-specific files:
// - docsearch_unix.go for Unix/Linux/macOS
// - docsearch_windows.go for Windows

// cleanStaleLock removes lock file if the owning process is dead
func cleanStaleLock() error {
	lockPath := filepath.Join(dataDir, lockFile)

	// Read lock file
	data, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No lock file, nothing to clean
		}
		return fmt.Errorf("failed to read lock file: %w", err)
	}

	// Parse PID
	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		// Corrupted lock file, remove it
		log.Printf("Warning: Corrupted lock file (invalid PID), removing...")
		return os.Remove(lockPath)
	}

	// Check if process is running
	if isProcessRunning(pid) {
		return fmt.Errorf("lock held by running process %d", pid)
	}

	// Process is dead, remove stale lock
	log.Printf("Stale lock detected (PID %d not running), cleaning...", pid)
	return os.Remove(lockPath)
}

// acquireLock attempts to acquire the index lock with retry
func acquireLock() error {
	lockPath := filepath.Join(dataDir, lockFile)
	ourPID := os.Getpid()

	// Check if we already have the lock
	if data, err := os.ReadFile(lockPath); err == nil {
		if pidStr := strings.TrimSpace(string(data)); pidStr != "" {
			if pid, err := strconv.Atoi(pidStr); err == nil && pid == ourPID {
				log.Printf("Lock already held by this process (PID %d)", ourPID)
				return nil
			}
		}
	}

	startTime := time.Now()

	for {
		// Try to clean stale lock first
		if err := cleanStaleLock(); err != nil {
			// Lock is held by active process
			elapsed := time.Since(startTime)
			if elapsed >= lockTimeout {
				return fmt.Errorf("timeout waiting for index lock after %v: %w", elapsed, err)
			}

			log.Printf("Index locked by another process, waiting... (%v elapsed)", elapsed.Round(100*time.Millisecond))
			time.Sleep(lockRetryWait)
			continue
		}

		// Try to create lock file with our PID
		err := os.WriteFile(lockPath, []byte(strconv.Itoa(ourPID)), 0644)
		if err != nil {
			return fmt.Errorf("failed to create lock file: %w", err)
		}

		log.Printf("✓ Index lock acquired (PID %d)", ourPID)
		return nil
	}
}

// releaseLock releases the index lock
func releaseLock() error {
	lockPath := filepath.Join(dataDir, lockFile)

	// Verify we own the lock before removing
	data, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Lock already removed
		}
		return fmt.Errorf("failed to read lock file: %w", err)
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err == nil && pid != os.Getpid() {
		log.Printf("Warning: Lock file contains different PID (%d vs %d), not removing", pid, os.Getpid())
		return nil
	}

	if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove lock file: %w", err)
	}

	log.Printf("✓ Index lock released")
	return nil
}

// SearchResult represents a search result with score
type SearchResult struct {
	Chunk indexing.DocChunk `json:"chunk"`
	Score float64           `json:"score"`
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

// indexHolder manages concurrent access to the Bleve documentation index
type indexHolder struct {
	// current holds the active index pointer (atomic access for lock-free reads)
	current atomic.Pointer[Index]

	// refreshMu prevents concurrent refresh operations
	// NOT used for searches - they are lock-free via atomic pointer
	refreshMu sync.Mutex

	// wg tracks in-flight search operations for graceful cleanup of old indexes
	wg sync.WaitGroup
}

var (
	indexMgr *indexHolder
)

// InitializeDocSearch initializes the documentation search system
// Priority: Local docs (if exist and recent) > Embedded docs (always available)
func InitializeDocSearch() error {
	startTime := time.Now()
	log.Printf("Initializing documentation search...")

	// Initialize indexHolder if needed
	if indexMgr == nil {
		indexMgr = &indexHolder{}
	}

	indexPath := filepath.Join(dataDir, indexDir)

	// Acquire lock before accessing index
	log.Printf("Acquiring index lock...")
	lockStart := time.Now()
	if err := acquireLock(); err != nil {
		return fmt.Errorf("failed to acquire index lock: %w", err)
	}
	log.Printf("Lock acquired in %v", time.Since(lockStart).Round(time.Millisecond))

	// Strategy 1: Try to open local index (from previous refresh or embedded extraction)
	if _, err := os.Stat(indexPath); err == nil {
		// Check index schema version
		currentVersion := getIndexVersion()
		if currentVersion != indexing.IndexSchemaVersion {
			log.Printf("Index schema version mismatch (have: v%d, want: v%d), invalidating old index...",
				currentVersion, indexing.IndexSchemaVersion)
			os.RemoveAll(indexPath)
			os.Remove(filepath.Join(dataDir, indexVersionFile))
		} else {
			openStart := time.Now()
			index, err := bleve.Open(indexPath)
			if err == nil {
				wrapped := NewBleveIndexWrapper(index)
				indexMgr.current.Store(&wrapped)
				count, _ := wrapped.DocCount()
				elapsed := time.Since(startTime).Round(time.Millisecond)
				log.Printf("✓ Documentation search initialized (%d docs, local index v%d) in %v",
					count, indexing.IndexSchemaVersion, elapsed)

				// Check if local cache is stale and suggest refresh
				if needsRefresh() {
					log.Printf("ℹ️  Local documentation is >7 days old. Consider using refresh_documentation_index tool to update.")
				}

				return nil
			}

			// Index corrupted, remove it
			log.Printf("Warning: Local index corrupted (open failed in %v), removing...", time.Since(openStart).Round(time.Millisecond))
			os.RemoveAll(indexPath)
			os.Remove(filepath.Join(dataDir, indexVersionFile))
		}
	}

	// Strategy 2: Extract embedded index to local storage
	log.Printf("No local index found, extracting embedded documentation...")
	extractStart := time.Now()

	if err := extractEmbeddedIndex(); err != nil {
		return fmt.Errorf("failed to extract embedded index: %w", err)
	}
	log.Printf("Extraction completed in %v", time.Since(extractStart).Round(time.Millisecond))

	// Open the extracted index
	openStart := time.Now()
	index, err := bleve.Open(indexPath)
	if err != nil {
		return fmt.Errorf("failed to open extracted index: %w", err)
	}
	log.Printf("Index opened in %v", time.Since(openStart).Round(time.Millisecond))

	wrapped := NewBleveIndexWrapper(index)
	indexMgr.current.Store(&wrapped)
	count, _ := wrapped.DocCount()
	elapsed := time.Since(startTime).Round(time.Millisecond)
	log.Printf("✓ Documentation search initialized (%d docs, embedded index) in %v", count, elapsed)
	log.Printf("ℹ️  Using embedded documentation (build-time). Use refresh_documentation_index to get latest docs.")

	return nil
}

// getIndexVersion reads the current index schema version from disk
func getIndexVersion() int {
	versionPath := filepath.Join(dataDir, indexVersionFile)
	data, err := os.ReadFile(versionPath)
	if err != nil {
		return 0 // No version file = v0 (old format)
	}

	version := 0
	fmt.Sscanf(string(data), "%d", &version)
	return version
}

// writeIndexVersion writes the current index schema version to disk
func writeIndexVersion() error {
	versionPath := filepath.Join(dataDir, indexVersionFile)
	os.MkdirAll(filepath.Dir(versionPath), 0755)

	content := fmt.Sprintf("%d", indexing.IndexSchemaVersion)
	return os.WriteFile(versionPath, []byte(content), 0644)
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

	// Write version file to mark this as v2 index
	if err := writeIndexVersion(); err != nil {
		log.Printf("Warning: Failed to write index version: %v", err)
	}

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

// indexChunks creates/updates the Bleve search index
// averageTokens calculates the average token count across chunks
func averageTokens(chunks []indexing.DocChunk) int {
	if len(chunks) == 0 {
		return 0
	}
	total := 0
	for _, chunk := range chunks {
		total += chunk.TokenCount
	}
	return total / len(chunks)
}

// countOversized counts chunks that exceed the maximum token limit
func countOversized(chunks []indexing.DocChunk) int {
	count := 0
	for _, chunk := range chunks {
		if chunk.TokenCount > indexing.MaxChunkTokens {
			count++
		}
	}
	return count
}

func indexChunks(chunks []indexing.DocChunk) error {
	startTime := time.Now()
	indexPath := filepath.Join(dataDir, indexDir)
	tempIndexPath := filepath.Join(dataDir, indexDir+".tmp")

	// Clean up any leftover temp index from previous crash
	os.RemoveAll(tempIndexPath)

	// Create directory for temp index
	if err := os.MkdirAll(filepath.Dir(tempIndexPath), 0755); err != nil {
		return fmt.Errorf("failed to create temp index directory: %w", err)
	}

	// Create new index in temp location
	log.Printf("Creating new index with %d chunks in temp location...", len(chunks))
	createStart := time.Now()
	mapping := bleve.NewIndexMapping()
	newIndex, err := bleve.New(tempIndexPath, mapping)
	if err != nil {
		return fmt.Errorf("failed to create temp index: %w", err)
	}
	log.Printf("Temp index created in %v", time.Since(createStart).Round(time.Millisecond))

	// Index all chunks
	indexStart := time.Now()
	batch := newIndex.NewBatch()
	for i, chunk := range chunks {
		if err := batch.Index(chunk.ID, chunk); err != nil {
			newIndex.Close()
			os.RemoveAll(tempIndexPath)
			return fmt.Errorf("failed to add chunk %s to batch: %w", chunk.ID, err)
		}

		// Submit batch every 100 documents
		if i%100 == 0 && i > 0 {
			if err := newIndex.Batch(batch); err != nil {
				newIndex.Close()
				os.RemoveAll(tempIndexPath)
				return fmt.Errorf("failed to index batch: %w", err)
			}
			batch = newIndex.NewBatch()
			log.Printf("Indexed %d/%d chunks...", i, len(chunks))
		}
	}

	// Submit remaining
	if batch.Size() > 0 {
		if err := newIndex.Batch(batch); err != nil {
			newIndex.Close()
			os.RemoveAll(tempIndexPath)
			return fmt.Errorf("failed to index final batch: %w", err)
		}
	}

	log.Printf("Indexed %d chunks in %v", len(chunks), time.Since(indexStart).Round(time.Millisecond))

	// Close temp index before moving
	if err := newIndex.Close(); err != nil {
		log.Printf("Warning: Error closing temp index: %v", err)
		os.RemoveAll(tempIndexPath)
		return fmt.Errorf("failed to close temp index: %w", err)
	}

	// Atomic filesystem swap: rename temp to final location
	log.Printf("Swapping temp index into place...")
	swapStart := time.Now()

	// Remove old index directory (atomic operation will replace it)
	if err := os.RemoveAll(indexPath); err != nil && !os.IsNotExist(err) {
		os.RemoveAll(tempIndexPath)
		return fmt.Errorf("failed to remove old index: %w", err)
	}

	// Rename temp to final location (atomic operation on POSIX)
	if err := os.Rename(tempIndexPath, indexPath); err != nil {
		os.RemoveAll(tempIndexPath)
		return fmt.Errorf("failed to rename temp index: %w", err)
	}
	log.Printf("Index swapped in %v", time.Since(swapStart).Round(time.Millisecond))

	// Open the index from final location
	log.Printf("Opening new index for use...")
	reopenStart := time.Now()
	finalIndex, err := bleve.Open(indexPath)
	if err != nil {
		return fmt.Errorf("failed to open new index: %w", err)
	}
	log.Printf("New index opened in %v", time.Since(reopenStart).Round(time.Millisecond))

	// Wrap the index with our interface
	wrapped := NewBleveIndexWrapper(finalIndex)

	// ATOMIC SWAP: Replace the global index pointer
	log.Printf("Swapping index pointer atomically...")
	oldIndexPtr := indexMgr.current.Swap(&wrapped)

	// Graceful cleanup of old index in background
	go func(oldPtr *Index) {
		if oldPtr == nil {
			return
		}

		log.Printf("Waiting for in-flight searches to complete before closing old index...")
		waitStart := time.Now()

		// Wait for all in-flight searches on old index to complete
		indexMgr.wg.Wait()

		log.Printf("All searches completed, closing old index (waited %v)...",
			time.Since(waitStart).Round(time.Millisecond))

		old := *oldPtr
		if err := old.Close(); err != nil {
			log.Printf("Warning: Error closing old index: %v", err)
		} else {
			log.Printf("✓ Old index closed successfully")
		}
	}(oldIndexPtr)

	elapsed := time.Since(startTime).Round(time.Millisecond)
	log.Printf("✓ Index swap completed in %v, searches now using new index", elapsed)

	// Write version file to mark this as current index version
	if err := writeIndexVersion(); err != nil {
		log.Printf("Warning: Failed to write index version: %v", err)
	}

	return nil
}

// refreshDocumentationIndex downloads and re-indexes documentation
func refreshDocumentationIndex(force bool) error {
	startTime := time.Now()

	if !force && !needsRefresh() {
		log.Printf("Documentation cache is fresh, skipping refresh")
		return nil // Cache is fresh
	}

	// Serialize refresh operations (prevent concurrent refreshes)
	indexMgr.refreshMu.Lock()
	defer indexMgr.refreshMu.Unlock()

	// Re-check after acquiring lock (double-checked locking pattern)
	// Another goroutine may have already refreshed while we were waiting
	if !force && !needsRefresh() {
		log.Printf("Documentation was refreshed by another goroutine, skipping")
		return nil
	}

	log.Printf("Starting documentation refresh (force=%v)...", force)

	// Acquire inter-process lock for re-indexing (will wait if another process has it)
	if err := acquireLock(); err != nil {
		return fmt.Errorf("failed to acquire lock for refresh: %w", err)
	}
	// Note: Lock will be released by CloseDocSearch() when process exits

	// Download documentation
	downloadStart := time.Now()
	if err := downloadDocumentation(); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	log.Printf("Download completed in %v", time.Since(downloadStart).Round(time.Millisecond))

	// Parse into chunks
	parseStart := time.Now()
	fullPath := filepath.Join(dataDir, docsFile)
	chunks, err := indexing.ParseDocumentation(fullPath)
	if err != nil {
		return fmt.Errorf("parse failed: %w", err)
	}
	log.Printf("Parsed %d documentation chunks (avg: %d tokens, %d over limit)",
		len(chunks), averageTokens(chunks), countOversized(chunks))
	log.Printf("Parse completed in %v", time.Since(parseStart).Round(time.Millisecond))

	// Create search index (this closes and reopens the global index)
	if err := indexChunks(chunks); err != nil {
		return fmt.Errorf("indexing failed: %w", err)
	}

	elapsed := time.Since(startTime).Round(time.Millisecond)
	log.Printf("✓ Documentation refresh completed in %v", elapsed)

	return nil
}

// SearchDocumentation searches through KrakenD documentation
func SearchDocumentation(ctx context.Context, req *mcp.CallToolRequest, input SearchDocumentationInput) (*mcp.CallToolResult, SearchDocumentationOutput, error) {
	// Track in-flight searches for graceful cleanup (MUST be before Load)
	indexMgr.wg.Add(1)
	defer indexMgr.wg.Done()

	// Get current index atomically (lock-free read)
	indexPtr := indexMgr.current.Load()

	// If index not initialized, try to initialize it now
	if indexPtr == nil {
		log.Printf("Doc index not initialized, initializing now...")
		if err := InitializeDocSearch(); err != nil {
			return nil, SearchDocumentationOutput{}, fmt.Errorf("failed to initialize documentation index: %w", err)
		}
		// Reload after initialization
		indexPtr = indexMgr.current.Load()
		if indexPtr == nil {
			return nil, SearchDocumentationOutput{}, fmt.Errorf("index still nil after initialization")
		}
	}

	// Dereference pointer to get actual index
	index := *indexPtr

	maxResults := input.MaxResults
	if maxResults == 0 || maxResults > 20 {
		maxResults = 10
	}

	// Create search query
	query := bleve.NewMatchQuery(input.Query)
	search := bleve.NewSearchRequest(query)
	search.Size = maxResults
	search.Fields = []string{"*"}

	// Execute search on current index
	searchResults, err := index.Search(search)
	if err != nil {
		return nil, SearchDocumentationOutput{}, fmt.Errorf("search failed: %w", err)
	}

	// Convert to output format
	results := make([]SearchResult, 0, len(searchResults.Hits))
	for _, hit := range searchResults.Hits {
		chunk := indexing.DocChunk{
			ID: hit.ID,
		}

		if subcategory, ok := hit.Fields["subcategory"].(string); ok {
			chunk.Subcategory = subcategory
		}
		if content, ok := hit.Fields["content"].(string); ok {
			chunk.Content = content
		}
		if page, ok := hit.Fields["page"].(string); ok {
			chunk.Page = page
		}
		if category, ok := hit.Fields["category"].(string); ok {
			chunk.Category = category
		}
		if url, ok := hit.Fields["url"].(string); ok {
			chunk.URL = url
		}
		if breadcrumb, ok := hit.Fields["breadcrumb"].(string); ok {
			chunk.Breadcrumb = breadcrumb
		}
		if keywords, ok := hit.Fields["keywords"].([]interface{}); ok {
			chunk.Keywords = make([]string, 0, len(keywords))
			for _, kw := range keywords {
				if kwStr, ok := kw.(string); ok {
					chunk.Keywords = append(chunk.Keywords, kwStr)
				}
			}
		}
		if tokenCount, ok := hit.Fields["token_count"].(float64); ok {
			chunk.TokenCount = int(tokenCount)
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

	// Count chunks from current index
	indexPtr := indexMgr.current.Load()
	if indexPtr != nil {
		index := *indexPtr
		count, _ := index.DocCount()
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

// CloseDocSearch closes the documentation search index and releases the lock
func CloseDocSearch() error {
	var closeErr error

	// Close index gracefully
	if indexMgr != nil {
		// Atomically swap index to nil (prevents new searches)
		indexPtr := indexMgr.current.Swap(nil)

		if indexPtr != nil {
			log.Printf("Waiting for in-flight searches to complete before closing...")

			// Wait for all in-flight searches to complete
			indexMgr.wg.Wait()

			// Now safe to close index
			index := *indexPtr
			closeErr = index.Close()
			if closeErr != nil {
				log.Printf("Error closing doc index: %v", closeErr)
			} else {
				log.Printf("✓ Doc index closed successfully")
			}
		}
	}

	// Always attempt to release inter-process lock, even if close failed
	if err := releaseLock(); err != nil {
		log.Printf("Error releasing lock: %v", err)
		if closeErr == nil {
			closeErr = err
		}
	}

	return closeErr
}
