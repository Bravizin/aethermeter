// Package applog is a tiny file logger (the overlay has no console).
package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	mu   sync.Mutex
	f    *os.File
	path string
)

// Dir returns (and creates) the app data folder, e.g. %APPDATA%\AetherMeter.
func Dir() string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	d := filepath.Join(base, "AetherMeter")
	if _, err := os.Stat(d); os.IsNotExist(err) {
		_ = os.MkdirAll(d, 0o755)
		// carry the settings over from the old name (PartyMeter)
		if b, err := os.ReadFile(filepath.Join(base, "PartyMeter", "config.json")); err == nil {
			_ = os.WriteFile(filepath.Join(d, "config.json"), b, 0o644)
		}
	}
	return d
}

// Open starts logging to aethermeter.log (previous log kept as .old).
func Open() string {
	mu.Lock()
	defer mu.Unlock()
	path = filepath.Join(Dir(), "aethermeter.log")
	_ = os.Rename(path, path+".old")
	f, _ = os.Create(path)
	return path
}

func Path() string { return path }

func Printf(format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	line := time.Now().Format("15:04:05.000 ") + fmt.Sprintf(format, args...) + "\n"
	if f != nil {
		_, _ = f.WriteString(line)
	} else {
		fmt.Fprint(os.Stderr, line)
	}
}

func Close() {
	mu.Lock()
	defer mu.Unlock()
	if f != nil {
		f.Close()
		f = nil
	}
}
