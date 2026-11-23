//go:build ignore

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"sort"
	"unicode/utf16"
)

const (
	rtIcon      = 3
	rtGroupIcon = 14
	rtVersion   = 16
	rtManifest  = 24
	langEN      = 0x0409
)

type res struct {
	typ, id uint32
	data    []byte
}

func le16(b *bytes.Buffer, v uint16) { binary.Write(b, binary.LittleEndian, v) }
func le32(b *bytes.Buffer, v uint32) { binary.Write(b, binary.LittleEndian, v) }

func utf16z(s string) []byte {
	var b bytes.Buffer
	for _, r := range utf16.Encode([]rune(s)) {
		le16(&b, r)
	}
	le16(&b, 0)
	return b.Bytes()
}

func pad4(b *bytes.Buffer) {
	for b.Len()%4 != 0 {
		b.WriteByte(0)
	}
}

func versionBlock(key string, value []byte, valueLen uint16, typ uint16, children [][]byte) []byte {
	var b bytes.Buffer
	le16(&b, 0)
	le16(&b, valueLen)
	le16(&b, typ)
	b.Write(utf16z(key))
	pad4(&b)
	b.Write(value)
	for _, c := range children {
		pad4(&b)
		b.Write(c)
	}
	out := b.Bytes()
	binary.LittleEndian.PutUint16(out[0:], uint16(len(out)))
	return out
}

func versionInfo(major, minor, patch uint16, strs [][2]string) []byte {
	var fixed bytes.Buffer
	ms := uint32(major)<<16 | uint32(minor)
	ls := uint32(patch) << 16
	for _, v := range []uint32{0xFEEF04BD, 0x00010000, ms, ls, ms, ls, 0x3F, 0, 0x40004, 1, 0, 0, 0} {
		le32(&fixed, v)
	}
	var strings [][]byte
	for _, kv := range strs {
		val := utf16z(kv[1])
		strings = append(strings, versionBlock(kv[0], val, uint16(len(val)/2), 1, nil))
	}
	table := versionBlock("040904B0", nil, 0, 1, strings)
	sfi := versionBlock("StringFileInfo", nil, 0, 1, [][]byte{table})
	var tr bytes.Buffer
	le16(&tr, 0x0409)
	le16(&tr, 0x04B0)
	vr := versionBlock("Translation", tr.Bytes(), 4, 0, nil)
	vfi := versionBlock("VarFileInfo", nil, 0, 1, [][]byte{vr})
	return versionBlock("VS_VERSION_INFO", fixed.Bytes(), 52, 0, [][]byte{sfi, vfi})
}

func iconResources(ico []byte) ([]res, error) {
	if len(ico) < 6 || binary.LittleEndian.Uint16(ico[2:]) != 1 {
		return nil, fmt.Errorf("arquivo .ico inválido")
	}
	n := int(binary.LittleEndian.Uint16(ico[4:]))
	var out []res
	var grp bytes.Buffer
	le16(&grp, 0)
	le16(&grp, 1)
	le16(&grp, uint16(n))
	for i := 0; i < n; i++ {
		e := ico[6+16*i : 6+16*(i+1)]
		size := binary.LittleEndian.Uint32(e[8:])
		off := binary.LittleEndian.Uint32(e[12:])
		id := uint32(i + 1)
		out = append(out, res{rtIcon, id, ico[off : off+size]})
		grp.Write(e[:12])
		le16(&grp, uint16(id))
	}
	out = append(out, res{rtGroupIcon, 1, grp.Bytes()})
	return out, nil
}

const manifest = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity type="win32" name="Hypercube" version="1.0.0.0" processorArchitecture="*"/>
  <description>Hypercube</description>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
    </application>
  </compatibility>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2</dpiAwareness>
    </windowsSettings>
  </application>
