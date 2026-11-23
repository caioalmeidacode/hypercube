package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type LoaderVersion struct {
	Version     string `json:"version"`
	Stable      bool   `json:"stable"`
	Recommended bool   `json:"recommended"`
}

func loaderVersions(loader, mc string) ([]LoaderVersion, error) {
	switch loader {
	case "fabric":
		return fabricLikeVersions("https://meta.fabricmc.net/v2/versions/loader/"+mc, "fabric_"+mc+".json")
	case "quilt":
		return fabricLikeVersions("https://meta.quiltmc.org/v3/versions/loader/"+mc, "quilt_"+mc+".json")
	case "forge":
		return forgeVersions(mc)
	case "neoforge":
		return neoforgeVersions(mc)
	case "vanilla", "":
		return []LoaderVersion{}, nil
	}
	return nil, fmt.Errorf("loader desconhecido: %s", loader)
}

func fabricLikeVersions(url, cache string) ([]LoaderVersion, error) {
	var list []struct {
		Loader struct {
			Version string `json:"version"`
			Stable  *bool  `json:"stable"`
		} `json:"loader"`
	}
	if err := cachedJSON(url, cache, 20*time.Minute, &list); err != nil {
		return nil, err
	}
	out := []LoaderVersion{}
	rec := false
	for _, l := range list {
		stable := l.Loader.Stable == nil || *l.Loader.Stable
		if strings.Contains(l.Loader.Version, "beta") || strings.Contains(l.Loader.Version, "pre") {
			stable = false
		}
		lv := LoaderVersion{Version: l.Loader.Version, Stable: stable}
		if stable && !rec {
			lv.Recommended = true
			rec = true
		}
		out = append(out, lv)
	}
	return out, nil
}

type mavenMetadata struct {
	Versioning struct {
		Versions struct {
			Version []string `xml:"version"`
		} `xml:"versions"`
	} `xml:"versioning"`
}

