//go:build windows

package main

import (
	"fmt"
	"path/filepath"
	"time"

	"aethermeter/internal/applog"
	"aethermeter/internal/overlay"
	"aethermeter/internal/setup"
)

type overlayOptions = overlay.Options

func runOverlay(o overlayOptions) error { return overlay.Run(o) }
func singleInstance() bool              { return overlay.SingleInstance() }
func showFatal(text string)             { overlay.ShowError("AetherMeter", text) }

func recordingPath() string {
	return filepath.Join(applog.Dir(), fmt.Sprintf("sessao-%s.pmrec", time.Now().Format("20060102-150405")))
}

// preflight handles installing, updating, uninstalling and Npcap.
// It returns false when the process should exit.
func preflight(f flags) bool {
	if f.uninstall {
		return uninstall()
	}

	devMode := f.demo || f.replay != ""
	if !f.portable && !devMode && !setup.RunningInstalled() {
		if !offerInstall() {
			return false
		}
		// offerInstall returns true only for "just run it this time"
	}

	if !devMode && !setup.NpcapInstalled() {
		ans := setup.Ask("Falta só uma coisa: o Npcap.\n\n"+
			"É o driver gratuito que deixa o AetherMeter ler o tráfego do jogo "+
			"(o mesmo usado pelo Wireshark). Ele só lê, não mexe em nada.\n\n"+
			"Baixar e instalar agora?\n"+
			"(o instalador do Npcap vai pedir permissão de administrador — pode deixar as opções padrão e clicar em Install/Next)",
			setup.MBYesNo|setup.MBIconQuest)
		if ans == setup.IDYes {
			if err := setup.InstallNpcap(); err != nil {
				applog.Printf("[npcap] %v", err)
				setup.Ask("Não deu para instalar o Npcap automaticamente:\n"+err.Error()+
					"\n\nVocê pode baixar em https://npcap.com e abrir o AetherMeter de novo.", setup.MBOk|setup.MBIconWarn)
			} else {
				applog.Printf("[npcap] instalado")
			}
		}
	}
	return true
}

// offerInstall returns true to continue running this (portable) copy.
func offerInstall() bool {
	var text string
	if setup.IsInstalled() {
		text = "Já existe um AetherMeter instalado neste PC.\n\n" +
			"Sim  →  atualizar a instalação com esta versão (" + version + ")\n" +
			"Não  →  só abrir esta cópia agora\n" +
			"Cancelar  →  sair"
	} else {
		text = "Bem-vindo ao AetherMeter — medidor de DPS de PT para Aion 2!\n\n" +
			"Instalar neste PC? Não precisa de administrador:\n" +
			"  • copia para " + setup.InstallDir() + "\n" +
			"  • cria atalhos na Área de Trabalho e no Menu Iniciar\n" +
			"  • aparece em Configurações → Apps (para desinstalar)\n\n" +
			"Sim  →  instalar\n" +
			"Não  →  só abrir agora, sem instalar\n" +
			"Cancelar  →  sair"
	}
	switch setup.Ask(text, setup.MBYesNoCancel|setup.MBIconQuest) {
	case setup.IDYes:
		if setup.InstanceRunning() {
			setup.Ask("O AetherMeter está aberto. Feche ele (botão direito no overlay → Sair) e rode o instalador de novo.", setup.MBOk|setup.MBIconWarn)
			return false
		}
		if err := setup.Install(version); err != nil {
			applog.Printf("[instalação] %v", err)
			setup.Ask("Problema na instalação:\n"+err.Error(), setup.MBOk|setup.MBIconError)
			if !setup.IsInstalled() {
				return false
			}
		}
		applog.Printf("[instalação] ok em %s", setup.InstallDir())
		setup.Ask("Pronto! O AetherMeter foi instalado.\n\n"+
			"Dica: deixe o Aion 2 em modo janela sem bordas para o overlay aparecer por cima.\n"+
			"Ctrl+Alt+L trava/destrava o overlay. Botão direito no topo dele abre o menu.\n\n"+
			"Pode apagar este arquivo baixado — use o atalho da Área de Trabalho daqui pra frente.",
			setup.MBOk|setup.MBIconInfo)
		if err := setup.Launch(setup.InstalledExe()); err != nil {
			setup.Ask("Instalado, mas não consegui abrir: "+err.Error(), setup.MBOk|setup.MBIconWarn)
		}
		return false
	case setup.IDNo:
		return true
	default:
		return false
	}
}

func uninstall() bool {
	if setup.Ask("Desinstalar o AetherMeter deste PC?", setup.MBYesNo|setup.MBIconQuest) != setup.IDYes {
		return false
	}
	if setup.InstanceRunning() {
		setup.Ask("Feche o AetherMeter primeiro (botão direito no overlay → Sair) e tente de novo.", setup.MBOk|setup.MBIconWarn)
		return false
	}
	removeData := setup.Ask("Apagar também as configurações, logs e gravações?\n("+applog.Dir()+")",
		setup.MBYesNo|setup.MBIconQuest) == setup.IDYes
	applog.Close()
	setup.Uninstall(applog.Dir(), removeData)
	setup.Ask("AetherMeter desinstalado.\n\n(O Npcap continua instalado; se quiser, remova em Configurações → Apps.)", setup.MBOk|setup.MBIconInfo)
	return false
}
