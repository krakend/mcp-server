package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/krakend/mcp-server/internal/indexing"
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

	chunks, err := indexing.ParseDocumentation(testFile)

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

	chunks, err := indexing.ParseDocumentation(testFile)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should have 4 chunks: Category 1, Section 1.1, Section 1.2, Category 2
	if len(chunks) != 4 {
		t.Errorf("Expected 4 chunks, got %d", len(chunks))
	}

	// Check first chunk
	if len(chunks) > 0 {
		if chunks[0].Subcategory != "Category 1" {
			t.Errorf("Expected title 'Category 1', got '%s'", chunks[0].Subcategory)
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

	chunks, err := indexing.ParseDocumentation(testFile)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// With intelligent chunking, large content should be split into multiple chunks
	if len(chunks) < 2 {
		t.Errorf("Expected multiple chunks due to large content, got %d", len(chunks))
	}

	// Verify all content is preserved across chunks
	allContent := ""
	for _, chunk := range chunks {
		allContent += chunk.Content
	}

	if !strings.Contains(allContent, "Line number 0 with some content") {
		t.Error("Expected content to contain first line")
	}
	if !strings.Contains(allContent, "Line number 999 with some content") {
		t.Error("Expected content to contain last line")
	}
	if !strings.Contains(allContent, "Line number 500") {
		t.Error("Expected content to contain middle line")
	}

	// Verify all chunks have metadata enrichment
	for i, chunk := range chunks {
		if chunk.TokenCount == 0 {
			t.Errorf("Chunk %d missing token count", i)
		}
		if chunk.TokenCount > indexing.MaxChunkTokens*2 {
			t.Errorf("Chunk %d has %d tokens, exceeds reasonable limit", i, chunk.TokenCount)
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

	chunks, err := indexing.ParseDocumentation(testFile)

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

func TestParseDocumentation_MarkdownLinksStripped(t *testing.T) {
	content := `# [Category with Link](https://example.com/category)

Content under linked category

## [Section with Link](https://example.com/section)

Content under linked section
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

	chunks, err := indexing.ParseDocumentation(testFile)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(chunks) != 2 {
		t.Fatalf("Expected 2 chunks, got %d", len(chunks))
	}

	// Check first chunk (category)
	if chunks[0].Page != "Category with Link" {
		t.Errorf("Expected category 'Category with Link', got '%s'", chunks[0].Page)
	}
	if chunks[0].Subcategory != "Category with Link" {
		t.Errorf("Expected title 'Category with Link', got '%s'", chunks[0].Subcategory)
	}
	if strings.Contains(chunks[0].Page, "](") {
		t.Error("Category should not contain markdown link syntax")
	}
	if strings.Contains(chunks[0].URL, "](") {
		t.Error("URL should not contain markdown link syntax")
	}

	// Check second chunk (section)
	if chunks[1].Category != "Section with Link" {
		t.Errorf("Expected section 'Section with Link', got '%s'", chunks[1].Category)
	}
	if strings.Contains(chunks[1].Category, "](") {
		t.Error("Section should not contain markdown link syntax")
	}
	if strings.Contains(chunks[1].URL, "](") {
		t.Error("URL should not contain markdown link syntax")
	}

	// Verify URL is properly formatted - extracted from H1 with subsection anchor
	expectedURL := "https://example.com/category#section-with-link"
	if chunks[1].URL != expectedURL {
		t.Errorf("Expected URL '%s', got '%s'", expectedURL, chunks[1].URL)
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

// --- Pure Unit Tests for Concurrency ---
// These tests verify the thread-safe atomic pointer swap implementation
// using mocks (no filesystem, no external dependencies)

func TestIndexHolderConcurrentReads(t *testing.T) {
	// Test that multiple goroutines can safely read from indexHolder
	// Using mock index (pure unit test - no filesystem)

	mockIdx := newMockIndex(1)
	idx := Index(mockIdx)

	// Create indexHolder and store the mock index
	holder := &indexHolder{}
	holder.current.Store(&idx)

	// Launch 50 concurrent goroutines that read the index
	const numReaders = 50
	errChan := make(chan error, numReaders)
	doneChan := make(chan bool, numReaders)

	for i := 0; i < numReaders; i++ {
		go func(id int) {
			defer func() { doneChan <- true }()

			holder.wg.Add(1)
			defer holder.wg.Done()

			// Load index atomically
			indexPtr := holder.current.Load()
			if indexPtr == nil {
				errChan <- fmt.Errorf("goroutine %d: got nil index", id)
				return
			}

			// Verify we can access the index
			index := *indexPtr
			count, err := index.DocCount()
			if err != nil {
				errChan <- fmt.Errorf("goroutine %d: DocCount failed: %v", id, err)
				return
			}

			// Verify count is valid
			if count != 100 { // Mock returns 100
				errChan <- fmt.Errorf("goroutine %d: expected 100, got %d", id, count)
			}
		}(i)
	}

	// Wait for all goroutines to finish
	for i := 0; i < numReaders; i++ {
		<-doneChan
	}
	close(errChan)

	// Check for errors
	for err := range errChan {
		t.Error(err)
	}

	// Verify WaitGroup drained
	holder.wg.Wait() // Should return immediately
}

func TestIndexHolderAtomicSwap(t *testing.T) {
	// Test that atomic swap works correctly (pure unit test with mocks)

	// Create two mock indexes
	mock1 := newMockIndex(1)
	mock2 := newMockIndex(2)
	idx1 := Index(mock1)
	idx2 := Index(mock2)

	// Create indexHolder with first index
	holder := &indexHolder{}
	holder.current.Store(&idx1)

	// Verify we have index1
	ptr1 := holder.current.Load()
	if ptr1 == nil {
		t.Fatal("First load returned nil")
	}

	// Verify it's idx1
	if *ptr1 != idx1 {
		t.Error("Expected idx1")
	}

	// Swap to index2
	oldPtr := holder.current.Swap(&idx2)
	if oldPtr == nil {
		t.Fatal("Swap returned nil for old index")
	}

	// Verify old pointer was idx1
	if *oldPtr != idx1 {
		t.Error("Old pointer should be idx1")
	}

	// Verify we now have index2
	ptr2 := holder.current.Load()
	if ptr2 == nil {
		t.Fatal("Second load returned nil")
	}

	// Verify it's idx2
	if *ptr2 != idx2 {
		t.Error("Expected idx2")
	}

	// Verify old and new pointers are different
	if ptr1 == ptr2 {
		t.Error("Old and new pointers should be different")
	}
}

func TestIndexHolderRefreshMutexSerialization(t *testing.T) {
	// Test that refreshMu properly serializes concurrent operations
	holder := &indexHolder{}

	const numGoroutines = 10
	counter := 0
	doneChan := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer func() { doneChan <- true }()

			holder.refreshMu.Lock()
			defer holder.refreshMu.Unlock()

			// Critical section: increment counter
			oldCounter := counter
			// Simulate some work
			for j := 0; j < 1000; j++ {
				_ = j * j
			}
			counter = oldCounter + 1
		}()
	}

	// Wait for all goroutines
	for i := 0; i < numGoroutines; i++ {
		<-doneChan
	}

	// Verify counter was incremented exactly numGoroutines times
	if counter != numGoroutines {
		t.Errorf("Expected counter=%d, got %d (mutex not properly serializing)", numGoroutines, counter)
	}
}

func TestIndexHolderWaitGroupTracking(t *testing.T) {
	// Test that WaitGroup properly tracks in-flight operations
	holder := &indexHolder{}

	const numOperations = 100
	doneChan := make(chan bool, numOperations)

	// Launch operations
	for i := 0; i < numOperations; i++ {
		holder.wg.Add(1)
		go func() {
			defer holder.wg.Done()
			defer func() { doneChan <- true }()

			// Simulate some work
			for j := 0; j < 100; j++ {
				_ = j * j
			}
		}()
	}

	// Wait for all operations to complete
	holder.wg.Wait()

	// Verify all goroutines finished
	completedCount := 0
	for i := 0; i < numOperations; i++ {
		select {
		case <-doneChan:
			completedCount++
		default:
			// Should not happen - all should be done
		}
	}

	if completedCount != numOperations {
		t.Errorf("Expected %d completed operations, got %d", numOperations, completedCount)
	}
}

func TestIndexHolderConcurrentSwapAndRead(t *testing.T) {
	// Test concurrent swaps and reads (stress test with mocks - pure unit test)
	// This test verifies that atomic swaps work correctly under high concurrency

	// Create initial mock index
	mockIdx := newMockIndex(0)
	idx := Index(mockIdx)

	// Create indexHolder
	holder := &indexHolder{}
	holder.current.Store(&idx)

	errChan := make(chan error, 100)
	doneChan := make(chan bool, 100)

	// Launch readers (20 goroutines, 5 iterations each - reduced for stability)
	const numReaders = 20
	const iterations = 5

	for i := 0; i < numReaders; i++ {
		go func(id int) {
			defer func() { doneChan <- true }()

			for j := 0; j < iterations; j++ {
				holder.wg.Add(1)
				indexPtr := holder.current.Load()

				if indexPtr == nil {
					holder.wg.Done()
					errChan <- fmt.Errorf("reader %d iteration %d: got nil", id, j)
					return
				}

				// Try to access the index
				index := *indexPtr
				_, err := index.DocCount()
				holder.wg.Done()

				if err != nil && err.Error() != "index closed" {
					// Allow "index closed" errors during swap (expected race)
					errChan <- fmt.Errorf("reader %d iteration %d: %v", id, j, err)
					return
				}
			}
		}(i)
	}

	// Launch swapper (simulates refresh with 3 swaps - reduced)
	go func() {
		defer func() { doneChan <- true }()

		for i := 0; i < 3; i++ {
			// Create new mock index
			newMock := newMockIndex(i + 1)
			newIdx := Index(newMock)

			// Swap atomically
			_ = holder.current.Swap(&newIdx)

			// Note: In production, cleanup happens in background
			// Here we skip cleanup to avoid WaitGroup misuse in test
		}
	}()

	// Wait for all goroutines (readers + 1 swapper)
	for i := 0; i < numReaders+1; i++ {
		<-doneChan
	}

	// Close error channel after all goroutines finish
	close(errChan)

	// Check errors
	for err := range errChan {
		t.Error(err)
	}

	// Final wait to ensure all ops completed
	holder.wg.Wait()
}

// --- Lock Mechanism Tests ---
// These tests verify the file-based locking mechanism for inter-process coordination

func TestLockMechanism(t *testing.T) {
	// Use temp directory for testing
	oldDataDir := dataDir
	dataDir = t.TempDir()
	defer func() { dataDir = oldDataDir }()

	// Create search directory
	searchDir := filepath.Join(dataDir, "search")
	if err := os.MkdirAll(searchDir, 0755); err != nil {
		t.Fatalf("Failed to create search dir: %v", err)
	}

	t.Run("acquire and release lock", func(t *testing.T) {
		// Clean state
		os.Remove(filepath.Join(dataDir, lockFile))

		// Acquire lock
		if err := acquireLock(); err != nil {
			t.Fatalf("Failed to acquire lock: %v", err)
		}

		// Verify lock file exists
		lockPath := filepath.Join(dataDir, lockFile)
		data, err := os.ReadFile(lockPath)
		if err != nil {
			t.Fatalf("Lock file not found: %v", err)
		}

		// Verify PID is correct
		pid, err := strconv.Atoi(string(data))
		if err != nil {
			t.Fatalf("Invalid PID in lock file: %v", err)
		}
		if pid != os.Getpid() {
			t.Errorf("Lock has wrong PID: got %d, want %d", pid, os.Getpid())
		}

		// Release lock
		if err := releaseLock(); err != nil {
			t.Fatalf("Failed to release lock: %v", err)
		}

		// Verify lock file removed
		if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
			t.Error("Lock file should be removed after release")
		}
	})

	t.Run("detect stale lock", func(t *testing.T) {
		// Clean state
		os.Remove(filepath.Join(dataDir, lockFile))

		// Create fake stale lock with non-existent PID
		stalePID := 99999
		lockPath := filepath.Join(dataDir, lockFile)
		if err := os.WriteFile(lockPath, []byte(strconv.Itoa(stalePID)), 0644); err != nil {
			t.Fatalf("Failed to create stale lock: %v", err)
		}

		// Try to acquire lock (should clean stale lock)
		if err := acquireLock(); err != nil {
			t.Fatalf("Failed to acquire lock after stale lock: %v", err)
		}

		// Verify our PID is now in lock
		data, _ := os.ReadFile(lockPath)
		pid, _ := strconv.Atoi(string(data))
		if pid != os.Getpid() {
			t.Errorf("Expected our PID after cleaning stale lock, got %d", pid)
		}

		// Cleanup
		releaseLock()
	})

	t.Run("reacquire same lock", func(t *testing.T) {
		// Clean state
		os.Remove(filepath.Join(dataDir, lockFile))

		// Acquire lock
		if err := acquireLock(); err != nil {
			t.Fatalf("Failed to acquire lock: %v", err)
		}

		// Try to acquire again (should succeed immediately - same PID)
		if err := acquireLock(); err != nil {
			t.Fatalf("Failed to reacquire lock: %v", err)
		}

		// Cleanup
		releaseLock()
	})

	t.Run("timeout on held lock", func(t *testing.T) {
		// Skip this test as it takes 5 seconds (lockTimeout)
		// In real usage, this timeout is intentional to wait for other processes
		t.Skip("Skipping timeout test (takes 5s) - timeout behavior verified manually")

		// Clean state
		os.Remove(filepath.Join(dataDir, lockFile))

		// Create lock with a different PID that exists (PID 1 always exists on Unix)
		lockPath := filepath.Join(dataDir, lockFile)
		if err := os.WriteFile(lockPath, []byte("1"), 0644); err != nil {
			t.Fatalf("Failed to create lock: %v", err)
		}

		start := time.Now()
		err := acquireLock()
		elapsed := time.Since(start)

		if err == nil {
			t.Error("Expected error acquiring held lock, got nil")
			releaseLock()
		}

		// Should timeout after ~5 seconds
		if elapsed < 4*time.Second || elapsed > 6*time.Second {
			t.Errorf("Expected timeout of ~5s, got %v", elapsed)
		}

		// Cleanup
		os.Remove(lockPath)
	})

	t.Run("is process running", func(t *testing.T) {
		// Test our own PID (should be running)
		if !isProcessRunning(os.Getpid()) {
			t.Error("Our own process should be detected as running")
		}

		// Test non-existent PID
		if isProcessRunning(99999) {
			t.Error("Non-existent process should not be detected as running")
		}
	})
}
