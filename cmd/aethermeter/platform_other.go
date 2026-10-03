//go:build !windows

package main

// Non-Windows builds have no overlay; they print a text table instead, which
// is handy for developing against recordings (go run ./cmd/aethermeter -replay x).

import (
	"fmt"
	"os"
	"time"

	"aethermeter/internal/combat"
	"aethermeter/internal/config"
	"aethermeter/internal/engine"
)

type overlayOptions struct {
	Engine   *engine.Engine
	Config   config.Config
	StartErr string
	Banner   string
}

func runOverlay(o overlayOptions) error {
	if o.StartErr != "" {
		return fmt.Errorf("%s", o.StartErr)
	}
	for i := 0; i < 20; i++ {
		time.Sleep(time.Second)
		s := o.Engine.Tracker.Snapshot(combat.SnapshotOptions{Mode: combat.ViewMode(o.Config.Mode)})
		fmt.Print("\033[H\033[2J")
		fmt.Println(combat.Summary(s))
	}
	return nil
}

func singleInstance() bool  { return true }
func showFatal(text string) { fmt.Fprintln(os.Stderr, text) }
func recordingPath() string { return "sessao.pmrec" }

func preflight(f flags) bool { return !f.uninstall }
