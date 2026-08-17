// Copyright (c) 2026 TREVARIX Corp.
// SPDX-License-Identifier: AGPL-3.0-or-later

package avc

import (
	"os"

	"github.com/trevarix/agentic-vc/avc/internal/mcp"
	"github.com/spf13/cobra"
)

var (
	mcpCompact   bool
	mcpToolsTier string
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "MCP server for agent integration",
	Long:  `Commands for running AVC as a Model Context Protocol (MCP) server.`,
}

var mcpServeCmd = &cobra.Command{
	Use:   "serve [search-root...]",
	Short: "Start an MCP JSON-RPC 2.0 server over stdio",
	Long: `Starts an MCP server that exposes AVC operations as tools.
Configure your agent framework to run: avc mcp serve
The server resolves the AVC project from the current working directory.

Search roots — for hosts with no working directory:
Claude Desktop launches the server outside any project, so it cannot resolve
one from the CWD. Pass one or more directories to search for AVC projects
instead:

  avc mcp serve ~/Projects ~/work

The server then exposes avc_projects_list and avc_project_use so the agent can
pick between them, and selects automatically when only one project is found.

Tool tiers (--tools):
  core      4 tools: snapshot, list, diff, restore
  standard  11 tools: core + branch (create/list/switch/diff) + merge + merge_abort + status  (default)
  full      All ~24 tools including annotate, run, tag, conflict resolution, etc.`,
	RunE: runMCPServe,
}

func init() {
	mcpServeCmd.Flags().BoolVar(&mcpCompact, "compact", false, "Emit compact JSON instead of pretty-printed")
	mcpServeCmd.Flags().StringVar(&mcpToolsTier, "tools", "standard",
		"Tool tier to advertise: core (4), standard (11, default), or full (~24)")
	mcpCmd.AddCommand(mcpServeCmd)
}

func runMCPServe(cmd *cobra.Command, args []string) error {
	// Positional arguments are project search roots. They only apply when no
	// single project is pinned, so a host that knows its project keeps the
	// simpler behaviour.
	roots := args

	// AVC_PROJECT allows Claude Desktop (and other launchers that don't set a
	// meaningful CWD) to specify the project root explicitly via the env block
	// in their MCP config.
	projectPath := os.Getenv("AVC_PROJECT")
	if projectPath == "" && len(roots) == 0 {
		// Best-effort CWD walk — if no project is found, start in projectless
		// mode so the server stays alive. Tool calls will return a clear error
		// directing the user to run `avc init`. This prevents the server from
		// exiting when launched globally (e.g. from ~/.claude/settings.json)
		// where CWD may not be an AVC project.
		projectPath, _ = requireInitializedProject()
	}
	return mcp.Serve(projectPath, roots, mcpCompact, mcpToolsTier)
}
