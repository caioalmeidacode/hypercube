package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var validName = regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`)

func publicSettings() map[string]any {
	st := store.Settings()
	p := st.Profile
	prof := map[string]any{"name": p.Name, "uuid": p.UUID, "created": p.Created, "avatar": ""}
	if p.Avatar != "" {
		prof["avatar"] = fmt.Sprintf("/api/profile/avatar?v=%d", p.Updated)
	}
	var mc any
	if a := st.MCAccount; a != nil {
		mc = map[string]string{"name": a.Name, "uuid": a.UUID}
	}
	st.MCAccount = nil
	return map[string]any{"settings": st, "profile": prof, "mcAccount": mc}
}

func downloadsDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		d := filepath.Join(home, "Downloads")
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return d
		}
		return home
	}
	return store.root
}

func registerProfileAPI(handle func(string, http.HandlerFunc), mux *http.ServeMux) {
	handle("POST /api/profile", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Name string `json:"name"`
		}
		decode(r, &b)
		name := strings.TrimSpace(b.Name)
		if !validName.MatchString(name) {
			writeErr(w, fmt.Errorf("o nome deve ter de 3 a 16 caracteres: letras, números ou _"))
			return
		}
		store.UpdateSettings(func(st *Settings) {
			if st.Profile.Created == 0 {
				st.Profile.Created = time.Now().Unix()
			}
			st.Profile.Name = name
			st.Profile.UUID = offlineUUID(name)
			st.Profile.Updated = time.Now().UnixMilli()
		})
		ps := publicSettings()
		hub.Emit("profile", ps["profile"])
		writeJSONResp(w, ps)
	})

	handle("POST /api/profile/avatar", func(w http.ResponseWriter, r *http.Request) {
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
		name := "avatar" + ext
		old := store.Settings().Profile.Avatar
		if old != "" && old != name {
			os.Remove(filepath.Join(store.root, old))
		}
		if err := os.WriteFile(filepath.Join(store.root, name), data, 0o644); err != nil {
			writeErr(w, err)
			return
		}
		store.UpdateSettings(func(st *Settings) {
			st.Profile.Avatar = name
			st.Profile.Updated = time.Now().UnixMilli()
		})
		ps := publicSettings()
		hub.Emit("profile", ps["profile"])
		writeJSONResp(w, ps)
	})

	handle("DELETE /api/profile/avatar", func(w http.ResponseWriter, r *http.Request) {
		if a := store.Settings().Profile.Avatar; a != "" {
			os.Remove(filepath.Join(store.root, a))
		}
		store.UpdateSettings(func(st *Settings) { st.Profile.Avatar = ""; st.Profile.Updated = time.Now().UnixMilli() })
		ps := publicSettings()
		hub.Emit("profile", ps["profile"])
		writeJSONResp(w, ps)
	})

	mux.HandleFunc("GET /api/profile/avatar", func(w http.ResponseWriter, r *http.Request) {
		a := store.Settings().Profile.Avatar
		if a == "" || !safeName.MatchString(a) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000")
		http.ServeFile(w, r, filepath.Join(store.root, a))
	})

	handle("POST /api/profile/export", func(w http.ResponseWriter, r *http.Request) {
		worlds := r.URL.Query().Get("worlds") != "0"
		p, _ := store.Profile()
		name := p.Name
		if name == "" {
			name = "perfil"
		}
		dst := filepath.Join(downloadsDir(), fmt.Sprintf("Hypercube-%s-%s.hcbackup", name, time.Now().Format("2006-01-02-1504")))
		go func() {
			t := NewTask("Exportando perfil", "")
			err := exportProfile(t, dst, worlds)
			t.Finish(err)
			if err == nil {
				hub.Emit("toast", map[string]string{"kind": "ok", "text": "Backup salvo em " + dst})
				revealFile(dst)
			}
		}()
		writeJSONResp(w, map[string]string{"path": dst})
	})

	handle("POST /api/profile/import", func(w http.ResponseWriter, r *http.Request) {
		tmp, err := os.CreateTemp(store.CacheDir(), "import-*.zip")
		if err != nil {
			writeErr(w, err)
			return
		}
		_, err = io.Copy(tmp, r.Body)
		tmp.Close()
		if err != nil {
			os.Remove(tmp.Name())
			writeErr(w, err)
			return
		}
		useProfile := r.URL.Query().Get("profile") == "1"
		n, err := importProfile(tmp.Name(), useProfile)
		os.Remove(tmp.Name())
		if err != nil {
			writeErr(w, err)
			return
		}
		ps := publicSettings()
		hub.Emit("profile", ps["profile"])
		writeJSONResp(w, map[string]any{"instances": n, "profile": ps["profile"], "settings": ps["settings"]})
	})
}

const backupManifest = "hypercube-backup.json"

func exportProfile(t *Task, dst string, worlds bool) error {
	root := store.root
	var files []string
	var total int64
	skip := func(rel string) bool {
		parts := strings.Split(filepath.ToSlash(rel), "/")

		if len(parts) >= 3 && (parts[2] == "natives" || parts[2] == "launch.args") {
			return true
		}
		if len(parts) >= 4 && parts[2] == "minecraft" {
			switch parts[3] {
			case "logs", "crash-reports", ".cache", "webcache2":
				return true
			case "saves":
				return !worlds
			}
		}
		return false
	}
	filepath.Walk(filepath.Join(root, "instances"), func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		if skip(rel) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.IsDir() {
			files = append(files, rel)
			total += info.Size()
		}
		return nil
	})
	st := store.Settings()
	st.MCAccount = nil
	if st.Profile.Avatar != "" {
		files = append(files, st.Profile.Avatar)
	}
	t.Stage(fmt.Sprintf("%d arquivos", len(files)), total)

	tmp := dst + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	man, _ := json.MarshalIndent(map[string]any{
		"app": "Hypercube", "version": appVersion, "exported": time.Now().Format(time.RFC3339),
		"settings": st, "instances": len(store.List()),
	}, "", "  ")
	mw, _ := zw.Create(backupManifest)
	mw.Write(man)
	for _, rel := range files {
		src, err := os.Open(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		info, _ := src.Stat()
		hdr, _ := zip.FileInfoHeader(info)
		hdr.Name = filepath.ToSlash(rel)
		low := strings.ToLower(rel)
		if strings.HasSuffix(low, ".jar") || strings.HasSuffix(low, ".zip") || strings.HasSuffix(low, ".png") || strings.HasSuffix(low, ".gif") || strings.HasSuffix(low, ".disabled") {
			hdr.Method = zip.Store
		} else {
			hdr.Method = zip.Deflate
		}
		zf, err := zw.CreateHeader(hdr)
		if err == nil {
			n, _ := io.Copy(zf, src)
			t.Add(n)
		}
		src.Close()
	}
	if err := zw.Close(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	return os.Rename(tmp, dst)
}

func importProfile(path string, useProfile bool) (int, error) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return 0, fmt.Errorf("arquivo de backup inválido")
	}
	defer zr.Close()
	var man struct {
		Settings Settings `json:"settings"`
	}
	found := false
	for _, f := range zr.File {
		if f.Name == backupManifest {
			rc, _ := f.Open()
			json.NewDecoder(rc).Decode(&man)
			rc.Close()
			found = true
		}
	}
	if !found {
		return 0, fmt.Errorf("este arquivo não é um backup do Hypercube")
	}

	idMap := map[string]string{}
	for _, f := range zr.File {
		parts := strings.Split(f.Name, "/")
		if len(parts) >= 3 && parts[0] == "instances" && safeName.MatchString(parts[1]) {
			if _, ok := idMap[parts[1]]; ok {
				continue
			}
			nid := parts[1]
			if _, err := os.Stat(store.InstanceDir(nid)); err == nil {
				nid = newID()
			}
			idMap[parts[1]] = nid
		}
	}
	root := store.root
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || f.Name == backupManifest {
			continue
		}
		name := filepath.ToSlash(filepath.Clean(f.Name))
		parts := strings.Split(name, "/")
		var target string
		switch {
		case len(parts) >= 3 && parts[0] == "instances":
			nid, ok := idMap[parts[1]]
			if !ok {
				continue
			}
			target = filepath.Join(append([]string{root, "instances", nid}, parts[2:]...)...)
		case len(parts) == 1 && strings.HasPrefix(parts[0], "avatar.") && useProfile:
			target = filepath.Join(root, parts[0])
		default:
			continue
		}
		if strings.Contains(name, "..") || !strings.HasPrefix(target, filepath.Clean(root)+string(os.PathSeparator)) {
			continue
		}
		os.MkdirAll(filepath.Dir(target), 0o755)
		rc, err := f.Open()
		if err != nil {
			continue
		}
		out, err := os.Create(target)
		if err == nil {
			io.Copy(out, rc)
			out.Close()
		}
		rc.Close()
	}

	for old, nid := range idMap {
		p := filepath.Join(store.InstanceDir(nid), "instance.json")
		var inst Instance
		if readJSON(p, &inst) == nil {
			inst.ID = nid
			if old != nid {
				inst.Name += " (importada)"
			}
			writeJSON(p, inst)
		}
	}
	store.reloadInstances()
	for _, nid := range idMap {
		emitInstance(nid)
	}
	if useProfile {
		store.UpdateSettings(func(st *Settings) {
			prof := man.Settings.Profile
			acc := st.MCAccount
			*st = man.Settings
			st.MCAccount = acc
			st.Legacy = nil
			if prof.Name != "" {
				prof.UUID = offlineUUID(prof.Name)
				prof.Updated = time.Now().UnixMilli()
			}
			st.Profile = prof
			if st.Concurrency <= 0 {
				st.Concurrency = 12
			}
			if st.MemoryMB < 512 {
				st.MemoryMB = 4096
			}
		})
	}
	return len(idMap), nil
}
