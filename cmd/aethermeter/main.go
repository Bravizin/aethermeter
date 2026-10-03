// AetherMeter — medidor de DPS de PT para Aion 2 (overlay).
//
// Lê passivamente os pacotes do jogo via Npcap; não injeta nada no cliente.
// O mesmo .exe também é o instalador (veja internal/setup).
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime/debug"

	"aethermeter/internal/applog"
	"aethermeter/internal/config"
	"aethermeter/internal/demo"
	"aethermeter/internal/engine"
)

var version = "0.2.2"

type flags struct {
	demo, record, portable, uninstall bool
	replay                            string
}

func main() {
	var f flags
	flag.BoolVar(&f.demo, "demo", false, "mostra uma luta simulada (para testar o overlay sem o jogo)")
	flag.StringVar(&f.replay, "replay", "", "reproduz uma gravação .pmrec")
	flag.BoolVar(&f.record, "record", false, "grava a sessão desde o início (diagnóstico)")
	flag.BoolVar(&f.portable, "portable", false, "não oferece instalação, só abre")
	flag.BoolVar(&f.uninstall, "uninstall", false, "remove o AetherMeter deste PC")
	flag.Parse()

	logPath := applog.Open()
	defer applog.Close()
	defer func() {
		if r := recover(); r != nil {
			applog.Printf("PANIC: %v\n%s", r, debug.Stack())
			showFatal(fmt.Sprintf("O AetherMeter travou: %v\n\nDetalhes em:\n%s", r, logPath))
			os.Exit(1)
		}
	}()
	applog.Printf("AetherMeter %s iniciando", version)

	// install / update / uninstall / Npcap — may decide we should exit now
	if !preflight(f) {
		return
	}

	if !singleInstance() {
		showFatal("O AetherMeter já está aberto.\nUse Ctrl+Alt+H para mostrar/esconder.")
		return
	}

	cfg := config.Load()
	eng := engine.New()
	eng.Log = applog.Printf

	opts := overlayOptions{Engine: eng, Config: cfg}
	switch {
	case f.demo:
		opts.Banner = "DEMO"
		go demo.Run(eng)
	case f.replay != "":
		opts.Banner = "REPLAY"
		go func() {
			if err := eng.Replay(f.replay, true); err != nil {
				applog.Printf("[replay] erro: %v", err)
			}
		}()
	default:
		if err := eng.StartCapture(); err != nil {
			applog.Printf("[captura] %v", err)
			opts.StartErr = err.Error()
		} else if f.record {
			if err := eng.StartRecording(recordingPath()); err != nil {
				applog.Printf("[gravação] %v", err)
			}
		}
	}

	if err := runOverlay(opts); err != nil {
		applog.Printf("overlay: %v", err)
		showFatal("Erro ao abrir o overlay: " + err.Error())
	}
	eng.StopCapture()
	applog.Printf("encerrado")
}
