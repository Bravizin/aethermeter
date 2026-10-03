package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"aethermeter/internal/applog"
)

// Hotkeys maps an action to a key combo like "Ctrl+Alt+L".
// Actions: lock, reset, hide, copy, mode, party, tab.
type Hotkeys map[string]string

func DefaultHotkeys() Hotkeys {
	return Hotkeys{
		"lock":  "Ctrl+Alt+L",
		"reset": "Ctrl+Alt+R",
		"hide":  "Ctrl+Alt+H",
		"copy":  "Ctrl+Alt+C",
		"mode":  "Ctrl+Alt+M",
		"party": "Ctrl+Alt+P",
		"tab":   "Ctrl+Alt+T",
	}
}

type Config struct {
	X           int     `json:"x"`
	Y           int     `json:"y"`
	Width       int     `json:"width"`
	Opacity     int     `json:"opacity"` // 40..255
	Mode        int     `json:"mode"`    // combat.ViewMode
	Metric      int     `json:"metric"`  // combat.Metric (aba: DPS / cura / tank)
	PartyOnly   bool    `json:"partyOnly"`
	Locked      bool    `json:"locked"`
	MaxRows     int     `json:"maxRows"`
	IdleSeconds int     `json:"idleSeconds"`
	Scale       float64 `json:"scale"` // extra UI scale on top of DPI (1.0 = normal)
	HasPos      bool    `json:"hasPos"`
	Hotkeys     Hotkeys `json:"hotkeys"`
	SelfName    string  `json:"selfName,omitempty"` // your character, remembered for solo play
	SelfClass   int     `json:"selfClass,omitempty"`
}

func Default() Config {
	return Config{Width: 380, Opacity: 235, MaxRows: 8, IdleSeconds: 12, Scale: 1.0, Hotkeys: DefaultHotkeys()}
}

func path() string { return filepath.Join(applog.Dir(), "config.json") }

func Load() Config {
	c := Default()
	b, err := os.ReadFile(path())
	if err == nil {
		_ = json.Unmarshal(b, &c)
	}
	if c.Width < 300 {
		c.Width = 380
	}
	if c.Opacity < 40 || c.Opacity > 255 {
		c.Opacity = 235
	}
	if c.MaxRows < 3 || c.MaxRows > 24 {
		c.MaxRows = 8
	}
	if c.IdleSeconds < 3 || c.IdleSeconds > 120 {
		c.IdleSeconds = 12
	}
	if c.Scale < 0.7 || c.Scale > 2 {
		c.Scale = 1
	}
	if c.Metric < 0 || c.Metric > 2 {
		c.Metric = 0
	}
	// fill in any missing actions with the defaults
	def := DefaultHotkeys()
	if c.Hotkeys == nil {
		c.Hotkeys = Hotkeys{}
	}
	for k, v := range def {
		if c.Hotkeys[k] == "" {
			c.Hotkeys[k] = v
		}
	}
	return c
}

func (c Config) Save() error {
	b, _ := json.MarshalIndent(c, "", "  ")
	return os.WriteFile(path(), b, 0o644)
}
