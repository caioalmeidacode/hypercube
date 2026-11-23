#!/bin/sh
# Compila o Hypercube.exe para Windows a partir de Linux/macOS (requer Go 1.22+)
set -e
cd "$(dirname "$0")"
[ -f rsrc_windows_amd64.syso ] || go run tools/winres/main.go
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H windowsgui" -o dist/Hypercube.exe .
echo "Pronto: dist/Hypercube.exe"
