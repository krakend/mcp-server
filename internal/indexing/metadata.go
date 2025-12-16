package indexing

import (
	"regexp"
	"strings"
)

var markdownLinkRegex = regexp.MustCompile(`\[([^\]]+)\]\([^\)]+\)`)
var urlExtractRegex = regexp.MustCompile(`\[([^\]]+)\]\(([^\)]+)\)`)

// StripMarkdownLinks removes markdown link syntax, keeping only the text
// Example: "[Text](url)" -> "Text"
func StripMarkdownLinks(text string) string {
	return markdownLinkRegex.ReplaceAllString(text, "$1")
}

// ExtractURLFromMarkdown extracts the URL from a markdown link
// Example: "[Text](https://example.com)" -> "https://example.com"
func ExtractURLFromMarkdown(text string) string {
	matches := urlExtractRegex.FindStringSubmatch(text)
	if len(matches) > 2 {
		return matches[2]
	}
	return ""
}

// EstimateTokens estimates the token count for a text string
func EstimateTokens(text string) int {
	return len(text) / CharsPerToken
}

// ExtractKeywords extracts key terms from title and content
func ExtractKeywords(title, content string) []string {
	// Simple keyword extraction: get significant words from title
	// and first few lines of content
	words := strings.Fields(strings.ToLower(title))

	// Add words from first 200 chars of content
	contentPreview := content
	if len(content) > 200 {
		contentPreview = content[:200]
	}
	words = append(words, strings.Fields(strings.ToLower(contentPreview))...)

	// Filter out common stop words and short words
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true,
		"but": true, "in": true, "on": true, "at": true, "to": true,
		"for": true, "of": true, "as": true, "by": true, "is": true,
		"it": true, "be": true, "with": true, "from": true, "that": true,
	}

	keywordMap := make(map[string]bool)
	for _, word := range words {
		word = strings.TrimFunc(word, func(r rune) bool {
			return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
		})
		if len(word) > 2 && !stopWords[word] {
			keywordMap[word] = true
		}
	}

	// Convert map to slice
	keywords := make([]string, 0, len(keywordMap))
	for word := range keywordMap {
		keywords = append(keywords, word)
	}

	// Limit to 10 keywords
	if len(keywords) > 10 {
		keywords = keywords[:10]
	}

	return keywords
}

// CreateAnchor creates a URL anchor from text
// Example: "Fields of Tiered Rate Limit" -> "fields-of-tiered-rate-limit"
func CreateAnchor(text string) string {
	// Convert to lowercase and replace spaces with hyphens
	anchor := strings.ToLower(strings.TrimSpace(text))
	anchor = strings.ReplaceAll(anchor, " ", "-")
	// Remove special characters except hyphens
	anchor = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return -1
	}, anchor)
	// Remove backticks and other markdown syntax
	anchor = strings.ReplaceAll(anchor, "`", "")
	return anchor
}

// ExtractFirstHeader extracts the first H3-H5 header from content
// Returns the header text without # symbols, or empty string if none found
func ExtractFirstHeader(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Look for H3, H4, or H5 headers (###, ####, #####)
		if strings.HasPrefix(line, "###") {
			header := strings.TrimLeft(line, "#")
			header = strings.TrimSpace(header)
			header = StripMarkdownLinks(header)
			return header
		}
	}
	return ""
}

// EnrichMetadata adds breadcrumb, keywords, URL, and token count to a chunk
// baseURL is the URL from the H1 category (extracted from markdown link)
func EnrichMetadata(chunk *DocChunk, breadcrumbParts []string, baseURL string) {
	// Build breadcrumb from chunk fields (Page > Category > Subcategory)
	var breadcrumb []string
	if chunk.Page != "" {
		breadcrumb = append(breadcrumb, chunk.Page)
	}
	if chunk.Category != "" && chunk.Category != chunk.Page {
		breadcrumb = append(breadcrumb, chunk.Category)
	}
	if chunk.Subcategory != "" && chunk.Subcategory != chunk.Category {
		breadcrumb = append(breadcrumb, chunk.Subcategory)
	}
	if len(breadcrumb) > 0 {
		chunk.Breadcrumb = strings.Join(breadcrumb, " > ")
	}

	// Set URL
	if baseURL != "" {
		if len(breadcrumbParts) > 1 {
			// Subsection - add anchor from subcategory
			anchor := CreateAnchor(chunk.Subcategory)
			chunk.URL = baseURL + "#" + anchor
		} else {
			// Top-level page - use base URL as-is
			chunk.URL = baseURL
		}
	}

	// Extract keywords from subcategory and content
	chunk.Keywords = ExtractKeywords(chunk.Subcategory, chunk.Content)

	// Estimate token count
	chunk.TokenCount = EstimateTokens(chunk.Content)
}
