package avc

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

func initHelp() {
	rootCmd.SilenceUsage = true
	rootCmd.SilenceErrors = true
	rootCmd.SetHelpFunc(avcHelpFunc)
}

func avcHelpFunc(cmd *cobra.Command, _ []string) {
	if cmd == rootCmd {
		printRootHelp()
	} else {
		printSubcmdHelp(cmd)
	}
}

// printRootHelp renders the grouped top-level help shown by `avc` or `avc --help`.
func printRootHelp() {
	// bannerPlain is the visible text inside the box — used only for width counting.
	// ▲ and · are multi-byte in UTF-8, so we use RuneCountInString, not len().
	const bannerPlain = " ▲  AVC  ·  Agentic Version Control "
	innerWidth := utf8.RuneCountInString(bannerPlain)
	border := strings.Repeat("─", innerWidth)

	// bannerColored has the same visible runes as bannerPlain, just styled.
	bannerColored := " " + accent("▲  AVC") + dim("  ·  ") + "Agentic Version Control "

	fmt.Println()
	fmt.Printf("  %s\n", dim("╭"+border+"╮"))
	fmt.Printf("  %s%s%s\n", dim("│"), bannerColored, dim("│"))
	fmt.Printf("  %s\n\n", dim("╰"+border+"╯"))

	helpSection("SNAPSHOTS")
	helpEntry("snapshot <label>", "Create a snapshot of the current state")
	helpEntry("watch [--status]", "Continuously checkpoint as files change")
	helpEntry("list", "List snapshots on the active branch")
	helpEntry("log", "Show snapshot history as a tree")
	helpEntry("restore <snapshot-id>", "Restore the project to a snapshot")
	helpEntry("undo", "Reverse the most recent restore or merge")
	helpEntry("diff <snapshot-id> <snapshot-id>", "Compare two snapshots")
	helpEntry("info <snapshot-id>", "Snapshot details and file list")
	helpEntry("delete <snapshot-id>", "Delete a snapshot")
	fmt.Println()

	helpSection("BRANCHES")
	helpEntry("branch create <branch>", "Create a new branch (agent workspace)")
	helpEntry("branch list", "List all branches")
	helpEntry("branch switch <branch>", "Switch to a branch")
	helpEntry("branch diff [branch]", "Show cumulative changes on a branch")
	helpEntry("branch delete <branch>", "Delete a branch")
	helpEntry("branch create <name> --from-branch <p>", "Stack a branch on another branch")
	helpEntry("branch diff <a>..<b>", "Compare two branches' latest snapshots")
	helpEntry("merge <branch>", "Merge a branch back into main")
	helpEntry("merge --train <branch>...", "Merge several branches in order")
	fmt.Println()

	helpSection("FILE INSPECTION")
	helpEntry("diff-current <snapshot-id>", "Compare a snapshot to the current working tree")
	helpEntry("file-history <file>", "Find snapshots that contain a file")
	helpEntry("restore-file <snapshot-id> <file>", "Restore a single file from a snapshot")
	helpEntry("annotate <file>", "Line-by-line blame across snapshot history")
	helpEntry("cat <snapshot-id> <file>", "Print a file's content from a snapshot")
	fmt.Println()

	helpSection("SEARCH & STATUS")
	helpEntry("search <query>", "Search snapshot labels and notes")
	helpEntry("status", "Compare working tree to the last snapshot")
	helpEntry("timeline [--session <id>]", "Branch history grouped by agent session")
	fmt.Println()

	helpSection("SETUP & AGENT INTEGRATION")
	helpEntry("init [directory]", "Initialize AVC for a project (run once)")
	helpEntry("ui", "Start the web UI (default port 3004)")
	helpEntry("mcp serve", "Start the MCP server for agent frameworks")
	helpEntry("run --branch <branch> <command>", "Run a command in a branch workspace")
	helpEntry("bisect --good <id> --cmd <command>", "Find the snapshot that broke a command")
	fmt.Println()

	helpSection("PORTABILITY")
	helpEntry("export [--branch <branch>]", "Export snapshots to a .avc.tar.gz bundle")
	helpEntry("import --from <file>", "Import a bundle into the current project")
	fmt.Println()

	helpSection("MAINTENANCE")
	helpEntry("verify [--repair]", "Check stored history is intact")
	helpEntry("trash list|restore|empty", "Manage files quarantined by restore")
	helpEntry("gc [--run]", "Find (and optionally delete) orphaned blobs")
	helpEntry("storage", "Show disk usage breakdown for .avc/")
	helpEntry("cache stats|clear", "Manage the diff cache")
	fmt.Println()

	fmt.Printf("  %s\n", ruler(48))
	fmt.Printf("  %s  %s\n", dim("--json"), dim("Machine-readable output on every command"))
	fmt.Printf("  %s\n\n", dim("Run 'avc <command> --help' for details on any command."))
}

func helpSection(title string) {
	fmt.Printf("  %s\n", accent(title))
}

func helpEntry(use, desc string) {
	fmt.Printf("    %s %s\n", cyan(fmt.Sprintf("%-36s", use)), dim(desc))
}

// printSubcmdHelp renders the help for a single subcommand.
func printSubcmdHelp(cmd *cobra.Command) {
	fmt.Println()

	// Description — prefer Long, fall back to Short.
	desc := cmd.Long
	if desc == "" {
		desc = cmd.Short
	}
	for _, line := range strings.Split(strings.TrimSpace(desc), "\n") {
		fmt.Printf("  %s\n", line)
	}
	fmt.Println()

	// Usage line — append <command> for group commands that have no RunE.
	useLine := cmd.UseLine()
	if cmd.HasAvailableSubCommands() {
		useLine = cmd.CommandPath() + " <command> [flags]"
	}
	fmt.Printf("  %s\n", accent("USAGE"))
	fmt.Printf("    %s\n\n", cyan(useLine))

	// Subcommands (e.g. `avc branch --help`).
	if cmd.HasAvailableSubCommands() {
		fmt.Printf("  %s\n", accent("COMMANDS"))
		for _, sub := range cmd.Commands() {
			if sub.IsAvailableCommand() {
				fmt.Printf("    %-22s %s\n", cyan(sub.Name()), sub.Short)
			}
		}
		fmt.Println()
	}

	// Local flags — filter out -h/--help (self-evident) and --json (shown in footer).
	if lf := cmd.LocalFlags(); lf.HasAvailableFlags() {
		raw := strings.TrimRight(lf.FlagUsages(), "\n")
		var lines []string
		for _, line := range strings.Split(raw, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			if strings.Contains(line, "--help") {
				continue
			}
			lines = append(lines, line)
		}
		if len(lines) > 0 {
			fmt.Printf("  %s\n", accent("FLAGS"))
			for _, line := range lines {
				fmt.Printf("  %s\n", dim(line))
			}
			fmt.Println()
		}
	}

	// Footer.
	fmt.Printf("  %s  %s\n", dim("--json"), dim("Output as JSON for agent consumption"))
	fmt.Printf("  %s\n\n", dim("Run 'avc --help' to see all commands."))
}
