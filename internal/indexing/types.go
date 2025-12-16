package indexing

// DocChunk represents a documentation chunk in the search index
type DocChunk struct {
	ID          string   `json:"id"`
	Page        string   `json:"page"`                  // H1 - Top-level page/document
	Category    string   `json:"category"`              // H2 - Category within the page
	Subcategory string   `json:"subcategory"`           // H3+ - Specific subcategory
	Content     string   `json:"content"`
	URL         string   `json:"url,omitempty"`
	Breadcrumb  string   `json:"breadcrumb,omitempty"`  // Full hierarchy: "Page > Category > Subcategory"
	Keywords    []string `json:"keywords,omitempty"`    // Key terms extracted from content
	TokenCount  int      `json:"token_count,omitempty"` // Estimated token count for monitoring
}
