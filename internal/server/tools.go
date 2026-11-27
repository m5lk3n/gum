// Package server exposes gummy's apt operations as MCP tools over streamable HTTP.
package server

import (
	"context"
	"net/http"
	"runtime"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"lttl.dev/gummy/internal/apt"
)

// JobInput is the input of the potentially long-running tools.
type JobInput struct {
	Async bool `json:"async,omitempty" jsonschema:"Don't wait: start the job and return immediately (fire and forget). Defaults to false."`
}

// BuildInfo describes the running gummy binary.
type BuildInfo struct {
	Version   string `json:"version"`
	BuiltBy   string `json:"built_by"`
	GoVersion string `json:"go_version"`
	// RestartPending is computed per call, see restartPending.
	RestartPending bool `json:"restart_pending" jsonschema:"true if a newer gummy binary was installed (e.g., by apt_upgrade) and gummy must be restarted (systemctl restart gummy) to run it"`
}

// NewMCPServer creates an MCP server with gummy's tools registered.
func NewMCPServer(m *apt.Manager, build BuildInfo) *mcp.Server {
	if build.GoVersion == "" {
		build.GoVersion = runtime.Version()
	}
	s := mcp.NewServer(&mcp.Implementation{Name: "gummy", Title: "go-util-mcp", Version: build.Version}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name:  "version",
		Title: "Show gummy version",
		Description: "Return the version of the running gummy MCP server, who built it, the Go version " +
			"it was built with, and whether a restart is pending because a newer gummy binary was installed.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: new(false)},
	}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, BuildInfo, error) {
		info := build
		info.RestartPending = restartPending()
		return nil, info, nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:  "apt_status",
		Title: "Show apt job status",
		Description: "Report the apt job currently running (if any), the outcome of the most recent " +
			"update, upgrade and autoremove jobs (including async ones), and when the package index " +
			"was last refreshed and may be refreshed again. Does not run apt.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(false)},
	}, func(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, apt.Status, error) {
		return nil, m.Status(), nil
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:  "apt_update",
		Title: "Retrieve new lists of packages",
		Description: "Refresh the apt package index (apt-get update) and return how many installed " +
			"packages can be upgraded. Throttled: if the index was refreshed recently, the network " +
			"refresh is skipped (refreshed=false) and the count is recomputed from the existing index.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true, OpenWorldHint: new(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, apt.UpdateResult, error) {
		res, err := m.Update(ctx)
		return nil, res, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:  "apt_upgrade",
		Title: "Perform an upgrade",
		Description: "Upgrade installed packages (apt-get upgrade -y); packages that would need new " +
			"dependencies or removals are kept back. Run apt_update first. Potentially long-running: " +
			"set async=true to return immediately. Only one apt job runs at a time.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), OpenWorldHint: new(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in JobInput) (*mcp.CallToolResult, apt.JobResult, error) {
		res, err := m.Upgrade(ctx, in.Async)
		return nil, res, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:  "apt_autoremove",
		Title: "Remove automatically all unused packages",
		Description: "Remove packages that were installed as dependencies and are no longer needed " +
			"(apt-get autoremove -y). Potentially long-running: set async=true to return immediately. " +
			"Only one apt job runs at a time.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in JobInput) (*mcp.CallToolResult, apt.JobResult, error) {
		res, err := m.Autoremove(ctx, in.Async)
		return nil, res, err
	})

	return s
}

// NewHandler returns the HTTP handler serving the MCP endpoint, protected by
// a bearer token if token is non-empty.
func NewHandler(s *mcp.Server, token string) http.Handler {
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return s }, nil)
	return RequireToken(token, h)
}
