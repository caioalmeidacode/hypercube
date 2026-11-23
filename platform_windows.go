//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"
)

const createNoWindow = 0x08000000

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
}

func openPath(p string) {
	cmd := exec.Command("explorer", p)
	cmd.Start()
}

func revealFile(p string) {
	cmd := exec.Command("explorer")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer /select,"` + p + `"`}
	cmd.Start()
}

func openURL(u string) {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	cmd.Start()
}

func findAppBrowser() string {
	var cands []string
	for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles", "LocalAppData"} {
		if base := os.Getenv(env); base != "" {
			cands = append(cands,
				filepath.Join(base, "Microsoft", "Edge", "Application", "msedge.exe"))
		}
	}
	for _, env := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
		if base := os.Getenv(env); base != "" {
			cands = append(cands,
				filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join(base, "BraveSoftware", "Brave-Browser", "Application", "brave.exe"))
		}
	}
	for _, c := range cands {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func openWindow(root, url string, closed chan struct{}) {
	browser := findAppBrowser()
	if browser == "" {
		openURL(url)
		return
	}
	profile := filepath.Join(root, "webview")
	cmd := exec.Command(browser,
		"--app="+url,
		"--user-data-dir="+profile,
		"--window-size=1320,860",
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-features=Translate,msEdgeSidebarV2,msHubApps,msUndersideButton",
		"--disable-sync",
		"--disable-extensions",
	)
	if err := cmd.Start(); err != nil {
		openURL(url)
		return
	}
	go func() {
		cmd.Wait()
		if closed != nil {
			closed <- struct{}{}
		}
	}()
}

func fatal(msg string) {
	mb := user32.NewProc("MessageBoxW")
	t, _ := syscall.UTF16PtrFromString(msg)
	c, _ := syscall.UTF16PtrFromString("Hypercube")
	mb.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), 0x10)
	os.Exit(1)
}
