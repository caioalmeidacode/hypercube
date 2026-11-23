package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

type log4jParser struct {
	emit    func(string)
	inEvent bool
	head    string
	field   string
	buf     strings.Builder
	msg     string
	thr     string
}

var log4jAttr = regexp.MustCompile(`(\w+)="([^"]*)"`)

func (p *log4jParser) Line(line string) {
	t := strings.TrimSpace(line)
	if !p.inEvent {
		if strings.HasPrefix(t, "<log4j:Event") {
			p.inEvent, p.msg, p.thr, p.field = true, "", "", ""
			attrs := map[string]string{}
			for _, m := range log4jAttr.FindAllStringSubmatch(t, -1) {
				attrs[m[1]] = m[2]
			}
			ts := ""
			if ms, err := strconv.ParseInt(attrs["timestamp"], 10, 64); err == nil {
				ts = time.UnixMilli(ms).Format("15:04:05")
			}
			logger := attrs["logger"]
			if i := strings.LastIndex(logger, "."); i >= 0 {
				logger = logger[i+1:]
			}
			p.head = "[" + ts + "] [" + attrs["thread"] + "/" + attrs["level"] + "] [" + logger + "]: "
			return
		}
		p.emit(line)
		return
	}
	if p.field != "" {
		if i := strings.Index(line, "]]>"); i >= 0 {
			p.buf.WriteString("\n" + line[:i])
			p.finishField()
		} else {
			p.buf.WriteString("\n" + line)
		}
		return
	}
	for _, f := range []struct{ tag, name string }{{"<log4j:Message><![CDATA[", "msg"}, {"<log4j:Throwable><![CDATA[", "thr"}} {
		if strings.HasPrefix(t, f.tag) {
			rest := t[len(f.tag):]
			p.buf.Reset()
			p.field = f.name
			if i := strings.Index(rest, "]]>"); i >= 0 {
				p.buf.WriteString(rest[:i])
				p.finishField()
			} else {
				p.buf.WriteString(rest)
			}
			return
		}
	}
	if strings.HasPrefix(t, "</log4j:Event>") {
		p.inEvent = false
		for i, l := range strings.Split(strings.TrimRight(p.msg, "\n"), "\n") {
			if i == 0 {
				p.emit(p.head + l)
			} else {
				p.emit(l)
			}
		}
		if p.thr != "" {
			for _, l := range strings.Split(strings.TrimRight(p.thr, "\n"), "\n") {
				p.emit(strings.TrimRight(l, "\r"))
			}
		}
	}
}

func (p *log4jParser) finishField() {
	if p.field == "msg" {
		p.msg = p.buf.String()
	} else {
		p.thr = p.buf.String()
	}
	p.field = ""
}
