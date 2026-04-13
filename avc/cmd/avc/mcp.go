package avc

import (
	"github.com/SkillMythOrg/agentic-vc/avc/internal/mcp"
	"github.com/spf13/cobra"
)

var mcpCompact bool

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "MCP server for agent integration",
	Long:  `Commands for running AVC as a Model Context Protocol (MCP) server.`,
}

var mcpServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start an MCP JSON-RPC 2.0 server over stdio",
	Long: `Starts an MCP server that exposes AVC operations as tools.
Configure your agent framework to run: avc mcp serve
The server resolves the AVC project from the current working directory.

Available tools:
  avc_snapshot, avc_list, avc_diff, avc_restore, avc_info, avc_delete
  avc_branch_create, avc_branch_list, avc_branch_switch, avc_branch_diff`,
	RunE: runMCPServe,
}

func init() {
	mcpServeCmd.Flags().BoolVar(&mcpCompact, "compact", false, "Emit compact JSON instead of pretty-printed")
	mcpCmd.AddCommand(mcpServeCmd)
}

func runMCPServe(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}
	return mcp.Serve(projectPath, mcpCompact)
}
