package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type JarMeta struct {
	ModID       string   `json:"modId"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Authors     []string `json:"authors"`
	Icon        string   `json:"icon"`
	IconWide    bool     `json:"iconWide"`
	Loaders     []string `json:"loaders"`
	SHA1        string   `json:"sha1"`
}

type jarCacheEntry struct {
	size  int64
	mtime int64
	meta  JarMeta
}

var (
	jarCacheMu sync.Mutex
	jarCache   = map[string]jarCacheEntry{}
)

func readJar(path string) (JarMeta, error) {
	st, err := os.Stat(path)
	if err != nil {
		return JarMeta{}, err
	}
	jarCacheMu.Lock()
	if e, ok := jarCache[path]; ok && e.size == st.Size() && e.mtime == st.ModTime().UnixNano() {
		jarCacheMu.Unlock()
		return e.meta, nil
	}
	jarCacheMu.Unlock()
	m, err := parseJar(path)
	if err != nil {
		return m, err
	}
	jarCacheMu.Lock()
	jarCache[path] = jarCacheEntry{size: st.Size(), mtime: st.ModTime().UnixNano(), meta: m}
	jarCacheMu.Unlock()
	return m, nil
}

func parseJar(path string) (JarMeta, error) {
	var m JarMeta
	data, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	h := sha1.Sum(data)
	m.SHA1 = hex.EncodeToString(h[:])
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return m, nil
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	read := func(name string) []byte {
		name = strings.TrimPrefix(name, "/")
		f, ok := files[name]
		if !ok {
			return nil
		}
		rc, err := f.Open()
		if err != nil {
			return nil
		}
		defer rc.Close()
		b, _ := io.ReadAll(io.LimitReader(rc, 8<<20))
		return b
	}
	iconPath := ""

	if b := read("fabric.mod.json"); b != nil {
		var fm struct {
			ID          string            `json:"id"`
			Name        string            `json:"name"`
			Version     string            `json:"version"`
			Description string            `json:"description"`
			Authors     []json.RawMessage `json:"authors"`
			Icon        json.RawMessage   `json:"icon"`
		}
		if lenientJSON(b, &fm) == nil {
			m.ModID, m.Name, m.Version, m.Description = fm.ID, fm.Name, fm.Version, fm.Description
			m.Authors = parsePeople(fm.Authors)
			iconPath = pickIcon(fm.Icon)
			m.Loaders = append(m.Loaders, "fabric")
		}
	}
	if b := read("quilt.mod.json"); b != nil {
		var qm struct {
			Loader struct {
				ID       string `json:"id"`
				Version  string `json:"version"`
				Metadata struct {
					Name         string          `json:"name"`
					Description  string          `json:"description"`
					Contributors map[string]any  `json:"contributors"`
					Icon         json.RawMessage `json:"icon"`
				} `json:"metadata"`
			} `json:"quilt_loader"`
		}
		if lenientJSON(b, &qm) == nil {
			l := qm.Loader
			if m.ModID == "" {
				m.ModID, m.Version = l.ID, l.Version
				m.Name, m.Description = l.Metadata.Name, l.Metadata.Description
				for k := range l.Metadata.Contributors {
					m.Authors = append(m.Authors, k)
				}
				sort.Strings(m.Authors)
				iconPath = pickIcon(l.Metadata.Icon)
			}
			m.Loaders = append(m.Loaders, "quilt")
		}
	}
	for _, tomlName := range []string{"META-INF/neoforge.mods.toml", "META-INF/mods.toml"} {
		b := read(tomlName)
		if b == nil {
			continue
		}
		top, mods := parseModsToml(string(b))
		if strings.Contains(tomlName, "neoforge") {
			m.Loaders = append(m.Loaders, "neoforge")
		} else {
			m.Loaders = append(m.Loaders, "forge")
		}
		if m.ModID != "" || len(mods) == 0 {
			break
		}
		md := mods[0]
		m.ModID = md["modId"]
		m.Name = md["displayName"]
		m.Version = md["version"]
		m.Description = strings.TrimSpace(md["description"])
		auth := md["authors"]
		if auth == "" {
			auth = top["authors"]
		}
		if auth != "" {
			for _, a := range strings.Split(auth, ",") {
				if a = strings.TrimSpace(a); a != "" {
					m.Authors = append(m.Authors, a)
				}
			}
		}
		if lf := md["logoFile"]; lf != "" {
			iconPath = lf
		} else if lf := top["logoFile"]; lf != "" {
			iconPath = lf
		}
		if strings.Contains(m.Version, "${") {
			m.Version = manifestVersion(read("META-INF/MANIFEST.MF"))
		}
		break
	}
	if m.ModID == "" {
		if b := read("mcmod.info"); b != nil {
			var list []struct {
				ModID       string   `json:"modid"`
				Name        string   `json:"name"`
				Description string   `json:"description"`
				Version     string   `json:"version"`
				AuthorList  []string `json:"authorList"`
				Authors     []string `json:"authors"`
				LogoFile    string   `json:"logoFile"`
			}
			if lenientJSON(b, &list) != nil {
				var wrap struct {
					ModList json.RawMessage `json:"modList"`
				}
				if lenientJSON(b, &wrap) == nil {
					lenientJSON(wrap.ModList, &list)
				}
			}
			if len(list) > 0 {
				x := list[0]
				m.ModID, m.Name, m.Description, m.Version = x.ModID, x.Name, x.Description, x.Version
				m.Authors = append(x.AuthorList, x.Authors...)
				iconPath = x.LogoFile
				m.Loaders = append(m.Loaders, "forge")
			}
		}
	}
	if m.Name == "" {
		m.Name = m.ModID
	}
	if iconPath != "" {
		if img := read(iconPath); img != nil {
			m.Icon, m.IconWide = storeIcon(img, iconPath)
		}
	}
	if m.Icon == "" {

		for _, cand := range []string{"pack.png", "icon.png", "logo.png"} {
			if img := read(cand); img != nil {
				m.Icon, m.IconWide = storeIcon(img, cand)
				break
			}
		}
	}
	return m, nil
}

func storeIcon(img []byte, name string) (string, bool) {
	h := sha1.Sum(img)
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".gif" && ext != ".webp" {
		ext = ".png"
	}
	file := hex.EncodeToString(h[:]) + ext
	dst := store.CacheDir("icons", file)
	if _, err := os.Stat(dst); err != nil {
		os.MkdirAll(filepath.Dir(dst), 0o755)
		os.WriteFile(dst, img, 0o644)
	}
	wide := false
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(img)); err == nil && cfg.Height > 0 {
		wide = float64(cfg.Width)/float64(cfg.Height) > 1.4
	}
	return "/api/icon/" + file, wide
}

func lenientJSON(b []byte, v any) error {
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	if err := json.Unmarshal(b, v); err == nil {
		return nil
	}

	clean := bytes.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, b)
	return json.Unmarshal(clean, v)
}

func parsePeople(raw []json.RawMessage) []string {
	var out []string
	for _, r := range raw {
		var s string
		if json.Unmarshal(r, &s) == nil {
			out = append(out, s)
			continue
		}
		var o struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(r, &o) == nil && o.Name != "" {
			out = append(out, o.Name)
		}
	}
	return out
}

func pickIcon(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var m map[string]string
	if json.Unmarshal(raw, &m) == nil {
		best, bestN := "", -1
		for k, v := range m {
			n := 0
			fmt.Sscanf(k, "%d", &n)
			if n > bestN {
				best, bestN = v, n
			}
		}
		return best
	}
	return ""
}

func manifestVersion(b []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		l := sc.Text()
		if strings.HasPrefix(l, "Implementation-Version:") {
			return strings.TrimSpace(strings.TrimPrefix(l, "Implementation-Version:"))
		}
	}
	return ""
}

func parseModsToml(s string) (map[string]string, []map[string]string) {
	top := map[string]string{}
	var mods []map[string]string
	cur := top
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if strings.HasPrefix(line, "[[mods]]") {
				nm := map[string]string{}
				mods = append(mods, nm)
				cur = nm
			} else {
				cur = nil
			}
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		key := strings.Trim(strings.TrimSpace(line[:eq]), `"'`)
		val := strings.TrimSpace(line[eq+1:])
		var out string
		switch {
		case strings.HasPrefix(val, `'''`) || strings.HasPrefix(val, `"""`):
			q := val[:3]
			rest := val[3:]
			if j := strings.Index(rest, q); j >= 0 {
				out = rest[:j]
			} else {
				var sb strings.Builder
				sb.WriteString(rest)
				for i++; i < len(lines); i++ {
					if j := strings.Index(lines[i], q); j >= 0 {
						sb.WriteString("\n" + lines[i][:j])
						break
					}
					sb.WriteString("\n" + lines[i])
				}
				out = sb.String()
			}
			if q == `"""` {
				out = unescapeToml(out)
			}
		case strings.HasPrefix(val, `"`):
			out = unescapeToml(readQuoted(val[1:], '"'))
		case strings.HasPrefix(val, `'`):
			out = readQuoted(val[1:], '\'')
		case strings.HasPrefix(val, "["):
			inner := strings.TrimSuffix(strings.TrimPrefix(strings.SplitN(val, "]", 2)[0], "["), "]")
			var parts []string
			for _, p := range strings.Split(inner, ",") {
				p = strings.Trim(strings.TrimSpace(p), `"'`)
				if p != "" {
					parts = append(parts, p)
				}
			}
			out = strings.Join(parts, ", ")
		default:
			if j := strings.Index(val, "#"); j >= 0 {
				val = val[:j]
			}
			out = strings.TrimSpace(val)
		}
		if cur != nil {
			if _, exists := cur[key]; !exists {
				cur[key] = strings.TrimSpace(out)
			}
		}
	}
	return top, mods
}

