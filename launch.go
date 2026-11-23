package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

type GameProc struct {
	cmd     *exec.Cmd
	started time.Time
	mu      sync.Mutex
	logs    []string
}

var (
	procMu sync.Mutex
	procs  = map[string]*GameProc{}
	logBuf = map[string][]string{}
)

func RunningGames() int {
	procMu.Lock()
	defer procMu.Unlock()
	return len(procs)
}

func isRunning(id string) bool {
	procMu.Lock()
	defer procMu.Unlock()
	_, ok := procs[id]
	return ok
}

func appendLog(id, line string) {
	procMu.Lock()
	b := append(logBuf[id], line)
	if len(b) > 5000 {
		b = b[len(b)-5000:]
	}
	logBuf[id] = b
	procMu.Unlock()
	hub.Emit("log", map[string]string{"instance": id, "line": line})
}

func getLogs(id string) []string {
	procMu.Lock()
	defer procMu.Unlock()
	return append([]string{}, logBuf[id]...)
}

func clearLogs(id string) {
	procMu.Lock()
	delete(logBuf, id)
	procMu.Unlock()
}

func offlineUUID(name string) string {
	h := md5.Sum([]byte("OfflinePlayer:" + name))
	h[6] = (h[6] & 0x0f) | 0x30
	h[8] = (h[8] & 0x3f) | 0x80
	return hex.EncodeToString(h[:])
}

func dashUUID(u string) string {
	u = strings.ReplaceAll(u, "-", "")
	if len(u) != 32 {
		return u
	}
	return u[0:8] + "-" + u[8:12] + "-" + u[12:16] + "-" + u[16:20] + "-" + u[20:]
}

var argVar = regexp.MustCompile(`\$\{([a-zA-Z0-9_]+)\}`)

func substitute(s string, vars map[string]string) string {
	return argVar.ReplaceAllStringFunc(s, func(m string) string {
		k := m[2 : len(m)-1]
		if v, ok := vars[k]; ok {
			return v
		}
		return m
	})
}

func collectArgs(raw []json.RawMessage, features map[string]bool) []string {
	var out []string
	for _, r := range raw {
		var s string
		if json.Unmarshal(r, &s) == nil {
			out = append(out, s)
			continue
		}
		var obj struct {
			Rules []Rule          `json:"rules"`
			Value json.RawMessage `json:"value"`
		}
		if json.Unmarshal(r, &obj) != nil || !rulesAllow(obj.Rules, features) {
			continue
		}
		var one string
		if json.Unmarshal(obj.Value, &one) == nil {
			out = append(out, one)
			continue
		}
		var many []string
		if json.Unmarshal(obj.Value, &many) == nil {
			out = append(out, many...)
		}
	}
	return out
}

func splitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inQ := rune(0)
	for _, r := range s {
		switch {
		case inQ != 0:
			if r == inQ {
				inQ = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '"' || r == '\'':
			inQ = r
		case r == ' ' || r == '\t' || r == '\n':
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func installInstance(id string) error {
	inst, ok := store.Get(id)
	if !ok {
		return errNotFound
	}
	t := NewTask("Instalando "+inst.Name, id)
	err := func() error {
		t.Stage("Minecraft "+inst.MCVersion, 0)
		if _, err := loadVersion(inst.MCVersion); err != nil {
			return err
		}
		vid, err := installLoader(t, inst.MCVersion, inst.Loader, inst.LoaderVersion)
		if err != nil {
			return err
		}
		if _, err := prepareVersion(t, vid, store.GameDir(id), inst.JavaPath); err != nil {
			return err
		}
		return store.Update(id, func(in *Instance) error {
			in.VersionID = vid
			in.Installed = true
			return nil
		})
	}()
	t.Finish(err)
	emitInstance(id)
	return err
}

func emitInstance(id string) {
	if in, ok := store.Get(id); ok {
		in.Mods = nil
		hub.Emit("instance", map[string]any{"instance": in, "running": isRunning(id)})
	}
}

func launchInstance(id string) error {
	if isRunning(id) {
		return fmt.Errorf("a instância já está em execução")
	}
	inst, ok := store.Get(id)
	if !ok {
		return errNotFound
	}
	acc, err := gameIdentity(id)
	if err != nil {
		return err
	}
	t := NewTask("Iniciando "+inst.Name, id)
	err = func() error {
		vid := inst.VersionID
		if !inst.Installed || vid == "" {
			if _, err := loadVersion(inst.MCVersion); err != nil {
				return err
			}
			v, err := installLoader(t, inst.MCVersion, inst.Loader, inst.LoaderVersion)
			if err != nil {
				return err
			}
			vid = v
		}
		gameDir := store.GameDir(id)
		os.MkdirAll(gameDir, 0o755)
		applyModlist(id)
		p, err := prepareVersion(t, vid, gameDir, inst.JavaPath)
		if err != nil {
			return err
		}
		store.Update(id, func(in *Instance) error { in.VersionID = vid; in.Installed = true; return nil })
		t.Stage("Abrindo o jogo", 0)
		return startProcess(id, inst, acc, p, gameDir)
	}()
	t.Finish(err)
	emitInstance(id)
	return err
}

func startProcess(id string, inst Instance, acc GameIdentity, p *Prepared, gameDir string) error {
	st := store.Settings()
	v := p.Version
	sep := ":"
	if runtime.GOOS == "windows" {
		sep = ";"
	}
	var cp []string
	seen := map[string]bool{}
	for _, l := range p.Libs {
		if !l.Classpath || seen[l.Path] {
			continue
		}
		if _, err := os.Stat(l.Path); err != nil {
			continue
		}
		seen[l.Path] = true
		cp = append(cp, l.Path)
	}
	if _, err := os.Stat(p.ClientJar); err == nil {
		cp = append(cp, p.ClientJar)
	}
	uuid := acc.UUID
	if uuid == "" {
		uuid = offlineUUID(acc.Name)
	}
	token := acc.Token
	userType := acc.UserType
	assetsIndex := v.Assets
	if v.AssetIndex != nil {
		assetsIndex = v.AssetIndex.ID
	}
	vars := map[string]string{
		"auth_player_name":    acc.Name,
		"auth_username":       acc.Name,
		"version_name":        v.ID,
		"game_directory":      gameDir,
		"assets_root":         p.AssetsRoot,
		"game_assets":         p.AssetsRoot,
		"assets_index_name":   assetsIndex,
		"auth_uuid":           strings.ReplaceAll(uuid, "-", ""),
		"auth_access_token":   token,
		"auth_session":        "token:" + token + ":" + strings.ReplaceAll(uuid, "-", ""),
		"auth_xuid":           acc.XUID,
		"clientid":            "0",
		"user_type":           userType,
		"user_properties":     "{}",
		"version_type":        "Hypercube",
		"natives_directory":   p.Natives,
		"launcher_name":       "Hypercube",
		"launcher_version":    appVersion,
		"classpath":           strings.Join(cp, sep),
		"classpath_separator": sep,
		"library_directory":   store.SharedDir("libraries"),
		"resolution_width":    strconv.Itoa(st.Width),
		"resolution_height":   strconv.Itoa(st.Height),
		"primary_jar":         p.ClientJar,
	}
	features := map[string]bool{"has_custom_resolution": st.Width > 0 && st.Height > 0}

	mem := inst.MemoryMB
	if mem <= 0 {
		mem = st.MemoryMB
	}
	var args []string
	args = append(args, fmt.Sprintf("-Xmx%dM", mem), "-Xms512M")
	if runtime.GOOS == "darwin" {
		args = append(args, "-XstartOnFirstThread")
	}
	extra := inst.JVMArgs
	if strings.TrimSpace(extra) == "" {
		extra = st.JVMArgs
	}
	args = append(args, splitArgs(extra)...)
	if v.Arguments != nil && len(v.Arguments.JVM) > 0 {
		for _, a := range collectArgs(v.Arguments.JVM, features) {
			args = append(args, substitute(a, vars))
		}
	} else {
		args = append(args,
			"-Djava.library.path="+p.Natives,
			"-Dminecraft.launcher.brand=Hypercube",
			"-Dminecraft.launcher.version="+appVersion,
			"-cp", vars["classpath"])
	}
	if p.LogConfig != "" && v.Logging != nil && v.Logging.Client != nil {
		args = append(args, strings.ReplaceAll(v.Logging.Client.Argument, "${path}", p.LogConfig))
	}
	args = append(args, v.MainClass)
	if v.Arguments != nil && len(v.Arguments.Game) > 0 {
		for _, a := range collectArgs(v.Arguments.Game, features) {
			args = append(args, substitute(a, vars))
		}
	} else {
		for _, a := range strings.Fields(v.MinecraftArguments) {
			args = append(args, substitute(a, vars))
		}
		if features["has_custom_resolution"] {
			args = append(args, "--width", vars["resolution_width"], "--height", vars["resolution_height"])
		}
	}

	java := p.Java
	major := 8
	if v.JavaVersion != nil {
		major = v.JavaVersion.MajorVersion
	}
	finalArgs := args
	if major >= 9 && runtime.GOOS == "windows" {

		argfile := filepath.Join(store.InstanceDir(id), "launch.args")
		var sb strings.Builder
		for _, a := range args {
			sb.WriteString(`"` + strings.ReplaceAll(strings.ReplaceAll(a, `\`, `\\`), `"`, `\"`) + `"` + "\n")
		}
		if err := os.WriteFile(argfile, []byte(sb.String()), 0o644); err == nil {
			finalArgs = []string{"@" + argfile}
		}
	}

	cmd := exec.Command(java, finalArgs...)
	cmd.Dir = gameDir
	hideWindow(cmd)
	clearLogs(id)
	masked := strings.Join(args, " ")
	if len(token) > 4 {
		masked = strings.ReplaceAll(masked, token, "•••")
	}
	appendLog(id, "[Hypercube] Java: "+java)
	appendLog(id, "[Hypercube] Versão: "+v.ID+" · Main: "+v.MainClass)
	if len(masked) > 4000 {
		masked = masked[:4000] + " …"
	}
	appendLog(id, "[Hypercube] Args: "+masked)
	lp := &log4jParser{emit: func(l string) { appendLog(id, l) }}
	lw := &lineWriter{fn: lp.Line}
	cmd.Stdout = lw
	cmd.Stderr = lw
	if err := cmd.Start(); err != nil {
		return err
	}
	gp := &GameProc{cmd: cmd, started: time.Now()}
	procMu.Lock()
	procs[id] = gp
	procMu.Unlock()
	store.Update(id, func(in *Instance) error { in.LastPlayed = time.Now().Unix(); return nil })
	emitInstance(id)
	go func() {
		err := cmd.Wait()
		lw.Flush()
		code := 0
		if cmd.ProcessState != nil {
			code = cmd.ProcessState.ExitCode()
		}
		procMu.Lock()
		delete(procs, id)
		procMu.Unlock()
		dur := int64(time.Since(gp.started).Seconds())
		store.Update(id, func(in *Instance) error { in.Playtime += dur; return nil })
		msg := fmt.Sprintf("[Hypercube] Jogo encerrado (código %d)", code)
		if err != nil && code == 0 {
			msg += ": " + err.Error()
		}
		appendLog(id, msg)
		hub.Emit("exit", map[string]any{"instance": id, "code": code, "crashed": code != 0 && time.Since(gp.started) < 10*time.Minute})
		emitInstance(id)
	}()
	return nil
}

func killInstance(id string) error {
	procMu.Lock()
	gp, ok := procs[id]
	procMu.Unlock()
	if !ok || gp.cmd.Process == nil {
		return fmt.Errorf("não está em execução")
	}
	return gp.cmd.Process.Kill()
}
