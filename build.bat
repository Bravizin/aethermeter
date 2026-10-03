@echo off
rem Compila o AetherMeter.exe (precisa do Go 1.24+)
go test ./... || exit /b 1
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0
go build -trimpath -ldflags "-s -w -H windowsgui" -o dist\AetherMeter.exe ./cmd/aethermeter && echo OK: dist\AetherMeter.exe
