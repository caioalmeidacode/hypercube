//go:build windows

package main

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	user32               = syscall.NewLazyDLL("user32.dll")
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procEnumWindows      = user32.NewProc("EnumWindows")
	procGetWindowTextW   = user32.NewProc("GetWindowTextW")
	procGetClassNameW    = user32.NewProc("GetClassNameW")
	procSendMessageW     = user32.NewProc("SendMessageW")
	procLoadImageW       = user32.NewProc("LoadImageW")
	procGetSystemMetrics = user32.NewProc("GetSystemMetrics")
	procIsWindowVisible  = user32.NewProc("IsWindowVisible")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

const (
	wmSetIcon  = 0x0080
	wmGetIcon  = 0x007F
	iconSmall  = 0
	iconBig    = 1
	imageIcon  = 1
	lrShared   = 0x8000
	smCxIcon   = 11
	smCyIcon   = 12
	smCxSmIcon = 49
	smCySmIcon = 50
)

func loadAppIcon(w, h uintptr) uintptr {
	hInst, _, _ := procGetModuleHandleW.Call(0)
	ic, _, _ := procLoadImageW.Call(hInst, 1, imageIcon, w, h, lrShared)
	return ic
}

func windowText(proc *syscall.LazyProc, hwnd uintptr) string {
	buf := make([]uint16, 256)
	n, _, _ := proc.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 256)
	return syscall.UTF16ToString(buf[:n])
}

func fixWindowIcons(stop <-chan struct{}) {
	cx, _, _ := procGetSystemMetrics.Call(smCxIcon)
	cy, _, _ := procGetSystemMetrics.Call(smCyIcon)
	sx, _, _ := procGetSystemMetrics.Call(smCxSmIcon)
	sy, _, _ := procGetSystemMetrics.Call(smCySmIcon)
	big := loadAppIcon(cx, cy)
	small := loadAppIcon(sx, sy)
	if big == 0 {
		return
	}
	cb := syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		if v, _, _ := procIsWindowVisible.Call(hwnd); v == 0 {
			return 1
		}
		if windowText(procGetClassNameW, hwnd) != "Chrome_WidgetWin_1" || windowText(procGetWindowTextW, hwnd) != "Hypercube" {
			return 1
		}
		cur, _, _ := procSendMessageW.Call(hwnd, wmGetIcon, iconBig, 0)
		if cur != big {
			procSendMessageW.Call(hwnd, wmSetIcon, iconBig, big)
			procSendMessageW.Call(hwnd, wmSetIcon, iconSmall, small)
		}
		return 1
	})
	start := time.Now()
	for {
		procEnumWindows.Call(cb, 0)
		wait := 400 * time.Millisecond
		if time.Since(start) > 20*time.Second {
			wait = 2 * time.Second
		}
		select {
		case <-stop:
			return
		case <-time.After(wait):
		}
	}
}

func startIconFixer() { go fixWindowIcons(nil) }