func readQuoted(s string, q byte) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && q == '"' {
			i++
			continue
		}
		if s[i] == q {
			return s[:i]
		}
	}
	return s
}

func unescapeToml(s string) string {
	r := strings.NewReplacer(`\n`, "\n", `\t`, "\t", `\"`, `"`, `\\`, `\`)
	return r.Replace(s)
}

type ModInfo struct {
	Key         string   `json:"key"`
	File        string   `json:"file"`
	Enabled     bool     `json:"enabled"`
	ModID       string   `json:"modId"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Authors     []string `json:"authors"`
	Icon        string   `json:"icon"`
	Loaders     []string `json:"loaders"`
	Size        int64    `json:"size"`
	Category    string   `json:"category"`
	Source      string   `json:"source"`
	ProjectID   string   `json:"projectId"`
	PageURL     string   `json:"pageUrl"`
	Added       int64    `json:"added"`
	Dependency  bool     `json:"dependency"`
}

func modsDir(id string) string { return filepath.Join(store.GameDir(id), "mods") }

func modKey(file string) string { return strings.TrimSuffix(file, ".disabled") }

func listModFiles(id string) map[string]string {
	out := map[string]string{}
	entries, _ := os.ReadDir(modsDir(id))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		low := strings.ToLower(n)
		if strings.HasSuffix(low, ".jar") || strings.HasSuffix(low, ".jar.disabled") || strings.HasSuffix(low, ".zip") || strings.HasSuffix(low, ".zip.disabled") {
			out[modKey(n)] = n
		}
	}
	return out
}

