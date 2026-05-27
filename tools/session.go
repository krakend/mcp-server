package tools

import (
	"crypto/rand"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stdioFallbackID is a stable, per-process identifier used when the MCP
// transport does not assign a session ID (e.g. stdio connections). A fresh
// random value is generated once at startup, so each process gets its own
// key in the hints cooldown file rather than sharing the global "stdio" key
// across all concurrent or sequential stdio sessions on the same machine.
var stdioFallbackID = rand.Text()

var sessionRegistry = &SessionRegistry{
	sessions: make(map[string]*session),
}

type SessionRegistry struct {
	mu       sync.Mutex
	sessions map[string]*session
}

type session struct {
	isEnterprise bool
	counters     map[string]int
}

func (r *SessionRegistry) Increase(sessionId, counter string) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	sess := r.getSession(sessionId)

	sess.counters[counter]++
	return sess.counters[counter]
}

func (r *SessionRegistry) Reset(sessionId, counter string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	sess := r.getSession(sessionId)
	sess.counters[counter] = 0
}

func (r *SessionRegistry) MarkAsEnterprise(sessionId string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	sess := r.getSession(sessionId)
	sess.isEnterprise = true
}

func (r *SessionRegistry) IsEnterprise(sessionId string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	sess := r.getSession(sessionId)
	return sess.isEnterprise
}

func (r *SessionRegistry) getSession(sessionId string) *session {
	sess, exists := r.sessions[sessionId]
	if !exists {
		sess = &session{
			counters: make(map[string]int),
		}
		r.sessions[sessionId] = sess
	}

	return sess
}

func sessionIdFromReq(req *mcp.CallToolRequest) string {
	if ss, ok := req.GetSession().(*mcp.ServerSession); ok && ss != nil {
		if id := ss.ID(); id != "" {
			return id
		}
	}
	return stdioFallbackID
}
