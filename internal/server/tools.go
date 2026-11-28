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
	Confirm bool `json:"confirm,omitempty" jsonschema:"Run the job for real (apt-get -y). Defaults to false: only preview what the job would do, without changing anything."`
	Async   bool `json:"async,omitempty" jsonschema:"Don't wait: start the job and return immediately (fire and forget). Requires confirm=true. Defaults to false."`
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
	s := mcp.NewServer(&mcp.Implementation{Name: "gummy", Title: "gummy (apt-get MCP server)", Version: build.Version}, nil)

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
		Description: "Upgrade installed packages; packages that would need new dependencies or " +
			"removals are kept back. Without confirm=true, only previews the upgrade (apt-get -s " +
			"upgrade) and changes nothing; with confirm=true, upgrades (apt-get upgrade -y). Returns " +
			"the names of the packages that are (or would be) upgraded and kept back. Run apt_update " +
			"first. A confirmed upgrade is potentially long-running: set async=true to return " +
			"immediately. Only one apt job runs at a time.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true), OpenWorldHint: new(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in JobInput) (*mcp.CallToolResult, apt.JobResult, error) {
		res, err := m.Upgrade(ctx, apt.JobOptions{Confirm: in.Confirm, Async: in.Async})
		return nil, res, err
	})

	mcp.AddTool(s, &mcp.Tool{
		Name:  "apt_autoremove",
		Title: "Remove automatically all unused packages",
		Description: "Remove packages that were installed as dependencies and are no longer needed, " +
			"and return their names. Without confirm=true, only previews the removal (apt-get -s " +
			"autoremove) and changes nothing; with confirm=true, removes them (apt-get autoremove -y). " +
			"A confirmed autoremove is potentially long-running: set async=true to return " +
			"immediately. Only one apt job runs at a time.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: new(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in JobInput) (*mcp.CallToolResult, apt.JobResult, error) {
		res, err := m.Autoremove(ctx, apt.JobOptions{Confirm: in.Confirm, Async: in.Async})
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
