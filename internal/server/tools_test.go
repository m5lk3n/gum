package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"lttl.dev/gum/internal/apt"
)

type okRunner struct{}

func (okRunner) Run(context.Context, bool, ...string) (string, int, error) {
	return "0 upgraded, 0 newly installed, 0 to remove and 0 not upgraded.\n", 0, nil
}

func connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	m := apt.NewManager(apt.Config{Runner: okRunner{}, UpdateMinInterval: time.Minute, JobTimeout: time.Minute})
	s := NewMCPServer(m, BuildInfo{Version: "1.2.3", BuiltBy: "tester"})
	st, ct := mcp.NewInMemoryTransports()
	if _, err := s.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, name string, out any) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("%s returned an error: %+v", name, res.Content)
	}
	b, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatal(err)
	}
}

func TestVersionTool(t *testing.T) {
	var got BuildInfo
	call(t, connect(t), "version", &got)
	if got.Version != "1.2.3" || got.BuiltBy != "tester" || got.GoVersion == "" || got.RestartPending {
		t.Fatalf("unexpected version %+v", got)
	}
}

func TestVersionToolRestartPending(t *testing.T) {
	orig := restartPending
	t.Cleanup(func() { restartPending = orig })
	restartPending = func() bool { return true }

	var got BuildInfo
	call(t, connect(t), "version", &got)
	if !got.RestartPending {
		t.Fatalf("restart_pending not reported: %+v", got)
	}
}

func TestStatusTool(t *testing.T) {
	cs := connect(t)
	var st map[string]any
	call(t, cs, "apt_status", &st)
	if _, ok := st["running"]; ok {
		t.Fatalf("nothing should be running: %v", st)
	}

	var upd apt.UpdateResult
	call(t, cs, "apt_update", &upd)
	call(t, cs, "apt_status", &st)
	last, _ := st["last"].(map[string]any)
	update, _ := last["update"].(map[string]any)
	if update["state"] != apt.StateSucceeded || update["result"] == nil {
		t.Fatalf("unexpected status after update: %v", st)
	}
	if _, ok := st["next_refresh"]; !ok {
		t.Fatalf("next_refresh missing: %v", st)
	}
}
