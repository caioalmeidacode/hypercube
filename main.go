package main

import (
	"crypto/rand"
	"embed"
	"encoding/hex"
	"flag"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const appVersion = "1.0.0-alpha.1"

//go:embed web
var webFS embed.FS

func dataRoot() string {
	if d := os.Getenv("HYPERCUBE_DATA"); d != "" {
		return d
	}
	if runtime.GOOS == "windows" {
		if ad := os.Getenv("APPDATA"); ad != "" {
			return filepath.Join(ad, "Hypercube")
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".hypercube")
	}
	return "hypercube-data"
}

type session struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

func main() {
	noWindow := flag.Bool("no-window", false, "não abrir janela (apenas servidor)")
	dataFlag := flag.String("data", "", "pasta de dados")
	portFlag := flag.Int("port", 0, "porta fixa")
	flag.Parse()

	root := dataRoot()
	if *dataFlag != "" {
		root = *dataFlag
	}
	os.MkdirAll(root, 0o755)

	var prev session
	if readJSON(filepath.Join(root, "session.json"), &prev) == nil && prev.Port > 0 {
		c := http.Client{Timeout: 800 * time.Millisecond}
		if resp, err := c.Get(fmt.Sprintf("http://127.0.0.1:%d/api/ping", prev.Port)); err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				if !*noWindow {
					openWindow(root, fmt.Sprintf("http://127.0.0.1:%d/?t=%s", prev.Port, prev.Token), nil)
				}
				return
			}
		}
	}

	var err error
	store, err = openStore(root)
	if err != nil {
		fatal("Não foi possível abrir a pasta de dados:\n" + err.Error())
	}

	tb := make([]byte, 16)
	rand.Read(tb)
	token := hex.EncodeToString(tb)

	mux := http.NewServeMux()
	registerAPI(mux, token)
	sub, _ := fs.Sub(webFS, "web")
	files := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".woff2") {
			w.Header().Set("Cache-Control", "public, max-age=31536000")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})

	ln, err := listen(*portFlag)
	if err != nil {
		fatal("Não foi possível iniciar o servidor local:\n" + err.Error())
	}
	port := ln.Addr().(*net.TCPAddr).Port
	writeJSON(filepath.Join(root, "session.json"), session{Port: port, Token: token})

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)

	url := fmt.Sprintf("http://127.0.0.1:%d/?t=%s", port, token)
	if *noWindow {
		fmt.Println("Hypercube rodando em", url)
		select {}
	}

	windowClosed := make(chan struct{}, 1)
	openWindow(root, url, windowClosed)
	startIconFixer()

	start := time.Now()
	for {
		time.Sleep(2 * time.Second)
		hub.mu.Lock()
		ever, idle := hub.ever, time.Since(hub.lastSeen)
		hub.mu.Unlock()
		busy := RunningGames() > 0 || RunningTasks() > 0
		if hub.Count() > 0 || busy {
			continue
		}
		if !ever && time.Since(start) < 90*time.Second {
			continue
		}
		if ever && idle < 6*time.Second {
			continue
		}
		break
	}
	os.Remove(filepath.Join(root, "session.json"))
}

func listen(port int) (net.Listener, error) {
	if port > 0 {
		return net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	}
	for _, p := range []int{47615, 47616, 47617} {
		if ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p)); err == nil {
			return ln, nil
		}
	}
	return net.Listen("tcp", "127.0.0.1:0")
}
