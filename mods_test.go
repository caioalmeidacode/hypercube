package main

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkJar(t *testing.T, path string, files map[string]string) {
	f, _ := os.Create(path)
	zw := zip.NewWriter(f)
	for n, c := range files {
		w, _ := zw.Create(n)
		w.Write([]byte(c))
	}
	zw.Close()
	f.Close()
}

func TestParse(t *testing.T) {
	dir := t.TempDir()
	store, _ = openStore(dir)
	png := "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x10\x00\x00\x00\x10\x08\x06\x00\x00\x00"
	mkJar(t, filepath.Join(dir, "a.jar"), map[string]string{
		"fabric.mod.json": `{"id":"sodium","name":"Sodium","version":"0.5.8","description":"A modern
rendering engine","authors":["JellySquid",{"name":"IMS"}],"icon":{"16":"a16.png","128":"assets/sodium/icon.png"}}`,
		"assets/sodium/icon.png": png,
	})
	m, err := parseJar(filepath.Join(dir, "a.jar"))
	if err != nil || m.Name != "Sodium" || len(m.Authors) != 2 || m.Icon == "" {
		t.Fatalf("fabric: %+v %v", m, err)
	}
	mkJar(t, filepath.Join(dir, "b.jar"), map[string]string{
		"META-INF/mods.toml": `modLoader="javafml" #comment
loaderVersion="[47,)"
license='MIT'
logoFile="logo.png"
[[mods]] #mod
modId="jei"
version="${file.jarVersion}"
displayName="Just Enough Items"
authors="mezz, others"
description='''
JEI is an item and recipe viewing mod.
Second line.
'''
[[dependencies.jei]]
modId="forge"
`,
		"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\nImplementation-Version: 15.2.0.27\n",
		"logo.png":             png,
	})
	m, err = parseJar(filepath.Join(dir, "b.jar"))
	if m.Name != "Just Enough Items" || m.Version != "15.2.0.27" || m.Icon == "" || len(m.Authors) != 2 || m.Description == "" {
		t.Fatalf("forge: %+v", m)
	}
	t.Logf("%+v", m)
	if neoforgeMC("21.1.77") != "1.21.1" || neoforgeMC("21.0.10-beta") != "1.21" || neoforgeMC("26.1.0.5") != "26.1" {
		t.Fatal("neoforge mapping")
	}
	if mavenPath("net.fabricmc:fabric-loader:0.15.0") != "net/fabricmc/fabric-loader/0.15.0/fabric-loader-0.15.0.jar" {
		t.Fatal("maven")
	}
	if cmpVersion("1.20.1-47.10.0", "1.20.1-47.9.3") <= 0 {
		t.Fatal("cmp")
	}
}

func TestLog4j(t *testing.T) {
	var out []string
	p := &log4jParser{emit: func(s string) { out = append(out, s) }}
	for _, l := range []string{
		`<log4j:Event logger="cpw.mods.modlauncher.Launcher" timestamp="1795466762697" level="INFO" thread="main">`,
		`  <log4j:Message><![CDATA[ModLauncher running]]></log4j:Message>`,
		`</log4j:Event>`,
		`Exception in thread "main" boom`,
		`<log4j:Event logger="x.Y" timestamp="1" level="ERROR" thread="Render thread">`,
		`  <log4j:Message><![CDATA[line one`, `line two]]></log4j:Message>`,
		`  <log4j:Throwable><![CDATA[java.lang.Err: x`, `	at a.b(C.java:1)`, `]]></log4j:Throwable>`,
		`</log4j:Event>`,
	} {
		p.Line(l)
	}
	if len(out) != 6 || !strings.HasSuffix(out[0], "[main/INFO] [Launcher]: ModLauncher running") || out[1] != `Exception in thread "main" boom` || !strings.Contains(out[2], "[Render thread/ERROR] [Y]: line one") || out[4] != "java.lang.Err: x" {
		t.Fatalf("%q", out)
	}
}
