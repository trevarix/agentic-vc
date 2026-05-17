package avc

import (
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/SkillMythOrg/agentic-vc/avc/internal/web"
	"github.com/spf13/cobra"
)

var (
	uiPort   int
	uiNoOpen bool
	uiHost   string
)

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Start the AVC web UI server (default port 3004)",
	Long: `Starts a local web server that provides a graphical interface for managing
AVC snapshots. Useful for users who don't run VSCode.

The server binds to 127.0.0.1 by default and opens your default browser.
Press Ctrl+C to stop the server.`,
	RunE: runUi,
}

func init() {
	uiCmd.Flags().IntVar(&uiPort, "port", 3004, "Port to listen on")
	uiCmd.Flags().BoolVar(&uiNoOpen, "no-open", false, "Don't open browser automatically")
	uiCmd.Flags().StringVar(&uiHost, "host", "127.0.0.1", "Bind host (default: localhost only)")
	rootCmd.AddCommand(uiCmd)
}

func runUi(cmd *cobra.Command, args []string) error {
	projectPath, err := requireInitializedProject()
	if err != nil {
		return err
	}

	addr := fmt.Sprintf("%s:%d", uiHost, uiPort)
	url := fmt.Sprintf("http://%s/", addr)

	fmt.Printf("AVC UI starting at %s\n", url)
	fmt.Println("Project:", projectPath)
	fmt.Println("Press Ctrl+C to stop.")

	if !uiNoOpen {
		// Brief delay so the server is ready before the browser request.
		go func() {
			time.Sleep(500 * time.Millisecond)
			openBrowser(url)
		}()
	}

	return web.Serve(addr, projectPath)
}

// openBrowser opens the default browser to the given URL on the current OS.
// Failures are silent — the user already sees the URL in the console.
func openBrowser(url string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	_ = exec.Command(cmd, args...).Start()
}
