@echo off
REM Compila o Hypercube.exe (requer Go 1.22+ : https://go.dev/dl/)
cd /d "%~dp0"
if not exist rsrc_windows_amd64.syso go run tools/winres/main.go
set CGO_ENABLED=0
go build -trimpath -ldflags "-s -w -H windowsgui" -o dist\Hypercube.exe .
if errorlevel 1 (
  echo.
  echo Falha na compilacao.
  pause
  exit /b 1
)
echo.
echo Pronto: dist\Hypercube.exe
pause
