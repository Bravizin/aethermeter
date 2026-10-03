package config

import (
	"fmt"
	"strings"
)

// Modifier flags (same values as Win32 MOD_*).
const (
	ModAlt   = 0x0001
	ModCtrl  = 0x0002
	ModShift = 0x0004
	ModWin   = 0x0008
)

// ParseHotkey turns "Ctrl+Alt+L" into modifier flags and a Win32 virtual-key code.
func ParseHotkey(s string) (mods uint32, vk uint32, err error) {
	parts := strings.Split(strings.ReplaceAll(s, " ", ""), "+")
	if len(parts) == 0 || parts[0] == "" {
		return 0, 0, fmt.Errorf("atalho vazio")
	}
	for i, p := range parts {
		up := strings.ToUpper(p)
		last := i == len(parts)-1
		switch up {
		case "CTRL", "CONTROL":
			mods |= ModCtrl
			continue
		case "ALT":
			mods |= ModAlt
			continue
		case "SHIFT":
			mods |= ModShift
			continue
		case "WIN":
			mods |= ModWin
			continue
		}
		if !last {
			return 0, 0, fmt.Errorf("tecla %q no meio do atalho", p)
		}
		v, ok := keyCode(up)
		if !ok {
			return 0, 0, fmt.Errorf("tecla desconhecida %q", p)
		}
		vk = v
	}
	if vk == 0 {
		return 0, 0, fmt.Errorf("falta a tecla em %q", s)
	}
	return mods, vk, nil
}

func keyCode(k string) (uint32, bool) {
	if len(k) == 1 {
		c := k[0]
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			return uint32(c), true
		}
	}
	if len(k) >= 2 && k[0] == 'F' {
		var n int
		if _, err := fmt.Sscanf(k[1:], "%d", &n); err == nil && n >= 1 && n <= 24 {
			return uint32(0x70 + n - 1), true
		}
	}
	if strings.HasPrefix(k, "NUM") && len(k) == 4 && k[3] >= '0' && k[3] <= '9' {
		return uint32(0x60 + int(k[3]-'0')), true
	}
	named := map[string]uint32{
		"HOME": 0x24, "END": 0x23, "INSERT": 0x2D, "INS": 0x2D, "DELETE": 0x2E, "DEL": 0x2E,
		"PAGEUP": 0x21, "PGUP": 0x21, "PAGEDOWN": 0x22, "PGDN": 0x22, "PAUSE": 0x13,
		"SCROLLLOCK": 0x91, "SPACE": 0x20, "TAB": 0x09,
	}
	v, ok := named[k]
	return v, ok
}
