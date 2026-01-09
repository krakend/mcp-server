package tools

import "github.com/blevesearch/bleve/v2"

// Index is an interface that abstracts bleve.Index operations
// This allows for easier testing with mocks
type Index interface {
	// Search executes a search request
	Search(req *bleve.SearchRequest) (*bleve.SearchResult, error)

	// DocCount returns the number of documents in the index
	DocCount() (uint64, error)

	// Close closes the index
	Close() error
}

// bleveIndexWrapper wraps a bleve.Index to implement our Index interface
type bleveIndexWrapper struct {
	index bleve.Index
}

// NewBleveIndexWrapper wraps a bleve.Index
func NewBleveIndexWrapper(index bleve.Index) Index {
	return &bleveIndexWrapper{index: index}
}

func (w *bleveIndexWrapper) Search(req *bleve.SearchRequest) (*bleve.SearchResult, error) {
	return w.index.Search(req)
}

func (w *bleveIndexWrapper) DocCount() (uint64, error) {
	return w.index.DocCount()
}

func (w *bleveIndexWrapper) Close() error {
	return w.index.Close()
}
