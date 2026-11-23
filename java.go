package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const javaRuntimeIndex = "https://piston-meta.mojang.com/v1/products/java-runtime/2ec0cc96c44e5a76b9c8b7c39df7210883d12871/all.json"

type runtimeEntry struct {
	Manifest struct {
		SHA1 string `json:"sha1"`
		Size int64  `json:"size"`
		URL  string `json:"url"`
	} `json:"manifest"`
	Version struct {
		Name string `json:"name"`
	} `json:"version"`
}

type runtimeManifest struct {
	Files map[string]struct {
		Type       string `json:"type"`
		Executable bool   `json:"executable"`
		Target     string `json:"target"`
		Downloads  struct {
			Raw Artifact `json:"raw"`
		} `json:"downloads"`
	} `json:"files"`
}

func javaPlatform() string {
	switch runtime.GOOS {
	case "windows":
		switch runtime.GOARCH {
		case "386":
			return "windows-x86"
		case "arm64":
			return "windows-arm64"
		}
		return "windows-x64"
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "mac-os-arm64"
		}
		return "mac-os"
	default:
		if runtime.GOARCH == "386" {
			return "linux-i386"
		}
		return "linux"
	}
}

func javaBinIn(home string, console bool) string {
	switch runtime.GOOS {
	case "windows":
		if console {
			return filepath.Join(home, "bin", "java.exe")
		}
		return filepath.Join(home, "bin", "javaw.exe")
	case "darwin":
		p := filepath.Join(home, "jre.bundle", "Contents", "Home", "bin", "java")
		if _, err := os.Stat(p); err == nil {
			return p
		}
		return filepath.Join(home, "bin", "java")
	default:
		return filepath.Join(home, "bin", "java")
	}
}

func consoleJava(p string) string {
	if strings.HasSuffix(strings.ToLower(p), "javaw.exe") {
		c := p[:len(p)-len("javaw.exe")] + "java.exe"
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return p
}

func ensureJava(t *Task, v *VersionJSON, override string) (string, error) {
	if override == "" {
		override = store.Settings().JavaPath
	}
	if override != "" {
		if _, err := os.Stat(override); err == nil {
			return override, nil
		}
		return "", fmt.Errorf("Java personalizado não encontrado: %s", override)
	}
	component, major := "jre-legacy", 8
	if v != nil && v.JavaVersion != nil {
		if v.JavaVersion.Component != "" {
			component = v.JavaVersion.Component
		}
		major = v.JavaVersion.MajorVersion
	}
	p, err := installRuntime(t, component)
	if err == nil {
		return p, nil
	}

	if sys := findSystemJava(); sys != "" {
		return sys, nil
	}
	return "", fmt.Errorf("não foi possível obter o Java %d (%s): %v", major, component, err)
}

func installRuntime(t *Task, component string) (string, error) {
	home := store.SharedDir("runtimes", component)
	marker := filepath.Join(home, ".hypercube-complete")
	if _, err := os.Stat(marker); err == nil {
		bin := javaBinIn(home, false)
		if _, err := os.Stat(bin); err == nil {
			return bin, nil
		}
	}
	var index map[string]map[string][]runtimeEntry
	if err := cachedJSON(javaRuntimeIndex, "java_runtimes.json", 24*time.Hour, &index); err != nil {
		return "", err
	}
	entries := index[javaPlatform()][component]
	if len(entries) == 0 {
		return "", fmt.Errorf("runtime %s indisponível para %s", component, javaPlatform())
	}
	e := entries[0]
	var man runtimeManifest
	if err := getJSON(e.Manifest.URL, &man); err != nil {
		return "", err
	}
	var dls []DL
	var execs []string
	for name, f := range man.Files {
		dst := filepath.Join(home, filepath.FromSlash(name))
		switch f.Type {
		case "directory":
			os.MkdirAll(dst, 0o755)
		case "file":
			dls = append(dls, DL{URL: f.Downloads.Raw.URL, Path: dst, SHA1: f.Downloads.Raw.SHA1, Size: f.Downloads.Raw.Size})
			if f.Executable {
				execs = append(execs, dst)
			}
		}
	}
	if t != nil {
		t.SetDetail("Java " + e.Version.Name)
	}
	if err := downloadAll(t, "Java "+e.Version.Name, dls); err != nil {
		return "", err
	}
	if runtime.GOOS != "windows" {
		for _, x := range execs {
			os.Chmod(x, 0o755)
		}
		for name, f := range man.Files {
			if f.Type == "link" {
				dst := filepath.Join(home, filepath.FromSlash(name))
				os.Remove(dst)
				os.Symlink(f.Target, dst)
			}
		}
	}
	os.WriteFile(marker, []byte(e.Version.Name), 0o644)
	return javaBinIn(home, false), nil
}

func findSystemJava() string {
	if jh := os.Getenv("JAVA_HOME"); jh != "" {
		p := javaBinIn(jh, false)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	name := "java"
	if runtime.GOOS == "windows" {
		name = "javaw"
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

func javaForMC(t *Task, mc string) (string, error) {
	v, err := loadVersion(mc)
	if err != nil {
		return "", err
	}
	j, err := ensureJava(t, v, "")
	if err != nil {
		return "", err
	}
	return consoleJava(j), nil
}
