package indexing

import (
	"fmt"
	"os"
	"strings"
)

// ForceSplitText splits text by character count at word boundaries
func ForceSplitText(text string, maxChars, overlapChars int) []string {
	var parts []string

	for len(text) > 0 {
		chunkSize := maxChars
		if len(text) < chunkSize {
			chunkSize = len(text)
		}

		// Try to break at word boundary
		if chunkSize < len(text) {
			// Look back for space or newline
			for i := chunkSize; i > chunkSize-100 && i > 0; i-- {
				if text[i] == ' ' || text[i] == '\n' {
					chunkSize = i
					break
				}
			}
		}

		parts = append(parts, text[:chunkSize])

		// Move forward with overlap
		if chunkSize+overlapChars < len(text) {
			text = text[chunkSize-overlapChars:]
		} else {
			text = text[chunkSize:]
		}
	}

	return parts
}

// improveSubchunkTitle extracts the first H3-H5 header from content and uses it as subcategory
// Category (H2 parent) is kept for proper hierarchy
// If no header found, adds a part suffix to the category name
func improveSubchunkTitle(subchunk *DocChunk, originalCategory string, partIndex int) {
	firstHeader := ExtractFirstHeader(subchunk.Content)
	if firstHeader != "" {
		// Use the first H3-H5 header as the specific subcategory
		subchunk.Subcategory = firstHeader
		// Keep category as the H2 parent (already set from chunk.Category)
	} else if partIndex > 0 {
		// No header found, add part suffix to category name
		subchunk.Subcategory = fmt.Sprintf("%s (part %d)", originalCategory, partIndex+1)
		// Keep category as the H2 parent
	}
}

// SubdivideChunk splits a large chunk into smaller ones with overlap
func SubdivideChunk(chunk DocChunk, breadcrumbParts []string, baseURL string) []DocChunk {
	tokens := EstimateTokens(chunk.Content)

	// If chunk is small enough, return as-is with enriched metadata
	if tokens <= MaxChunkTokens {
		EnrichMetadata(&chunk, breadcrumbParts, baseURL)
		return []DocChunk{chunk}
	}

	// Need to subdivide - split by paragraphs
	paragraphs := strings.Split(chunk.Content, "\n\n")
	if len(paragraphs) <= 1 {
		// No paragraph breaks, split by sentences
		paragraphs = strings.Split(chunk.Content, ". ")
		for i := range paragraphs {
			if i < len(paragraphs)-1 {
				paragraphs[i] += "."
			}
		}
	}

	var subchunks []DocChunk
	var currentContent strings.Builder
	var previousContent string
	subchunkIndex := 0
	maxChars := MaxChunkTokens * CharsPerToken
	overlapChars := OverlapTokens * CharsPerToken

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		// If this single paragraph is too large, force-split it
		if EstimateTokens(para) > MaxChunkTokens {
			// Save current buffer first
			if currentContent.Len() > 0 {
				content := currentContent.String()
				if previousContent != "" && len(previousContent) > overlapChars {
					overlap := previousContent[len(previousContent)-overlapChars:]
					content = overlap + "\n\n" + content
				}

				subchunk := DocChunk{
					ID:       fmt.Sprintf("%s_sub%d", chunk.ID, subchunkIndex),
					Subcategory: chunk.Subcategory,
					Page: chunk.Page,
					Category:  chunk.Category,
					Content:  content,
				}
				improveSubchunkTitle(&subchunk, chunk.Category, subchunkIndex)
				EnrichMetadata(&subchunk, breadcrumbParts, baseURL)
				subchunks = append(subchunks, subchunk)
				previousContent = currentContent.String()
				currentContent.Reset()
				subchunkIndex++
			}

			// Force-split the large paragraph
			parts := ForceSplitText(para, maxChars, overlapChars)
			for _, part := range parts {
				subchunk := DocChunk{
					ID:       fmt.Sprintf("%s_sub%d", chunk.ID, subchunkIndex),
					Subcategory: chunk.Subcategory,
					Page: chunk.Page,
					Category:  chunk.Category,
					Content:  part,
				}
				improveSubchunkTitle(&subchunk, chunk.Category, subchunkIndex)
				EnrichMetadata(&subchunk, breadcrumbParts, baseURL)
				subchunks = append(subchunks, subchunk)
				previousContent = part
				subchunkIndex++
			}
			continue
		}

		// Check if adding this paragraph would exceed target
		testContent := currentContent.String()
		if testContent != "" {
			testContent += "\n\n" + para
		} else {
			testContent = para
		}

		if EstimateTokens(testContent) > TargetChunkTokens {
			// Save current chunk
			var content string
			if currentContent.Len() > 0 {
				content = currentContent.String()

				// Add overlap from previous chunk
				if previousContent != "" && len(previousContent) > overlapChars {
					overlap := previousContent[len(previousContent)-overlapChars:]
					content = overlap + "\n\n" + content
				} else if previousContent != "" {
					content = previousContent + "\n\n" + content
				}
			} else {
				content = previousContent
			}

			subchunk := DocChunk{
				ID:       fmt.Sprintf("%s_sub%d", chunk.ID, subchunkIndex),
				Subcategory: chunk.Subcategory,
				Page: chunk.Page,
				Category:  chunk.Category,
				Content:  content,
			}
			improveSubchunkTitle(&subchunk, chunk.Category, subchunkIndex)
			EnrichMetadata(&subchunk, breadcrumbParts, baseURL)
			subchunks = append(subchunks, subchunk)

			previousContent = currentContent.String()
			currentContent.Reset()
			subchunkIndex++
		}

		// Add paragraph to current chunk
		if currentContent.Len() > 0 {
			currentContent.WriteString("\n\n")
		}
		currentContent.WriteString(para)
	}

	// Save final chunk if any content remains
	if currentContent.Len() > 0 {
		content := currentContent.String()

		// Add overlap from previous chunk
		if previousContent != "" && len(previousContent) > overlapChars {
			overlap := previousContent[len(previousContent)-overlapChars:]
			content = overlap + "\n\n" + content
		} else {
			content = previousContent + "\n\n" + content
		}

		subchunk := DocChunk{
			ID:       fmt.Sprintf("%s_sub%d", chunk.ID, subchunkIndex),
			Subcategory: chunk.Subcategory,
			Page: chunk.Page,
			Category:  chunk.Category,
			Content:  content,
		}
		improveSubchunkTitle(&subchunk, chunk.Category, subchunkIndex)
		EnrichMetadata(&subchunk, breadcrumbParts, baseURL)
		subchunks = append(subchunks, subchunk)
	}

	// If we couldn't subdivide effectively, use force-splitting
	if len(subchunks) == 0 {
		// Last resort: split by fixed character chunks
		if len(chunk.Content) > maxChars {
			parts := ForceSplitText(chunk.Content, maxChars, overlapChars)
			for i, part := range parts {
				subchunk := DocChunk{
					ID:       fmt.Sprintf("%s_sub%d", chunk.ID, i),
					Subcategory: chunk.Subcategory,
					Page: chunk.Page,
					Category:  chunk.Category,
					Content:  part,
				}
				improveSubchunkTitle(&subchunk, chunk.Category, i)
				EnrichMetadata(&subchunk, breadcrumbParts, baseURL)
				subchunks = append(subchunks, subchunk)
			}
			return subchunks
		}

		// Small enough after all
		EnrichMetadata(&chunk, breadcrumbParts, baseURL)
		return []DocChunk{chunk}
	}

	return subchunks
}