</assembly>`

func main() {
	ico, err := os.ReadFile("assets/hypercube.ico")
	if err != nil {
		panic(err)
	}
	resources, err := iconResources(ico)
	if err != nil {
		panic(err)
	}
	resources = append(resources, res{rtVersion, 1, versionInfo(1, 0, 0, [][2]string{
		{"CompanyName", "Hypercube"},
		{"FileDescription", "Hypercube"},
		{"FileVersion", "1.0.0"},
		{"InternalName", "Hypercube"},
		{"LegalCopyright", "Hypercube"},
		{"OriginalFilename", "Hypercube.exe"},
		{"ProductName", "Hypercube"},
		{"ProductVersion", "1.0.0"},
	})})
	resources = append(resources, res{rtManifest, 1, []byte(manifest)})

	byType := map[uint32][]res{}
	var types []uint32
	for _, r := range resources {
		if _, ok := byType[r.typ]; !ok {
			types = append(types, r.typ)
		}
		byType[r.typ] = append(byType[r.typ], r)
	}
	sort.Slice(types, func(i, j int) bool { return types[i] < types[j] })

	rootSize := 16 + 8*len(types)
	off := rootSize
	typeDirOff := map[uint32]int{}
	for _, t := range types {
		typeDirOff[t] = off
		off += 16 + 8*len(byType[t])
	}
	type leaf struct {
		r       res
		langDir int
		dataEnt int
	}
	var leaves []*leaf
	for _, t := range types {
		for _, r := range byType[t] {
			leaves = append(leaves, &leaf{r: r, langDir: off})
			off += 16 + 8
		}
	}
	for _, l := range leaves {
		l.dataEnt = off
		off += 16
	}
	dataStart := (off + 7) &^ 7

	var sec bytes.Buffer
	dirHeader := func(n int) {
		le32(&sec, 0)
		le32(&sec, 0)
		le16(&sec, 0)
		le16(&sec, 0)
		le16(&sec, 0)
		le16(&sec, uint16(n))
	}
	dirHeader(len(types))
	for _, t := range types {
		le32(&sec, t)
		le32(&sec, uint32(typeDirOff[t])|0x80000000)
	}
	li := 0
	for _, t := range types {
		dirHeader(len(byType[t]))
		for _, r := range byType[t] {
			le32(&sec, r.id)
			le32(&sec, uint32(leaves[li].langDir)|0x80000000)
			li++
		}
	}
	for _, l := range leaves {
		dirHeader(1)
		le32(&sec, langEN)
		le32(&sec, uint32(l.dataEnt))
	}
	var relocs []uint32
	dpos := dataStart
	var raw bytes.Buffer
	for _, l := range leaves {
		relocs = append(relocs, uint32(sec.Len()))
		le32(&sec, uint32(dpos))
		le32(&sec, uint32(len(l.r.data)))
		le32(&sec, 0)
		le32(&sec, 0)
		raw.Write(l.r.data)
		for raw.Len()%8 != 0 {
			raw.WriteByte(0)
		}
		dpos = dataStart + raw.Len()
	}
	for sec.Len() < dataStart {
		sec.WriteByte(0)
	}
	sec.Write(raw.Bytes())

	var obj bytes.Buffer
	const fileHdr, secHdr = 20, 40
	rawPtr := fileHdr + secHdr
	relocPtr := rawPtr + sec.Len()
	symPtr := relocPtr + 10*len(relocs)
	le16(&obj, 0x8664)
	le16(&obj, 1)
	le32(&obj, 0)
	le32(&obj, uint32(symPtr))
	le32(&obj, 1)
	le16(&obj, 0)
	le16(&obj, 0)
	obj.WriteString(".rsrc\x00\x00\x00")
	le32(&obj, 0)
	le32(&obj, 0)
	le32(&obj, uint32(sec.Len()))
	le32(&obj, uint32(rawPtr))
	le32(&obj, uint32(relocPtr))
	le32(&obj, 0)
	le16(&obj, uint16(len(relocs)))
	le16(&obj, 0)
	le32(&obj, 0x40000040)
	obj.Write(sec.Bytes())
	for _, r := range relocs {
		le32(&obj, r)
		le32(&obj, 0)
		le16(&obj, 3)
	}
	obj.WriteString(".rsrc\x00\x00\x00")
	le32(&obj, 0)
	binary.Write(&obj, binary.LittleEndian, int16(1))
	le16(&obj, 0)
	obj.WriteByte(3)
	obj.WriteByte(0)
	le32(&obj, 4)

	if err := os.WriteFile("rsrc_windows_amd64.syso", obj.Bytes(), 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("rsrc_windows_amd64.syso: %d bytes, %d recursos\n", obj.Len(), len(leaves))
}
