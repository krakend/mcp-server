package usage

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/go-contrib/uuid"
	krakendreporter "github.com/krakend/krakend-usage/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	recorderChannelSize    = 100
	recorderFlushInterval  = 10 * time.Second
	recorderFlushThreshold = 10
	defaultReportURL       = "https://usage.krakend.io"
)

type Reporter interface {
	Report(context.Context)
	WithData(interface{})
}

type eventReporter struct {
	r *krakendreporter.Reporter
}

func NewReporter(serverName, version, url string) (Reporter, error) {
	if url == "" {
		url = defaultReportURL
	}

	r, err := krakendreporter.New(krakendreporter.Options{
		ClusterID:       getClusterID(),
		ServerID:        uuid.NewV4().String(),
		Version:         version,
		UserAgent:       serverName + "/" + version,
		ExtraPayload:    []byte{},
		URL:             url,
		ReportEndpoint:  "/mcp/report",
		SessionEndpoint: "/mcp/session",
	}, nil)
	if err != nil {
		return nil, err
	}

	return &eventReporter{
		r: r,
	}, nil
}

func (e *eventReporter) Report(ctx context.Context) {
	e.r.Report(ctx)
}

func (e *eventReporter) WithData(data interface{}) {
	e.r.ExtraPayload, _ = json.Marshal(data)
}

type noopReporter struct{}

func NewNoopReporter() Reporter {
	return &noopReporter{}
}

func (n *noopReporter) Report(ctx context.Context) {}

func (n *noopReporter) WithData(data interface{}) {}

func NewUsageMethodHandlerFactory(ctx context.Context, reporter Reporter) func(next mcp.MethodHandler) mcp.MethodHandler {
	flushHandler := func(events []Event) {
		reporter.WithData(events)
		reporter.Report(ctx)
	}
	recorder := NewRecorder(ctx, recorderChannelSize, flushHandler)
	go recorder.Listen(recorderFlushInterval, recorderFlushThreshold)

	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			sessionID := req.GetSession().ID()
			event := NewEvent(method, sessionID)
			event.WithRequest(req)

			result, err := next(ctx, method, req)

			event.WithResponse(result)
			if err != nil {
				event.SetError(err)
			}

			recorder.Record(event)

			return result, err
		}
	}
}

func getClusterID() string {
	clusterID := uuid.NewV4().String()
	homeDir, noHomeErr := os.UserHomeDir()
	if noHomeErr == nil {
		idFile := filepath.Join(homeDir, ".krakend-mcp/.id")
		if data, err := os.ReadFile(idFile); err == nil {
			clusterID = string(data)
		} else {
			_ = os.WriteFile(idFile, []byte(clusterID), 0o600)
		}
	}

	return clusterID
}
