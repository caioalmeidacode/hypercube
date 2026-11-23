package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func writeJSONResp(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	code := 400
	if err == errNotFound {
		code = 404
	}
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(v)
}

var safeName = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

func registerAPI(mux *http.ServeMux, token string) {

	guard := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			t := r.Header.Get("X-Hypercube")
			if t == "" {
				t = r.URL.Query().Get("t")
			}
			if t != token {
				http.Error(w, "forbidden", 403)
				return
			}
			h(w, r)
		}
	}
	handle := func(pattern string, h http.HandlerFunc) { mux.HandleFunc(pattern, guard(h)) }

	mux.HandleFunc("GET /api/ping", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("hypercube")) })

	handle("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		fl, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "stream unsupported", 500)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		ch := hub.Subscribe()
		defer hub.Unsubscribe(ch)
		fmt.Fprintf(w, "retry: 1500\n\n")
		fl.Flush()
		tick := time.NewTicker(15 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case b := <-ch:
				w.Write([]byte("data: "))
				w.Write(b)
				w.Write([]byte("\n\n"))
				fl.Flush()
			case <-tick.C:
				w.Write([]byte(": ping\n\n"))
				fl.Flush()
			}
		}
	})

	handle("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		running := map[string]bool{}
		for _, in := range store.List() {
			if isRunning(in.ID) {
				running[in.ID] = true
			}
		}
		ps := publicSettings()
		writeJSONResp(w, map[string]any{
			"instances": store.List(), "running": running, "tasks": TaskSnapshot(),
			"settings": ps["settings"], "profile": ps["profile"], "mcAccount": ps["mcAccount"],
			"version": appVersion, "dataDir": store.root,
		})
	})

	handle("GET /api/versions", func(w http.ResponseWriter, r *http.Request) {
		m, err := getManifest()
		if err != nil {
			writeErr(w, fmt.Errorf("sem conexão com a Mojang: %w", err))
			return
		}
		writeJSONResp(w, m)
	})

	handle("GET /api/loaders/{loader}", func(w http.ResponseWriter, r *http.Request) {
		lv, err := loaderVersions(r.PathValue("loader"), r.URL.Query().Get("mc"))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSONResp(w, lv)
	})

	handle("POST /api/instances", func(w http.ResponseWriter, r *http.Request) {
		var in Instance
		if err := decode(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		in.ID = newID()
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" {
			in.Name = "Minecraft " + in.MCVersion
		}
		if in.MCVersion == "" {
			writeErr(w, fmt.Errorf("escolha uma versão"))
			return
		}
		if in.Loader == "" {
			in.Loader = "vanilla"
		}
		in.Installed, in.VersionID, in.Mods, in.Modlists, in.Categories = false, "", nil, nil, nil
		if err := store.Add(&in); err != nil {
			writeErr(w, err)
			return
		}
		go installInstance(in.ID)
		emitInstance(in.ID)
		writeJSONResp(w, in)
	})

	handle("GET /api/instances/{id}", func(w http.ResponseWriter, r *http.Request) {
		in, ok := store.Get(r.PathValue("id"))
		if !ok {
			writeErr(w, errNotFound)
			return
		}
		in.Mods = nil
		writeJSONResp(w, map[string]any{"instance": in, "running": isRunning(in.ID)})
	})

	handle("PATCH /api/instances/{id}", func(w http.ResponseWriter, r *http.Request) {
		var p map[string]json.RawMessage
		if err := decode(r, &p); err != nil {
			writeErr(w, err)
			return
		}
		id := r.PathValue("id")
		reinstall := false
		err := store.Update(id, func(in *Instance) error {
			str := func(k string, dst *string) {
				if v, ok := p[k]; ok {
					json.Unmarshal(v, dst)
				}
			}
			str("name", &in.Name)
			str("icon", &in.Icon)
			str("jvmArgs", &in.JVMArgs)
			str("javaPath", &in.JavaPath)
			if v, ok := p["memoryMB"]; ok {
				json.Unmarshal(v, &in.MemoryMB)
			}
			oldMC, oldL, oldLV := in.MCVersion, in.Loader, in.LoaderVersion
			str("mcVersion", &in.MCVersion)
			str("loader", &in.Loader)
			str("loaderVersion", &in.LoaderVersion)
			if oldMC != in.MCVersion || oldL != in.Loader || oldLV != in.LoaderVersion {
				in.Installed = false
				in.VersionID = ""
				reinstall = true
			}
			return nil
		})
		if err != nil {
			writeErr(w, err)
			return
		}
		if reinstall {
			go installInstance(id)
		}
		emitInstance(id)
		writeJSONResp(w, map[string]bool{"ok": true})
	})

	handle("DELETE /api/instances/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if isRunning(id) {
			writeErr(w, fmt.Errorf("feche o jogo primeiro"))
			return
		}
		if err := store.Remove(id); err != nil {
			writeErr(w, err)
			return
		}
		hub.Emit("removed", id)
		writeJSONResp(w, map[string]bool{"ok": true})
	})

	handle("POST /api/instances/{id}/duplicate", func(w http.ResponseWriter, r *http.Request) {
		src, ok := store.Get(r.PathValue("id"))
		if !ok {
			writeErr(w, errNotFound)
			return
		}
		cp := src
		cp.ID = newID()
		cp.Name = src.Name + " (cópia)"
		cp.Created = time.Now().Unix()
		cp.LastPlayed, cp.Playtime = 0, 0
		if err := copyDir(store.GameDir(src.ID), store.GameDir(cp.ID)); err != nil {
			writeErr(w, err)
			return
		}
		if p := instanceIconFile(src.ID); p != "" {
			copyFile(p, filepath.Join(store.InstanceDir(cp.ID), filepath.Base(p)))
		}
		if err := store.Add(&cp); err != nil {
			writeErr(w, err)
			return
		}
		emitInstance(cp.ID)
		writeJSONResp(w, cp)
	})

	handle("POST /api/instances/{id}/install", func(w http.ResponseWriter, r *http.Request) {
		go installInstance(r.PathValue("id"))
		writeJSONResp(w, map[string]bool{"ok": true})
	})

	handle("POST /api/instances/{id}/launch", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if isRunning(id) {
			writeErr(w, fmt.Errorf("já está em execução"))
			return
		}
		if store.Settings().MCAccount == nil {
			writeErr(w, errNeedAccount)
			return
		}
		go func() {
			if err := launchInstance(id); err != nil {
				appendLog(id, "[Hypercube] ERRO: "+err.Error())
				hub.Emit("toast", map[string]string{"kind": "error", "text": err.Error()})
			}
		}()
		writeJSONResp(w, map[string]bool{"ok": true})
	})

	handle("POST /api/instances/{id}/kill", func(w http.ResponseWriter, r *http.Request) {
		if err := killInstance(r.PathValue("id")); err != nil {
			writeErr(w, err)
			return
		}
		writeJSONResp(w, map[string]bool{"ok": true})
	})

	handle("GET /api/instances/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		writeJSONResp(w, getLogs(r.PathValue("id")))
	})

	handle("POST /api/instances/{id}/open", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, ok := store.Get(id); !ok {
			writeErr(w, errNotFound)
			return
		}
		dir := store.GameDir(id)
		switch sub := r.URL.Query().Get("sub"); sub {
		case "mods", "config", "saves", "resourcepacks", "shaderpacks", "logs", "screenshots", "crash-reports":
			dir = filepath.Join(dir, sub)
		}
		os.MkdirAll(dir, 0o755)
		openPath(dir)
		writeJSONResp(w, map[string]bool{"ok": true})
	})

	handle("POST /api/instances/{id}/icon", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if _, ok := store.Get(id); !ok {
			writeErr(w, errNotFound)
			return
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, 25<<20))
		if err != nil || len(data) == 0 {
			writeErr(w, fmt.Errorf("imagem inválida"))
			return
		}
		ext := imageExt(data)
		if ext == "" {
			writeErr(w, fmt.Errorf("use uma imagem PNG, JPG, WEBP ou GIF"))
			return
		}
		for _, old := range []string{".png", ".jpg", ".gif", ".webp"} {
			os.Remove(filepath.Join(store.InstanceDir(id), "icon"+old))
		}
		if err := os.WriteFile(filepath.Join(store.InstanceDir(id), "icon"+ext), data, 0o644); err != nil {
			writeErr(w, err)
			return
		}
		store.Update(id, func(in *Instance) error {
			in.Icon = "custom:" + strconv.FormatInt(time.Now().UnixMilli(), 10)
			return nil
		})
		emitInstance(id)
		writeJSONResp(w, map[string]bool{"ok": true})
	})

	mux.HandleFunc("GET /api/instances/{id}/icon", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !safeName.MatchString(id) {
			http.NotFound(w, r)
			return
		}
		if p := instanceIconFile(id); p != "" {
			w.Header().Set("Cache-Control", "public, max-age=31536000")
			http.ServeFile(w, r, p)
			return
		}
		http.NotFound(w, r)
	})

	handle("GET /api/instances/{id}/mods", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		mods, err := scanMods(id)
		if err != nil {
			writeErr(w, err)
			return
		}
		in, _ := store.Get(id)
		writeJSONResp(w, map[string]any{
			"mods": mods, "categories": in.Categories, "modlists": in.Modlists, "activeModlist": in.ActiveModlist,
		})
	})

	handle("POST /api/instances/{id}/mods/toggle", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Keys    []string `json:"keys"`
			Enabled bool     `json:"enabled"`
		}
		if err := decode(r, &b); err != nil {
			writeErr(w, err)
			return
		}
		if err := toggleMods(r.PathValue("id"), b.Keys, b.Enabled); err != nil {
			writeErr(w, err)
			return
		}
		writeJSONResp(w, map[string]bool{"ok": true})
	})

	handle("POST /api/instances/{id}/mods/delete", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Keys []string `json:"keys"`
		}
		if err := decode(r, &b); err != nil {
			writeErr(w, err)
			return
		}
		if err := deleteMods(r.PathValue("id"), b.Keys); err != nil {
			writeErr(w, err)
			return
		}
		writeJSONResp(w, map[string]bool{"ok": true})
	})

	handle("POST /api/instances/{id}/mods/category", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Keys     []string `json:"keys"`
			Category string   `json:"category"`
		}
		if err := decode(r, &b); err != nil {
			writeErr(w, err)
			return
		}
		if err := setCategory(r.PathValue("id"), b.Keys, b.Category); err != nil {
			writeErr(w, err)
			return
		}
		writeJSONResp(w, map[string]bool{"ok": true})
	})

	handle("POST /api/instances/{id}/categories", func(w http.ResponseWriter, r *http.Request) {
		var op CategoryOp
		if err := decode(r, &op); err != nil {
			writeErr(w, err)
			return
		}
		cid, err := categoryOp(r.PathValue("id"), op)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSONResp(w, map[string]string{"id": cid})
	})

	handle("POST /api/instances/{id}/modlists", func(w http.ResponseWriter, r *http.Request) {
		var op ModlistOp
		if err := decode(r, &op); err != nil {
			writeErr(w, err)
			return
		}
		if err := modlistOp(r.PathValue("id"), op); err != nil {
			writeErr(w, err)
			return
		}
		writeJSONResp(w, map[string]bool{"ok": true})
	})

	handle("POST /api/instances/{id}/mods/copy", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Keys   []string `json:"keys"`
			Target string   `json:"target"`
		}
		if err := decode(r, &b); err != nil {
			writeErr(w, err)
			return
		}
		n, err := copyMods(r.PathValue("id"), b.Target, b.Keys)
		if err != nil {
			writeErr(w, err)
			return
		}
		hub.Emit("mods", b.Target)
		writeJSONResp(w, map[string]int{"copied": n})
	})

	handle("POST /api/instances/{id}/mods/import", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := r.ParseMultipartForm(256 << 20); err != nil {
			writeErr(w, err)
			return
		}
		n := 0
		for _, fhs := range r.MultipartForm.File {
			for _, fh := range fhs {
				f, err := fh.Open()
				if err != nil {
					continue
				}
				if err := importModFile(id, fh.Filename, f); err != nil {
					f.Close()
					writeErr(w, err)
					return
				}
				f.Close()
				n++
			}
		}
		writeJSONResp(w, map[string]int{"imported": n})
	})

	handle("POST /api/instances/{id}/mods/install", func(w http.ResponseWriter, r *http.Request) {
		var req InstallReq
		if err := decode(r, &req); err != nil {
			writeErr(w, err)
			return
		}
		if err := installRemote(r.PathValue("id"), req); err != nil {
			writeErr(w, err)
			return
		}
		writeJSONResp(w, map[string]bool{"ok": true})
	})

	handle("GET /api/instances/{id}/mods/updates", func(w http.ResponseWriter, r *http.Request) {
		u, err := checkUpdates(r.PathValue("id"))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSONResp(w, u)
	})

	handle("POST /api/instances/{id}/mods/update", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Key       string `json:"key"`
			VersionID string `json:"versionId"`
		}
		if err := decode(r, &b); err != nil {
			writeErr(w, err)
			return
		}
		if err := updateMod(r.PathValue("id"), b.Key, b.VersionID); err != nil {
			writeErr(w, err)
			return
		}
		writeJSONResp(w, map[string]bool{"ok": true})
	})

	handle("GET /api/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		off, _ := strconv.Atoi(q.Get("offset"))
		res, err := searchMods(q.Get("source"), q.Get("q"), q.Get("mc"), q.Get("loader"), q.Get("sort"), off, q.Get("type"))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSONResp(w, res)
	})

	mux.HandleFunc("GET /api/icon/{file}", func(w http.ResponseWriter, r *http.Request) {
		f := r.PathValue("file")
		if !safeName.MatchString(f) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeFile(w, r, store.CacheDir("icons", f))
	})

	handle("POST /api/settings", func(w http.ResponseWriter, r *http.Request) {
		var in Settings
		if err := decode(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		store.UpdateSettings(func(st *Settings) {
			prof, acc := st.Profile, st.MCAccount
			*st = in
			st.Profile, st.MCAccount = prof, acc
			if st.Concurrency <= 0 || st.Concurrency > 64 {
				st.Concurrency = 12
			}
			if st.MemoryMB < 512 {
				st.MemoryMB = 512
			}
		})
		writeJSONResp(w, publicSettings())
	})

	registerProfileAPI(handle, mux)
	registerMCAuthAPI(handle)

	handle("POST /api/open-url", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			URL string `json:"url"`
		}
		decode(r, &b)
		if strings.HasPrefix(b.URL, "https://") || strings.HasPrefix(b.URL, "http://") {
			openURL(b.URL)
		}
		writeJSONResp(w, map[string]bool{"ok": true})
	})

	handle("POST /api/open-data", func(w http.ResponseWriter, r *http.Request) {
		openPath(store.root)
		writeJSONResp(w, map[string]bool{"ok": true})
	})
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(p, target)
	})
}

func imageExt(data []byte) string {
	switch http.DetectContentType(data) {
	case "image/gif":
		return ".gif"
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/webp":
		return ".webp"
	}
	return ""
}

func instanceIconFile(id string) string {
	for _, ext := range []string{".gif", ".png", ".jpg", ".webp"} {
		p := filepath.Join(store.InstanceDir(id), "icon"+ext)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}
