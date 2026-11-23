package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const versionManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"

type ManifestVersion struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	URL         string `json:"url"`
	ReleaseTime string `json:"releaseTime"`
	SHA1        string `json:"sha1"`
}

type VersionManifest struct {
	Latest   map[string]string `json:"latest"`
	Versions []ManifestVersion `json:"versions"`
}

func getManifest() (*VersionManifest, error) {
	var m VersionManifest
	err := cachedJSON(versionManifestURL, "version_manifest_v2.json", 30*time.Minute, &m)
	return &m, err
}

type Artifact struct {
	ID   string `json:"id,omitempty"`
	Path string `json:"path"`
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

type Rule struct {
	Action string `json:"action"`
	OS     *struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Arch    string `json:"arch"`
	} `json:"os,omitempty"`
	Features map[string]bool `json:"features,omitempty"`
}

type Library struct {
	Name      string `json:"name"`
	URL       string `json:"url,omitempty"`
	SHA1      string `json:"sha1,omitempty"`
	Size      int64  `json:"size,omitempty"`
	Downloads *struct {
		Artifact    *Artifact           `json:"artifact,omitempty"`
		Classifiers map[string]Artifact `json:"classifiers,omitempty"`
	} `json:"downloads,omitempty"`
	Natives map[string]string `json:"natives,omitempty"`
	Rules   []Rule            `json:"rules,omitempty"`
	Extract *struct {
		Exclude []string `json:"exclude"`
	} `json:"extract,omitempty"`
	ClientReq *bool `json:"clientreq,omitempty"`
	ServerReq *bool `json:"serverreq,omitempty"`
}

type Arguments struct {
	Game []json.RawMessage `json:"game"`
	JVM  []json.RawMessage `json:"jvm"`
}

type VersionJSON struct {
	ID                 string     `json:"id"`
	InheritsFrom       string     `json:"inheritsFrom,omitempty"`
	Type               string     `json:"type"`
	MainClass          string     `json:"mainClass"`
	MinecraftArguments string     `json:"minecraftArguments,omitempty"`
	Arguments          *Arguments `json:"arguments,omitempty"`
	AssetIndex         *struct {
		ID        string `json:"id"`
		SHA1      string `json:"sha1"`
		Size      int64  `json:"size"`
		TotalSize int64  `json:"totalSize"`
		URL       string `json:"url"`
	} `json:"assetIndex,omitempty"`
	Assets      string              `json:"assets,omitempty"`
	Downloads   map[string]Artifact `json:"downloads,omitempty"`
	Libraries   []Library           `json:"libraries"`
	JavaVersion *struct {
		Component    string `json:"component"`
		MajorVersion int    `json:"majorVersion"`
	} `json:"javaVersion,omitempty"`
	Logging *struct {
		Client *struct {
			Argument string   `json:"argument"`
			File     Artifact `json:"file"`
			Type     string   `json:"type"`
		} `json:"client,omitempty"`
	} `json:"logging,omitempty"`
	Jar string `json:"jar,omitempty"`

	jarID       string
	inherited   bool
	explicitJar bool
}

func mcOSName() string {
	switch runtime.GOOS {
	case "windows":
		return "windows"
	case "darwin":
		return "osx"
	default:
		return "linux"
	}
}

func rulesAllow(rules []Rule, features map[string]bool) bool {
	if len(rules) == 0 {
		return true
	}
	allowed := false
	for _, r := range rules {
		match := true
		if r.OS != nil {
			if r.OS.Name != "" && r.OS.Name != mcOSName() {
				match = false
			}
			if r.OS.Arch != "" {
				is32 := runtime.GOARCH == "386"
				if r.OS.Arch == "x86" && !is32 {
					match = false
				}
				if r.OS.Arch == "arm64" && runtime.GOARCH != "arm64" {
					match = false
				}
			}
		}
		for f, want := range r.Features {
			if features[f] != want {
				match = false
			}
		}
		if match {
			allowed = r.Action == "allow"
		}
	}
	return allowed
}

