package gmail

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// OpenUnsubscribeURL parses the List-Unsubscribe header and opens the first
// HTTP(S) URL in the user's default browser. Returns an error if no HTTP URL
// is found (e.g. mailto-only headers require manual action).
func OpenUnsubscribeURL(rawHeader string) error {
	url := extractHTTPUnsubscribeURL(rawHeader)
	if url == "" {
		return fmt.Errorf("no HTTP unsubscribe URL found (header may contain only mailto links)")
	}
	return OpenBrowser(url)
}

func OpenBrowser(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		cmd = "xdg-open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default:
		return fmt.Errorf("unsupported platform %s", runtime.GOOS)
	}

	// Validate URL scheme to prevent command injection
	lower := strings.ToLower(url)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return fmt.Errorf("refusing to open non-HTTP URL: %s", url)
	}

	return exec.Command(cmd, args...).Start()
}
