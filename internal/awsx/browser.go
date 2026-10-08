package awsx

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// isWSL reports whether we are running under Windows Subsystem for Linux, where
// there is usually no default Linux browser and auto-open would fail. Detected via WSL_DISTRO_NAME or /proc/version.
func isWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	if b, err := os.ReadFile("/proc/version"); err == nil {
		return strings.Contains(strings.ToLower(string(b)), "microsoft")
	}
	return false
}

// openBrowser makes a best-effort attempt to open url in the user's browser.
// It returns true on success. On WSL it prefers wslview; the caller is expected
// to always ALSO print the URL+code so the "no browser" path degrades cleanly.
func openBrowser(url string) bool {
	var candidates [][]string

	if b := os.Getenv("BROWSER"); b != "" {
		candidates = append(candidates, []string{b, url})
	}

	switch {
	case runtime.GOOS == "darwin":
		candidates = append(candidates, []string{"open", url})
	case runtime.GOOS == "windows":
		candidates = append(candidates, []string{"rundll32", "url.dll,FileProtocolHandler", url})
	case isWSL():
		candidates = append(candidates,
			[]string{"wslview", url},
			[]string{"powershell.exe", "-NoProfile", "Start-Process", url},
		)
	default:
		candidates = append(candidates, []string{"xdg-open", url})
	}

	for _, c := range candidates {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		if err := exec.Command(c[0], c[1:]...).Start(); err == nil {
			return true
		}
	}
	return false
}