// ParseDocumentation parses documentation into chunks with intelligent sizing and metadata
func ParseDocumentation(docsFile string) ([]DocChunk, error) {
	content, err := os.ReadFile(docsFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read documentation: %w", err)
	}

	text := string(content)
	lines := strings.Split(text, "\n")

	var finalChunks []DocChunk
	var currentChunk *DocChunk
	var breadcrumbParts []string // Hierarchical trail: ["Config", "Auth", "JWT"]
	var baseURL string           // Base URL from H1 category
	var contentBuilder strings.Builder
	chunkID := 0

	// Helper to save current chunk with intelligent subdivision
	saveCurrentChunk := func() {
		if currentChunk != nil && contentBuilder.Len() > 0 {
			currentChunk.Content = contentBuilder.String()

			// Apply intelligent subdivision and metadata enrichment
			subchunks := SubdivideChunk(*currentChunk, breadcrumbParts, baseURL)
			for i := range subchunks {
				if strings.Contains(subchunks[i].ID, "_sub") {
					// Keep original ID structure for subchunks
				} else {
					// Reindex single chunks
					subchunks[i].ID = fmt.Sprintf("chunk_%d", chunkID)
					chunkID++
				}
			}
			finalChunks = append(finalChunks, subchunks...)

			// Update chunkID for next iteration if subchunks were created
			if len(subchunks) > 1 {
				chunkID += len(subchunks) - 1
			}
		}
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Detect headers
		// Only H1 and H2 create new chunks. H3+ (###, ####, #####) are kept as content
		if strings.HasPrefix(line, "##") && !strings.HasPrefix(line, "###") {
			// Save previous chunk
			saveCurrentChunk()

			// Start new chunk - H2 is a subsection
			title := strings.TrimLeft(line, "#")          // Remove ALL # symbols
			title = strings.TrimSpace(title)              // Clean whitespace
			title = StripMarkdownLinks(title)             // Remove markdown link syntax

			// Update breadcrumb: keep H1 (category), replace H2 if exists
			if len(breadcrumbParts) > 1 {
				breadcrumbParts = breadcrumbParts[:1]
			}
			breadcrumbParts = append(breadcrumbParts, title)

			currentChunk = &DocChunk{
				ID:          fmt.Sprintf("chunk_%d", chunkID),
				Subcategory: title,
				Category:    title,
			}
			if len(breadcrumbParts) > 0 {
				currentChunk.Page = breadcrumbParts[0]
			}
			contentBuilder.Reset()

		} else if strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "##") {
			// Save previous chunk
			saveCurrentChunk()

			// Top-level category (H1)
			rawCategory := strings.TrimLeft(line, "#")     // Remove ALL # symbols
			rawCategory = strings.TrimSpace(rawCategory)   // Clean whitespace

			// Extract URL from markdown link (if present)
			baseURL = ExtractURLFromMarkdown(rawCategory)

			// Strip markdown link syntax to get clean category name
			category := StripMarkdownLinks(rawCategory)
			breadcrumbParts = []string{category}

			// Start new chunk for category
			currentChunk = &DocChunk{
				ID:       fmt.Sprintf("chunk_%d", chunkID),
				Subcategory: category,
				Category:  category,
				Page: category,
			}
			contentBuilder.Reset()

		} else if line != "" && currentChunk != nil {
			// Add content to current chunk using Builder
			if contentBuilder.Len() > 0 {
				contentBuilder.WriteString("\n")
			}
			contentBuilder.WriteString(line)
		}
	}

	// Save last chunk
	saveCurrentChunk()

	return finalChunks, nil
}