func mavenPath(coord string) string {
	ext := "jar"
	if i := strings.Index(coord, "@"); i >= 0 {
		ext = coord[i+1:]
		coord = coord[:i]
	}
	p := strings.Split(coord, ":")
	if len(p) < 3 {
		return ""
	}
	group := strings.ReplaceAll(p[0], ".", "/")
	file := p[1] + "-" + p[2]
	if len(p) >= 4 && p[3] != "" {
		file += "-" + p[3]
	}
	return group + "/" + p[1] + "/" + p[2] + "/" + file + "." + ext
}

func libKey(name string) string {
	p := strings.Split(strings.SplitN(name, "@", 2)[0], ":")
	if len(p) >= 4 {
		return p[0] + ":" + p[1] + ":" + p[3]
	}
	if len(p) >= 2 {
		return p[0] + ":" + p[1]
	}
	return name
}

func fixMavenURL(u string) string {
	u = strings.Replace(u, "http://files.minecraftforge.net/maven", "https://maven.minecraftforge.net", 1)
	u = strings.Replace(u, "https://files.minecraftforge.net/maven", "https://maven.minecraftforge.net", 1)
	u = strings.Replace(u, "http://", "https://", 1)
	return u
}

func versionPath(id string) string {
	return store.SharedDir("versions", id, id+".json")
}

func fetchVanillaJSON(id string) error {
	m, err := getManifest()
	if err != nil {
		return err
	}
	for _, v := range m.Versions {
		if v.ID == id {
			return downloadFile(DL{URL: v.URL, Path: versionPath(id), SHA1: v.SHA1}, nil)
		}
	}
	return fmt.Errorf("versão %s não existe no manifesto da Mojang", id)
}

func loadVersionRaw(id string) (*VersionJSON, error) {
	p := versionPath(id)
	if _, err := os.Stat(p); err != nil {
		if err := fetchVanillaJSON(id); err != nil {
			return nil, err
		}
	}
	var v VersionJSON
	if err := readJSON(p, &v); err != nil {
		return nil, fmt.Errorf("lendo %s: %w", id, err)
	}
	return &v, nil
}

