package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/blevesearch/bleve/v2"
	"github.com/krakend/mcp-server/internal/indexing"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <docs-file> <index-dir>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nExample:\n")
		fmt.Fprintf(os.Stderr, "  %s docs/llms-full.txt search/index\n", os.Args[0])
		os.Exit(1)
	}

	docsFile := os.Args[1]
	indexDir := os.Args[2]

	log.Printf("KrakenD Documentation Indexer v%d", indexing.IndexSchemaVersion)
	log.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")

	// Step 1: Parse documentation
	log.Printf("Parsing documentation: %s", docsFile)
	chunks, err := indexing.ParseDocumentation(docsFile)
	if err != nil {
		log.Fatalf("Failed to parse documentation: %v", err)
	}

	// Calculate statistics
	totalTokens := 0
	oversized := 0
	for _, chunk := range chunks {
		totalTokens += chunk.TokenCount
		if chunk.TokenCount > indexing.MaxChunkTokens {
			oversized++
		}
	}
	avgTokens := 0
	if len(chunks) > 0 {
		avgTokens = totalTokens / len(chunks)
	}

	log.Printf("✓ Parsed %d chunks (avg: %d tokens, %d oversized)", len(chunks), avgTokens, oversized)

	// Step 2: Remove existing index
	if err := os.RemoveAll(indexDir); err != nil && !os.IsNotExist(err) {
		log.Fatalf("Failed to remove old index: %v", err)
	}

	// Create parent directory
	if err := os.MkdirAll(filepath.Dir(indexDir), 0755); err != nil {
		log.Fatalf("Failed to create index directory: %v", err)
	}

	// Step 3: Create new index
	log.Printf("Creating search index: %s", indexDir)
	mapping := bleve.NewIndexMapping()
	index, err := bleve.New(indexDir, mapping)
	if err != nil {
		log.Fatalf("Failed to create index: %v", err)
	}

	// Step 4: Index chunks in batches
	log.Printf("Indexing %d chunks...", len(chunks))
	batch := index.NewBatch()
	batchSize := 100

	for i, chunk := range chunks {
		if err := batch.Index(chunk.ID, chunk); err != nil {
			index.Close()
			log.Fatalf("Failed to add chunk %s to batch: %v", chunk.ID, err)
		}

		// Submit batch every 100 documents
		if (i+1)%batchSize == 0 {
			if err := index.Batch(batch); err != nil {
				index.Close()
				log.Fatalf("Failed to index batch: %v", err)
			}
			batch = index.NewBatch()
			log.Printf("  Indexed %d/%d chunks...", i+1, len(chunks))
		}
	}

	// Submit remaining
	if batch.Size() > 0 {
		if err := index.Batch(batch); err != nil {
			index.Close()
			log.Fatalf("Failed to index final batch: %v", err)
		}
	}

	// Close index
	if err := index.Close(); err != nil {
		log.Fatalf("Failed to close index: %v", err)
	}

	log.Printf("✓ Indexed %d chunks successfully", len(chunks))

	// Step 5: Write version file
	versionFile := filepath.Join(filepath.Dir(indexDir), ".index_version")
	versionContent := fmt.Sprintf("%d", indexing.IndexSchemaVersion)
	if err := os.WriteFile(versionFile, []byte(versionContent), 0644); err != nil {
		log.Printf("Warning: Failed to write version file: %v", err)
	} else {
		log.Printf("✓ Index schema version: v%d", indexing.IndexSchemaVersion)
	}

	log.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	log.Printf("✓ Indexing complete!")
	log.Printf("")
	log.Printf("Index details:")
	log.Printf("  Location:     %s", indexDir)
	log.Printf("  Total chunks: %d", len(chunks))
	log.Printf("  Avg size:     %d tokens (~%d chars)", avgTokens, avgTokens*indexing.CharsPerToken)
	log.Printf("  Schema:       v%d (optimized chunking with metadata)", indexing.IndexSchemaVersion)
}
