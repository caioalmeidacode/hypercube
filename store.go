package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type legacyAccount struct {
	Name string `json:"name"`
}

type MCAccount struct {
	Name         string `json:"name"`
	UUID         string `json:"uuid"`
	XUID         string `json:"xuid,omitempty"`
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    int64  `json:"expiresAt"`
}

type Profile struct {
	Name    string `json:"name"`
	UUID    string `json:"uuid"`
	Avatar  string `json:"avatar"`
	Created int64  `json:"created"`
	Updated int64  `json:"updated"`
}

type Settings struct {
	MemoryMB      int             `json:"memoryMB"`
	JavaPath      string          `json:"javaPath"`
	JVMArgs       string          `json:"jvmArgs"`
	Width         int             `json:"width"`
	Height        int             `json:"height"`
	CurseForgeKey string          `json:"curseforgeKey"`
	Concurrency   int             `json:"concurrency"`
	ShowSnapshots bool            `json:"showSnapshots"`
	Profile       Profile         `json:"profile"`
	MSClientID    string          `json:"msClientId"`
	MCAccount     *MCAccount      `json:"mcAccount,omitempty"`
	Legacy        []legacyAccount `json:"accounts,omitempty"`
	Reduced       bool            `json:"reducedMotion"`
}

type Category struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Collapsed bool   `json:"collapsed"`
}

type Modlist struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Enabled map[string]bool `json:"enabled"`
}

type ModMeta struct {
	Category   string `json:"category,omitempty"`
	Source     string `json:"source,omitempty"`
	ProjectID  string `json:"projectId,omitempty"`
	VersionID  string `json:"versionId,omitempty"`
	Slug       string `json:"slug,omitempty"`
	Title      string `json:"title,omitempty"`
	Summary    string `json:"summary,omitempty"`
	IconURL    string `json:"iconUrl,omitempty"`
	PageURL    string `json:"pageUrl,omitempty"`
	Checked    bool   `json:"checked,omitempty"`
	Added      int64  `json:"added,omitempty"`
	SHA1       string `json:"sha1,omitempty"`
	Dependency bool   `json:"dependency,omitempty"`
}

type Instance struct {
	ID            string              `json:"id"`
	Name          string              `json:"name"`
	Icon          string              `json:"icon"`
	MCVersion     string              `json:"mcVersion"`
	Loader        string              `json:"loader"`
	LoaderVersion string              `json:"loaderVersion"`
	VersionID     string              `json:"versionId"`
	Installed     bool                `json:"installed"`
	Created       int64               `json:"created"`
	LastPlayed    int64               `json:"lastPlayed"`
	Playtime      int64               `json:"playtime"`
	MemoryMB      int                 `json:"memoryMB"`
	JVMArgs       string              `json:"jvmArgs"`
	JavaPath      string              `json:"javaPath"`
	Categories    []Category          `json:"categories"`
	Mods          map[string]*ModMeta `json:"mods"`
	Modlists      []Modlist           `json:"modlists"`
	ActiveModlist string              `json:"activeModlist"`
}

type Store struct {
	mu        sync.Mutex
	root      string
	settings  Settings
	instances map[string]*Instance
}

var store *Store

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Store) SharedDir(parts ...string) string {
	return filepath.Join(append([]string{s.root, "shared"}, parts...)...)
}
func (s *Store) InstanceDir(id string) string { return filepath.Join(s.root, "instances", id) }
func (s *Store) GameDir(id string) string     { return filepath.Join(s.root, "instances", id, "minecraft") }
func (s *Store) CacheDir(parts ...string) string {
	return filepath.Join(append([]string{s.root, "cache"}, parts...)...)
}

func openStore(root string) (*Store, error) {
	s := &Store{root: root, instances: map[string]*Instance{}}
	for _, d := range []string{"instances", "shared", "cache"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			return nil, err
		}
	}
	s.settings = Settings{MemoryMB: 4096, Concurrency: 12, Width: 1280, Height: 720}
	readJSON(filepath.Join(root, "settings.json"), &s.settings)
	if s.settings.Concurrency <= 0 {
		s.settings.Concurrency = 12
	}
	if s.settings.MemoryMB <= 0 {
		s.settings.MemoryMB = 4096
	}
	if s.settings.Profile.Name == "" && len(s.settings.Legacy) > 0 {
		s.settings.Profile = Profile{Name: s.settings.Legacy[0].Name, Created: time.Now().Unix()}
	}
	if s.settings.Legacy != nil {
		s.settings.Legacy = nil
		s.saveSettingsLocked()
	}
	if s.settings.Profile.Name != "" {
		s.settings.Profile.UUID = offlineUUID(s.settings.Profile.Name)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "instances"))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var inst Instance
		if readJSON(filepath.Join(root, "instances", e.Name(), "instance.json"), &inst) == nil && inst.ID != "" {
			inst.normalize()
			s.instances[inst.ID] = &inst
		}
	}
	return s, nil
}