func loadVersion(id string) (*VersionJSON, error) {
	v, err := loadVersionRaw(id)
	if err != nil {
		return nil, err
	}
	if v.InheritsFrom == "" {
		v.jarID = v.ID
		if v.Jar != "" {
			v.jarID = v.Jar
		}
		return v, nil
	}
	parent, err := loadVersion(v.InheritsFrom)
	if err != nil {
		return nil, err
	}
	merged := *parent
	merged.ID = v.ID
	merged.InheritsFrom = ""
	if v.MainClass != "" {
		merged.MainClass = v.MainClass
	}
	if v.MinecraftArguments != "" {
		merged.MinecraftArguments = v.MinecraftArguments
	}
	if v.Type != "" {
		merged.Type = v.Type
	}
	if v.Arguments != nil {
		a := &Arguments{}
		if parent.Arguments != nil {
			a.Game = append(a.Game, parent.Arguments.Game...)
			a.JVM = append(a.JVM, parent.Arguments.JVM...)
		}
		a.Game = append(a.Game, v.Arguments.Game...)
		a.JVM = append(a.JVM, v.Arguments.JVM...)
		merged.Arguments = a
	}
	if v.AssetIndex != nil {
		merged.AssetIndex = v.AssetIndex
		merged.Assets = v.Assets
	}
	if v.JavaVersion != nil {
		merged.JavaVersion = v.JavaVersion
	}
	if v.Logging != nil && v.Logging.Client != nil {
		merged.Logging = v.Logging
	}
	merged.inherited = true
	merged.explicitJar = v.Jar != ""
	if v.Jar != "" {
		merged.jarID = v.Jar
	}

	seen := map[string]bool{}
	var libs []Library
	for _, l := range v.Libraries {
		k := libKey(l.Name)
		if l.Natives != nil {
			k += ":natives"
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		libs = append(libs, l)
	}
	for _, l := range parent.Libraries {
		k := libKey(l.Name)
		if l.Natives != nil {
			k += ":natives"
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		libs = append(libs, l)
	}
	merged.Libraries = libs
	return &merged, nil
}

type ResolvedLib struct {
	DL
	Native    bool
	Exclude   []string
	Classpath bool
}

func resolveLibraries(v *VersionJSON) []ResolvedLib {
	libDir := store.SharedDir("libraries")
	var out []ResolvedLib
	for _, l := range v.Libraries {
		if !rulesAllow(l.Rules, nil) {
			continue
		}

		var art *Artifact
		if l.Downloads != nil && l.Downloads.Artifact != nil {
			art = l.Downloads.Artifact
		}
		if l.Natives == nil || art != nil {
			if art != nil {
				path := art.Path
				if path == "" {
					path = mavenPath(l.Name)
				}
				out = append(out, ResolvedLib{
					DL:        DL{URL: art.URL, Path: filepath.Join(libDir, filepath.FromSlash(path)), SHA1: art.SHA1, Size: art.Size},
					Classpath: true,
					Native:    strings.Contains(l.Name, ":natives-"),
				})
			} else if l.Natives == nil {
				path := mavenPath(l.Name)
				base := l.URL
				if base == "" {
					base = "https://libraries.minecraft.net/"
				}
				if !strings.HasSuffix(base, "/") {
					base += "/"
				}
				out = append(out, ResolvedLib{
					DL:        DL{URL: fixMavenURL(base + path), Path: filepath.Join(libDir, filepath.FromSlash(path)), SHA1: l.SHA1, Size: l.Size, Optional: l.SHA1 == ""},
					Classpath: true,
					Native:    strings.Contains(l.Name, ":natives-"),
				})
			}
		}

		if l.Natives != nil {
			cls, ok := l.Natives[mcOSName()]
			if !ok {
				continue
			}
			arch := "64"
			if runtime.GOARCH == "386" {
				arch = "32"
			}
			cls = strings.ReplaceAll(cls, "${arch}", arch)
			var ex []string
			if l.Extract != nil {
				ex = l.Extract.Exclude
			}
			if l.Downloads != nil {
				if a, ok := l.Downloads.Classifiers[cls]; ok {
					path := a.Path
					if path == "" {
						path = mavenPath(l.Name + ":" + cls)
					}
					out = append(out, ResolvedLib{DL: DL{URL: a.URL, Path: filepath.Join(libDir, filepath.FromSlash(path)), SHA1: a.SHA1, Size: a.Size}, Native: true, Exclude: ex})
					continue
				}
			}
			path := mavenPath(l.Name + ":" + cls)
			base := l.URL
			if base == "" {
				base = "https://libraries.minecraft.net/"
			}
			if !strings.HasSuffix(base, "/") {
				base += "/"
			}
			out = append(out, ResolvedLib{DL: DL{URL: fixMavenURL(base + path), Path: filepath.Join(libDir, filepath.FromSlash(path))}, Native: true, Exclude: ex})
		}
	}
	return out
}

func clientJarPath(v *VersionJSON) string {
	id := v.jarID
	if id == "" {
		id = v.ID
	}
	return store.SharedDir("versions", id, id+".jar")
}

type AssetIndex struct {
	Objects map[string]struct {
		Hash string `json:"hash"`
		Size int64  `json:"size"`
	} `json:"objects"`
	Virtual        bool `json:"virtual"`
	MapToResources bool `json:"map_to_resources"`
}

func prepareAssets(t *Task, v *VersionJSON, gameDir string) (assetsRoot string, err error) {
	assetsDir := store.SharedDir("assets")
	if v.AssetIndex == nil {
		return assetsDir, nil
	}
	idxPath := filepath.Join(assetsDir, "indexes", v.AssetIndex.ID+".json")
	if err := downloadFile(DL{URL: v.AssetIndex.URL, Path: idxPath, SHA1: v.AssetIndex.SHA1}, nil); err != nil {
		if _, serr := os.Stat(idxPath); serr != nil {
			return "", err
		}
	}
	var idx AssetIndex
	if err := readJSON(idxPath, &idx); err != nil {
		return "", err
	}
	var dls []DL
	for _, o := range idx.Objects {
		h := o.Hash
		dls = append(dls, DL{
			URL:  "https://resources.download.minecraft.net/" + h[:2] + "/" + h,
			Path: filepath.Join(assetsDir, "objects", h[:2], h),
			SHA1: h, Size: o.Size,
		})
	}
	if err := downloadAll(t, "Assets", dls); err != nil {
		return "", err
	}
	if idx.Virtual || idx.MapToResources {
		target := filepath.Join(assetsDir, "virtual", v.AssetIndex.ID)
		if idx.MapToResources {
			target = filepath.Join(gameDir, "resources")
		}
		for name, o := range idx.Objects {
			dst := filepath.Join(target, filepath.FromSlash(name))
			if _, err := os.Stat(dst); err == nil {
				continue
			}
			src := filepath.Join(assetsDir, "objects", o.Hash[:2], o.Hash)
			copyFile(src, dst)
		}
		if idx.Virtual {
			return target, nil
		}
	}
	return assetsDir, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	os.MkdirAll(filepath.Dir(dst), 0o755)
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	cerr := out.Close()
	if err != nil {
		return err
	}
	return cerr
}

func extractNatives(libs []ResolvedLib, dir string) error {
	os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, l := range libs {
		if !l.Native {
			continue
		}
		zr, err := zip.OpenReader(l.Path)
		if err != nil {
			continue
		}
		for _, f := range zr.File {
			name := f.Name
			if f.FileInfo().IsDir() || strings.HasPrefix(name, "META-INF") {
				continue
			}
			skip := false
			for _, ex := range l.Exclude {
				if strings.HasPrefix(name, ex) {
					skip = true
				}
			}
			low := strings.ToLower(name)
			if !(strings.HasSuffix(low, ".dll") || strings.HasSuffix(low, ".so") || strings.HasSuffix(low, ".dylib") || strings.HasSuffix(low, ".jnilib")) {
				skip = true
			}
			if skip || strings.Contains(name, "..") {
				continue
			}
			dst := filepath.Join(dir, filepath.Base(filepath.FromSlash(name)))
			rc, err := f.Open()
			if err != nil {
				continue
			}
			out, err := os.Create(dst)
			if err == nil {
				io.Copy(out, rc)
				out.Close()
			}
			rc.Close()
		}
		zr.Close()
	}
	return nil
}

type Prepared struct {
	Version    *VersionJSON
	Libs       []ResolvedLib
	ClientJar  string
	AssetsRoot string
	Natives    string
	Java       string
	LogConfig  string
}

func prepareVersion(t *Task, versionID, gameDir string, javaOverride string) (*Prepared, error) {
	t.Stage("Lendo versão "+versionID, 0)
	v, err := loadVersion(versionID)
	if err != nil {
		return nil, err
	}
	p := &Prepared{Version: v}

	var dls []DL
	if c, ok := v.Downloads["client"]; ok {
		p.ClientJar = clientJarPath(v)
		dls = append(dls, DL{URL: c.URL, Path: p.ClientJar, SHA1: c.SHA1, Size: c.Size})
	} else {
		p.ClientJar = clientJarPath(v)
	}
	p.Libs = resolveLibraries(v)
	for _, l := range p.Libs {
		dls = append(dls, l.DL)
	}
	if v.Logging != nil && v.Logging.Client != nil && v.Logging.Client.File.URL != "" {
		f := v.Logging.Client.File
		p.LogConfig = store.SharedDir("assets", "log_configs", f.ID)
		dls = append(dls, DL{URL: f.URL, Path: p.LogConfig, SHA1: f.SHA1, Size: f.Size})
	}
	if err := downloadAll(t, "Bibliotecas", dls); err != nil {
		return nil, err
	}

	if v.inherited && !v.explicitJar {
		own := store.SharedDir("versions", v.ID, v.ID+".jar")
		src, serr := os.Stat(p.ClientJar)
		dst, derr := os.Stat(own)
		if serr == nil && (derr != nil || dst.Size() != src.Size()) {
			if err := copyFile(p.ClientJar, own); err != nil {
				return nil, fmt.Errorf("copiando o jar do jogo: %w", err)
			}
		}
		if serr == nil {
			p.ClientJar = own
		}
	}
	p.AssetsRoot, err = prepareAssets(t, v, gameDir)
	if err != nil {
		return nil, err
	}
	p.Natives = filepath.Join(store.InstanceDir(filepath.Base(filepath.Dir(gameDir))), "natives")
	t.Stage("Extraindo nativos", 0)
	if err := extractNatives(p.Libs, p.Natives); err != nil {
		return nil, err
	}
	p.Java, err = ensureJava(t, v, javaOverride)
	if err != nil {
		return nil, err
	}
	return p, nil
}