func scanMods(id string) ([]ModInfo, error) {
	files := listModFiles(id)
	var out []ModInfo
	var needLookup []string
	jars := map[string]JarMeta{}
	sizes := map[string]int64{}
	for key, file := range files {
		path := filepath.Join(modsDir(id), file)
		jars[key], _ = readJar(path)
		if st, err := os.Stat(path); err == nil {
			sizes[key] = st.Size()
		}
	}
	err := store.Update(id, func(in *Instance) error {
		al := in.activeList()
		for key, file := range files {
			meta := in.Mods[key]
			if meta == nil {
				meta = &ModMeta{Source: "local", Added: time.Now().Unix()}
				in.Mods[key] = meta
			}
			enabled := !strings.HasSuffix(file, ".disabled")
			if _, ok := al.Enabled[key]; !ok {
				al.Enabled[key] = enabled
			}
			jm := jars[key]
			if jm.SHA1 != "" {
				meta.SHA1 = jm.SHA1
			}
			mi := ModInfo{
				Key: key, File: file, Enabled: enabled,
				ModID: jm.ModID, Name: jm.Name, Version: jm.Version, Description: jm.Description,
				Authors: jm.Authors, Icon: jm.Icon, Loaders: jm.Loaders,
				Category: meta.Category, Source: meta.Source, ProjectID: meta.ProjectID, PageURL: meta.PageURL,
				Added: meta.Added, Dependency: meta.Dependency,
			}
			mi.Size = sizes[key]
			if mi.Name == "" {
				mi.Name = meta.Title
			}
			if mi.Name == "" {
				mi.Name = strings.TrimSuffix(strings.TrimSuffix(key, ".jar"), ".zip")
			}
			if mi.Description == "" {
				mi.Description = meta.Summary
			}

			if meta.IconURL != "" && (mi.Icon == "" || jm.IconWide) {
				mi.Icon = meta.IconURL
			}
			if !meta.Checked && meta.SHA1 != "" {
				needLookup = append(needLookup, key)
			}
			out = append(out, mi)
		}

		for key := range in.Mods {
			if _, ok := files[key]; !ok {
				delete(in.Mods, key)
				for i := range in.Modlists {
					delete(in.Modlists[i].Enabled, key)
				}
			}
		}

		valid := map[string]bool{}
		for _, c := range in.Categories {
			valid[c.ID] = true
		}
		for i := range out {
			if out[i].Category != "" && !valid[out[i].Category] {
				out[i].Category = ""
			}
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	if len(needLookup) > 0 {
		go enrichFromModrinth(id, needLookup)
	}
	if out == nil {
		out = []ModInfo{}
	}
	return out, err
}

func applyModlist(id string) error {
	files := listModFiles(id)
	return store.Update(id, func(in *Instance) error {
		al := in.activeList()
		for key, file := range files {
			want, ok := al.Enabled[key]
			if !ok {
				want = false
				al.Enabled[key] = false
			}
			setModFileState(id, key, file, want)
		}
		return nil
	})
}

func setModFileState(id, key, file string, enabled bool) error {
	cur := filepath.Join(modsDir(id), file)
	var target string
	if enabled {
		target = filepath.Join(modsDir(id), key)
	} else {
		target = filepath.Join(modsDir(id), key+".disabled")
	}
	if cur == target {
		return nil
	}
	return os.Rename(cur, target)
}

func toggleMods(id string, keys []string, enabled bool) error {
	files := listModFiles(id)
	return store.Update(id, func(in *Instance) error {
		al := in.activeList()
		for _, k := range keys {
			f, ok := files[k]
			if !ok {
				continue
			}
			if err := setModFileState(id, k, f, enabled); err != nil {
				return fmt.Errorf("não foi possível alterar %s (o jogo está aberto?)", k)
			}
			al.Enabled[k] = enabled
		}
		return nil
	})
}

func deleteMods(id string, keys []string) error {
	files := listModFiles(id)
	return store.Update(id, func(in *Instance) error {
		for _, k := range keys {
			if f, ok := files[k]; ok {
				if err := os.Remove(filepath.Join(modsDir(id), f)); err != nil {
					return fmt.Errorf("não foi possível remover %s", k)
				}
			}
			delete(in.Mods, k)
			for i := range in.Modlists {
				delete(in.Modlists[i].Enabled, k)
			}
		}
		return nil
	})
}

func setCategory(id string, keys []string, cat string) error {
	return store.Update(id, func(in *Instance) error {
		for _, k := range keys {
			if in.Mods[k] == nil {
				in.Mods[k] = &ModMeta{Source: "local"}
			}
			in.Mods[k].Category = cat
		}
		return nil
	})
}

type CategoryOp struct {
	Action string   `json:"action"`
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Order  []string `json:"order"`
	Value  bool     `json:"value"`
}

func categoryOp(id string, op CategoryOp) (string, error) {
	newCat := ""
	err := store.Update(id, func(in *Instance) error {
		switch op.Action {
		case "create":
			name := strings.TrimSpace(op.Name)
			if name == "" {
				return fmt.Errorf("nome vazio")
			}
			newCat = newID()
			in.Categories = append(in.Categories, Category{ID: newCat, Name: name})
		case "rename":
			for i := range in.Categories {
				if in.Categories[i].ID == op.ID {
					in.Categories[i].Name = strings.TrimSpace(op.Name)
				}
			}
		case "delete":
			for i := range in.Categories {
				if in.Categories[i].ID == op.ID {
					in.Categories = append(in.Categories[:i], in.Categories[i+1:]...)
					break
				}
			}
			for _, m := range in.Mods {
				if m.Category == op.ID {
					m.Category = ""
				}
			}
		case "reorder":
			pos := map[string]int{}
			for i, c := range op.Order {
				pos[c] = i
			}
			sort.SliceStable(in.Categories, func(i, j int) bool {
				a, ok1 := pos[in.Categories[i].ID]
				b, ok2 := pos[in.Categories[j].ID]
				if !ok1 {
					a = 1 << 20
				}
				if !ok2 {
					b = 1 << 20
				}
				return a < b
			})
		case "collapse":
			for i := range in.Categories {
				if in.Categories[i].ID == op.ID {
					in.Categories[i].Collapsed = op.Value
				}
			}
		default:
			return fmt.Errorf("ação inválida")
		}
		return nil
	})
	return newCat, err
}

type ModlistOp struct {
	Action string `json:"action"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Copy   bool   `json:"copy"`
}

func modlistOp(id string, op ModlistOp) error {
	if isRunning(id) && (op.Action == "switch" || op.Action == "delete") {
		return fmt.Errorf("feche o jogo antes de trocar de modlist")
	}
	err := store.Update(id, func(in *Instance) error {
		switch op.Action {
		case "create":
			name := strings.TrimSpace(op.Name)
			if name == "" {
				name = fmt.Sprintf("Modlist %d", len(in.Modlists)+1)
			}
			ml := Modlist{ID: newID(), Name: name, Enabled: map[string]bool{}}
			if op.Copy {
				for k, v := range in.activeList().Enabled {
					ml.Enabled[k] = v
				}
			}
			in.Modlists = append(in.Modlists, ml)
			in.ActiveModlist = ml.ID
		case "rename":
			for i := range in.Modlists {
				if in.Modlists[i].ID == op.ID {
					in.Modlists[i].Name = strings.TrimSpace(op.Name)
				}
			}
		case "delete":
			if len(in.Modlists) <= 1 {
				return fmt.Errorf("a instância precisa de pelo menos uma modlist")
			}
			for i := range in.Modlists {
				if in.Modlists[i].ID == op.ID {
					in.Modlists = append(in.Modlists[:i], in.Modlists[i+1:]...)
					break
				}
			}
			if in.ActiveModlist == op.ID {
				in.ActiveModlist = in.Modlists[0].ID
			}
		case "switch":
			in.ActiveModlist = op.ID
			in.normalize()
		default:
			return fmt.Errorf("ação inválida")
		}
		return nil
	})
	if err != nil {
		return err
	}
	return applyModlist(id)
}

func copyMods(from, to string, keys []string) (int, error) {
	src, ok := store.Get(from)
	if !ok {
		return 0, errNotFound
	}
	if _, ok := store.Get(to); !ok {
		return 0, errNotFound
	}
	files := listModFiles(from)
	catName := map[string]string{}
	for _, c := range src.Categories {
		catName[c.ID] = c.Name
	}
	os.MkdirAll(modsDir(to), 0o755)
	n := 0
	err := store.Update(to, func(dst *Instance) error {
		al := dst.activeList()
		for _, k := range keys {
			f, ok := files[k]
			if !ok {
				continue
			}
			if err := copyFile(filepath.Join(modsDir(from), f), filepath.Join(modsDir(to), k)); err != nil {
				return err
			}

			os.Remove(filepath.Join(modsDir(to), k+".disabled"))
			meta := ModMeta{}
			if m := src.Mods[k]; m != nil {
				meta = *m
			}
			meta.Added = time.Now().Unix()
			if cn := catName[meta.Category]; cn != "" {
				found := ""
				for _, c := range dst.Categories {
					if strings.EqualFold(c.Name, cn) {
						found = c.ID
					}
				}
				if found == "" {
					found = newID()
					dst.Categories = append(dst.Categories, Category{ID: found, Name: cn})
				}
				meta.Category = found
			} else {
				meta.Category = ""
			}
			dst.Mods[k] = &meta
			al.Enabled[k] = true
			n++
		}
		return nil
	})
	return n, err
}

func importModFile(id, name string, r io.Reader) error {
	name = filepath.Base(name)
	low := strings.ToLower(name)
	if !strings.HasSuffix(low, ".jar") && !strings.HasSuffix(low, ".zip") {
		return fmt.Errorf("%s não é um .jar", name)
	}
	os.MkdirAll(modsDir(id), 0o755)
	dst := filepath.Join(modsDir(id), name)
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		os.Remove(dst)
		return err
	}
	f.Close()
	return store.Update(id, func(in *Instance) error {
		in.Mods[name] = &ModMeta{Source: "local", Added: time.Now().Unix()}
		in.activeList().Enabled[name] = true
		return nil
	})
}