func cmpVersion(a, b string) int {
	split := func(s string) []string {
		return strings.FieldsFunc(s, func(r rune) bool { return r == '.' || r == '-' || r == '+' })
	}
	pa, pb := split(a), split(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y string
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		xi, ex := strconv.Atoi(x)
		yi, ey := strconv.Atoi(y)
		if ex == nil && ey == nil {
			if xi != yi {
				if xi < yi {
					return -1
				}
				return 1
			}
			continue
		}
		if x != y {

			if ex == nil {
				return 1
			}
			if ey == nil {
				return -1
			}
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func forgeVersions(mc string) ([]LoaderVersion, error) {
	path := store.CacheDir("meta", "forge_maven.xml")
	var data []byte
	if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < 30*time.Minute {
		data, _ = os.ReadFile(path)
	}
	if data == nil {
		d, err := doRequest("GET", "https://maven.minecraftforge.net/net/minecraftforge/forge/maven-metadata.xml", nil, nil)
		if err != nil {
			d, _ = os.ReadFile(path)
			if d == nil {
				return nil, err
			}
		} else {
			os.MkdirAll(filepath.Dir(path), 0o755)
			os.WriteFile(path, d, 0o644)
		}
		data = d
	}
	var md mavenMetadata
	if err := xml.Unmarshal(data, &md); err != nil {
		return nil, err
	}
	var promos struct {
		Promos map[string]string `json:"promos"`
	}
	cachedJSON("https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json", "forge_promos.json", 30*time.Minute, &promos)
	rec := promos.Promos[mc+"-recommended"]
	latest := promos.Promos[mc+"-latest"]
	out := []LoaderVersion{}
	for _, v := range md.Versioning.Versions.Version {
		if !strings.HasPrefix(v, mc+"-") {
			continue
		}
		short := strings.TrimPrefix(v, mc+"-")
		lv := LoaderVersion{Version: v, Stable: true}
		base := strings.TrimSuffix(short, "-"+mc)
		if base == rec {
			lv.Recommended = true
		}
		_ = latest
		out = append(out, lv)
	}
	sort.Slice(out, func(i, j int) bool { return cmpVersion(out[i].Version, out[j].Version) > 0 })
	if rec == "" && len(out) > 0 {
		out[0].Recommended = true
	}
	return out, nil
}

func neoforgeMC(v string) string {
	base := strings.SplitN(v, "-", 2)[0]
	p := strings.Split(base, ".")
	if len(p) < 2 {
		return ""
	}
	major, err := strconv.Atoi(p[0])
	if err != nil {
		return ""
	}
	if major >= 25 {
		mc := p[0] + "." + p[1]
		if len(p) >= 4 && p[2] != "0" {
			mc += "." + p[2]
		}
		return mc
	}
	if p[1] == "0" {
		return "1." + p[0]
	}
	return "1." + p[0] + "." + p[1]
}

func neoforgeVersions(mc string) ([]LoaderVersion, error) {
	out := []LoaderVersion{}
	if mc == "1.20.1" {
		var r struct {
			Versions []string `json:"versions"`
		}
		if err := cachedJSON("https://maven.neoforged.net/api/maven/versions/releases/net/neoforged/forge", "neoforge_legacy.json", 30*time.Minute, &r); err != nil {
			return nil, err
		}
		for _, v := range r.Versions {
			if strings.HasPrefix(v, "1.20.1-") {
				out = append(out, LoaderVersion{Version: v, Stable: true})
			}
		}
	} else {
		var r struct {
			Versions []string `json:"versions"`
		}
		if err := cachedJSON("https://maven.neoforged.net/api/maven/versions/releases/net/neoforged/neoforge", "neoforge.json", 30*time.Minute, &r); err != nil {
			return nil, err
		}
		for _, v := range r.Versions {
			if neoforgeMC(v) == mc {
				out = append(out, LoaderVersion{Version: v, Stable: !strings.Contains(v, "beta") && !strings.Contains(v, "alpha")})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return cmpVersion(out[i].Version, out[j].Version) > 0 })
	for i := range out {
		if out[i].Stable {
			out[i].Recommended = true
			break
		}
	}
	if len(out) > 0 && !out[0].Recommended {
		has := false
		for _, o := range out {
			has = has || o.Recommended
		}
		if !has {
			out[0].Recommended = true
		}
	}
	return out, nil
}

func installLoader(t *Task, mc, loader, lv string) (string, error) {
	switch loader {
	case "", "vanilla":
		return mc, nil
	case "fabric":
		return installFabricLike(t, "https://meta.fabricmc.net/v2/versions/loader/"+mc+"/"+lv+"/profile/json")
	case "quilt":
		return installFabricLike(t, "https://meta.quiltmc.org/v3/versions/loader/"+mc+"/"+lv+"/profile/json")
	case "forge":
		url := fmt.Sprintf("https://maven.minecraftforge.net/net/minecraftforge/forge/%s/forge-%s-installer.jar", lv, lv)
		return installForgeLike(t, mc, url, "forge-"+lv+"-installer.jar")
	case "neoforge":
		var url string
		if strings.HasPrefix(lv, "1.20.1-") {
			url = fmt.Sprintf("https://maven.neoforged.net/releases/net/neoforged/forge/%s/forge-%s-installer.jar", lv, lv)
		} else {
			url = fmt.Sprintf("https://maven.neoforged.net/releases/net/neoforged/neoforge/%s/neoforge-%s-installer.jar", lv, lv)
		}
		return installForgeLike(t, mc, url, "neoforge-"+lv+"-installer.jar")
	}
	return "", fmt.Errorf("loader desconhecido: %s", loader)
}

func installFabricLike(t *Task, url string) (string, error) {
	t.Stage("Baixando perfil do loader", 0)
	data, err := doRequest("GET", url, nil, nil)
	if err != nil {
		return "", err
	}
	var v struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &v); err != nil || v.ID == "" {
		return "", fmt.Errorf("perfil do loader inválido")
	}
	p := versionPath(v.ID)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return "", err
	}
	return v.ID, nil
}

func readZipFile(zr *zip.Reader, name string) ([]byte, error) {
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, os.ErrNotExist
}

func installForgeLike(t *Task, mc, url, fileName string) (string, error) {
	installer := store.CacheDir("installers", fileName)
	t.Stage("Baixando instalador", 0)
	if err := downloadFile(DL{URL: url, Path: installer}, nil); err != nil {
		return "", err
	}
	data, err := os.ReadFile(installer)
	if err != nil {
		return "", err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("instalador corrompido: %w", err)
	}
	profRaw, err := readZipFile(zr, "install_profile.json")
	if err != nil {
		return "", fmt.Errorf("install_profile.json ausente")
	}
	var prof struct {
		Version string `json:"version"`
		Install *struct {
			Path     string `json:"path"`
			FilePath string `json:"filePath"`
			Target   string `json:"target"`
		} `json:"install"`
		VersionInfo json.RawMessage `json:"versionInfo"`
	}
	if err := json.Unmarshal(profRaw, &prof); err != nil {
		return "", err
	}

	if prof.Install != nil && len(prof.VersionInfo) > 0 {
		var vi struct {
			ID string `json:"id"`
		}
		json.Unmarshal(prof.VersionInfo, &vi)
		id := vi.ID
		if id == "" {
			id = prof.Install.Target
		}
		var viMap map[string]any
		json.Unmarshal(prof.VersionInfo, &viMap)
		if _, ok := viMap["inheritsFrom"]; !ok {
			viMap["inheritsFrom"] = mc
		}
		viMap["id"] = id
		if _, ok := viMap["jar"]; !ok {
			viMap["jar"] = mc
		}
		if err := writeJSON(versionPath(id), viMap); err != nil {
			return "", err
		}
		uni, err := readZipFile(zr, prof.Install.FilePath)
		if err != nil {
			return "", fmt.Errorf("universal jar ausente no instalador")
		}
		dst := store.SharedDir("libraries", filepath.FromSlash(mavenPath(prof.Install.Path)))
		os.MkdirAll(filepath.Dir(dst), 0o755)
		if err := os.WriteFile(dst, uni, 0o644); err != nil {
			return "", err
		}
		return id, nil
	}

	vjRaw, err := readZipFile(zr, "version.json")
	if err != nil {
		return "", fmt.Errorf("version.json ausente no instalador")
	}
	var vj struct {
		ID string `json:"id"`
	}
	json.Unmarshal(vjRaw, &vj)
	if vj.ID == "" {
		vj.ID = prof.Version
	}

	if _, err := os.Stat(versionPath(vj.ID)); err == nil {
		if ok, _ := forgeLibsPresent(vj.ID); ok {
			return vj.ID, nil
		}
	}

	if _, err := prepareVanillaForInstaller(t, mc); err != nil {
		return "", err
	}
	java, err := javaForMC(t, mc)
	if err != nil {
		return "", err
	}
	root := store.SharedDir()
	lp := filepath.Join(root, "launcher_profiles.json")
	if _, err := os.Stat(lp); err != nil {
		os.WriteFile(lp, []byte(`{"profiles":{},"selectedProfile":"","clientToken":"hypercube"}`), 0o644)
	}
	t.Stage("Executando instalador do loader (pode levar alguns minutos)", 0)
	var lastOut string
	for _, flag := range []string{"--installClient", "--install-client"} {
		cmd := exec.Command(java, "-jar", installer, flag, root)
		cmd.Dir = store.CacheDir("installers")
		hideWindow(cmd)
		var buf bytes.Buffer
		pw := &lineWriter{fn: func(line string) {
			buf.WriteString(line + "\n")
			if l := strings.TrimSpace(line); l != "" {
				if len(l) > 110 {
					l = l[:110] + "…"
				}
				t.SetDetail(l)
			}
		}}
		cmd.Stdout = pw
		cmd.Stderr = pw
		err = cmd.Run()
		pw.Flush()
		lastOut = buf.String()
		if err == nil {
			if _, serr := os.Stat(versionPath(vj.ID)); serr == nil {
				return vj.ID, nil
			}

			if id := findInstalledVersion(vj.ID); id != "" {
				return id, nil
			}
		}
		if !strings.Contains(lastOut, "not a recognized option") && !strings.Contains(lastOut, "UnrecognizedOptionException") {
			break
		}
	}
	tail := lastOut
	if len(tail) > 1500 {
		tail = tail[len(tail)-1500:]
	}
	return "", fmt.Errorf("o instalador do loader falhou: %v\n%s", err, tail)
}

func findInstalledVersion(id string) string {
	entries, _ := os.ReadDir(store.SharedDir("versions"))
	for _, e := range entries {
		if strings.EqualFold(e.Name(), id) {
			return e.Name()
		}
	}
	return ""
}

func forgeLibsPresent(id string) (bool, error) {
	v, err := loadVersion(id)
	if err != nil {
		return false, err
	}
	for _, l := range resolveLibraries(v) {
		if _, err := os.Stat(l.Path); err != nil {
			return false, nil
		}
	}
	return true, nil
}

func prepareVanillaForInstaller(t *Task, mc string) (*VersionJSON, error) {
	v, err := loadVersion(mc)
	if err != nil {
		return nil, err
	}
	var dls []DL
	if c, ok := v.Downloads["client"]; ok {
		dls = append(dls, DL{URL: c.URL, Path: clientJarPath(v), SHA1: c.SHA1, Size: c.Size})
	}
	for _, l := range resolveLibraries(v) {
		dls = append(dls, l.DL)
	}
	return v, downloadAll(t, "Minecraft "+mc, dls)
}

type lineWriter struct {
	fn  func(string)
	buf []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(w.buf[:i]), "\r")
		w.buf = w.buf[i+1:]
		w.fn(line)
	}
	return len(p), nil
}

func (w *lineWriter) Flush() {
	if len(w.buf) > 0 {
		w.fn(string(w.buf))
		w.buf = nil
	}
}
