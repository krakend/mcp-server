package indexing

// Chunking strategy constants
const (
	// TargetChunkTokens is the optimal chunk size (~2000 chars)
	TargetChunkTokens = 500

	// MaxChunkTokens is the maximum before subdividing (~3200 chars)
	MaxChunkTokens = 800

	// OverlapTokens is the overlap between consecutive chunks (~400 chars)
	OverlapTokens = 100

	// CharsPerToken is the approximation for token estimation
	CharsPerToken = 4

	// IndexSchemaVersion increments when chunking logic changes
	// v1: basic chunking (line-based), v2: optimized chunking with metadata
	IndexSchemaVersion = 2
)