func (in *Instance) normalize() {
	if in.Mods == nil {
		in.Mods = map[string]*ModMeta{}
	}
	if in.Categories == nil {
		in.Categories = []Category{}
	}
	if len(in.Modlists) == 0 {
		in.Modlists = []Modlist{{ID: newID(), Name: "Principal", Enabled: map[string]bool{}}}
	}
	for i := range in.Modlists {
		if in.Modlists[i].Enabled == nil {
			in.Modlists[i].Enabled = map[string]bool{}
		}
	}
	found := false
	for _, m := range in.Modlists {
		if m.ID == in.ActiveModlist {
			found = true
		}
	}
	if !found {
		in.ActiveModlist = in.Modlists[0].ID
	}
}

func (in *Instance) activeList() *Modlist {
	for i := range in.Modlists {
		if in.Modlists[i].ID == in.ActiveModlist {
			return &in.Modlists[i]
		}
	}
	return &in.Modlists[0]
}

func (s *Store) saveSettingsLocked() error {
	return writeJSON(filepath.Join(s.root, "settings.json"), s.settings)
}

func (s *Store) SaveSettings() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveSettingsLocked()
}

func (s *Store) saveInstanceLocked(in *Instance) error {
	os.MkdirAll(s.InstanceDir(in.ID), 0o755)
	return writeJSON(filepath.Join(s.InstanceDir(in.ID), "instance.json"), in)
}

func (s *Store) Update(id string, fn func(in *Instance) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	in, ok := s.instances[id]
	if !ok {
		return errNotFound
	}
	if err := fn(in); err != nil {
		return err
	}
	return s.saveInstanceLocked(in)
}

func (s *Store) Get(id string) (Instance, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	in, ok := s.instances[id]
	if !ok {
		return Instance{}, false
	}
	return in.clone(), true
}

func (in *Instance) clone() Instance {
	b, _ := json.Marshal(in)
	var c Instance
	json.Unmarshal(b, &c)
	c.normalize()
	return c
}

func (s *Store) List() []Instance {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Instance, 0, len(s.instances))
	for _, in := range s.instances {
		c := in.clone()
		c.Mods = nil
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		la, lb := a.LastPlayed, b.LastPlayed
		if la == 0 {
			la = a.Created
		}
		if lb == 0 {
			lb = b.Created
		}
		return la > lb
	})
	return out
}

func (s *Store) Add(in *Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	in.normalize()
	if in.Created == 0 {
		in.Created = time.Now().Unix()
	}
	s.instances[in.ID] = in
	os.MkdirAll(filepath.Join(s.GameDir(in.ID), "mods"), 0o755)
	return s.saveInstanceLocked(in)
}

func (s *Store) Remove(id string) error {
	s.mu.Lock()
	delete(s.instances, id)
	s.mu.Unlock()
	return os.RemoveAll(s.InstanceDir(id))
}

func (s *Store) Settings() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, _ := json.Marshal(s.settings)
	var c Settings
	json.Unmarshal(b, &c)
	return c
}

func (s *Store) UpdateSettings(fn func(st *Settings)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.settings)
	return s.saveSettingsLocked()
}

func (s *Store) Profile() (Profile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.settings.Profile
	return p, p.Name != ""
}

func (s *Store) reloadInstances() {
	entries, _ := os.ReadDir(filepath.Join(s.root, "instances"))
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, ok := s.instances[e.Name()]; ok {
			continue
		}
		var inst Instance
		if readJSON(filepath.Join(s.root, "instances", e.Name(), "instance.json"), &inst) == nil && inst.ID == e.Name() {
			inst.normalize()
			s.instances[inst.ID] = &inst
		}
	}
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func writeJSON(path string, v any) error {
	os.MkdirAll(filepath.Dir(path), 0o755)
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
