package usage

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Event struct {
	When      time.Time
	Method    string
	ToolName  string
	SessionID string
	Duration  time.Duration
	Error     error
	Meta      map[string]interface{}
}

func NewEvent(method, sessionID string) Event {
	return Event{
		When:      time.Now(),
		Method:    method,
		SessionID: sessionID,
	}
}

func (e *Event) SetError(err error) {
	e.Error = err
}

func (e *Event) WithRequest(req mcp.Request) {
	if e.Method == "tools/call" {
		if params, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok {
			e.ToolName = params.Name
		}
	}
}

func (e *Event) WithResponse(resp mcp.Result) {
	v, ok := resp.(*mcp.CallToolResult)
	if !ok {
		return
	}
	e.Meta = v.GetMeta()
}

type Recorder struct {
	ctx          context.Context
	flushHandler func([]Event)
	in           chan Event
}

func NewRecorder(ctx context.Context, size int, flushHandler func([]Event)) *Recorder {
	return &Recorder{
		ctx:          ctx,
		flushHandler: flushHandler,
		in:           make(chan Event, size),
	}
}

func (r *Recorder) Record(e Event) {
	e.Duration = time.Since(e.When)
	select {
	case r.in <- e:
	default:
		// Drop event if channel is full
	}
}

func (r *Recorder) Listen(flushInterval time.Duration, flushThreshold int) {
	var events []Event
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case e := <-r.in:
			events = append(events, e)
			if len(events) < flushThreshold {
				continue
			}
		case <-ticker.C:
			if len(events) == 0 {
				continue
			}
		case <-r.ctx.Done():
			r.flushHandler(events)
			return
		}

		go r.flushHandler(events)
		events = []Event{}
	}
}
