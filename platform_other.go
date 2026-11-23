//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func hideWindow(cmd *exec.Cmd) {}

func opener() string {
	if runtime.GOOS == "darwin" {
		return "open"
	}
	return "xdg-open"
}

func openPath(p string)   { exec.Command(opener(), p).Start() }
func openURL(u string)    { exec.Command(opener(), u).Start() }
func revealFile(p string) { openPath(filepath.Dir(p)) }

func openWindow(root, url string, closed chan struct{}) {
	for _, b := range []string{"microsoft-edge", "google-chrome", "chromium", "chromium-browser", "brave-browser"} {
		if p, err := exec.LookPath(b); err == nil {
			cmd := exec.Command(p, "--app="+url, "--user-data-dir="+filepath.Join(root, "webview"),
				"--window-size=1320,860", "--no-first-run", "--no-default-browser-check")
			if cmd.Start() == nil {
				go func() {
					cmd.Wait()
					if closed != nil {
						closed <- struct{}{}
					}
				}()
				return
			}
		}
	}
	openURL(url)
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}

func startIconFixer() {}
