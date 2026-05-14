package tools

import (
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

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
	return "stdio"
}
