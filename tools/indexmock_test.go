package tools

import (
	"fmt"
	"sync/atomic"

	"github.com/blevesearch/bleve/v2"
)

// mockIndex is a simple in-memory mock of the Index interface for testing
type mockIndex struct {
	id          int
	docCount    uint64
	searchError error
	closeError  error
	closed      atomic.Bool
}

// newMockIndex creates a new mock index with the given ID
func newMockIndex(id int) *mockIndex {
	return &mockIndex{
		id:       id,
		docCount: 100, // Default doc count
	}
}

func (m *mockIndex) Search(req *bleve.SearchRequest) (*bleve.SearchResult, error) {
	if m.closed.Load() {
		return nil, fmt.Errorf("index closed")
	}
	if m.searchError != nil {
		return nil, m.searchError
	}
	// Return minimal valid search result (nil hits is valid)
	return &bleve.SearchResult{
		Request: req,
		Total:   m.docCount,
	}, nil
}

func (m *mockIndex) DocCount() (uint64, error) {
	if m.closed.Load() {
		return 0, fmt.Errorf("index closed")
	}
	return m.docCount, nil
}

func (m *mockIndex) Close() error {
	if m.closed.Load() {
		return fmt.Errorf("already closed")
	}
	m.closed.Store(true)
	return m.closeError
}

// IsClosed returns true if the index has been closed
func (m *mockIndex) IsClosed() bool {
	return m.closed.Load()
}
