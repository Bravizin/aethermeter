# AetherMeter — DPS, cura e tank da PT para Aion 2

Overlay leve que mostra em tempo real quanto cada membro da PT está causando de dano, curando e tomando de dano. É um único `.exe` de ~8 MB, que também é o instalador, e não precisa de runtime.

## Como funciona

O AetherMeter **só lê** o tráfego de rede que o jogo recebe, via Npcap (o mesmo driver do Wireshark). Ele não injeta código, não lê a memória do jogo e não envia nada ao servidor.

> ⚠️ Mesmo sendo passivo, é uma ferramenta de terceiros. A NCSoft não aprovou ferramentas assim. Use por sua conta e risco.

## Instalação (usuário)

1. Extraia o `AetherMeter-0.2.2.zip` e abra o `AetherMeter.exe`.
2. Clique em **Sim** para instalar. Não pede administrador. O programa:
   - vai para `%LOCALAPPDATA%\Programs\AetherMeter`;
   - cria atalhos na Área de Trabalho e no Menu Iniciar (inclusive "demonstração" e "desinstalar");
   - aparece em **Configurações → Apps**.

   Se o PartyMeter (nome antigo) estiver instalado, ele é removido e as configurações são levadas para o AetherMeter.
3. Se o **Npcap** não estiver instalado, o AetherMeter oferece baixar e abrir o instalador oficial. Pode deixar as opções padrão.
4. Deixe o Aion 2 em **janela sem bordas** (*borderless*). Em tela cheia exclusiva, o overlay não aparece por cima.

Para atualizar, abra o `.exe` da versão nova e escolha **Sim**. Para usar sem instalar, escolha **Não** ou rode com `-portable`.

Para testar sem o jogo, use o atalho **AetherMeter (demonstração)** ou rode `AetherMeter.exe -demo`. Ele simula uma luta de chefe com dano, cura e tank.

## O overlay

- **Abas no topo:** ⚔ dano (DPS), ♥ cura (HPS) e 🛡 dano recebido (tank). Elas continuam clicáveis mesmo com o overlay travado.
- **Vida do chefe:** número cheio com percentual, por exemplo `1.950.000 / 3.200.000 · 60,9%`.
- **Cada jogador:** ícone da classe, nome, valor por segundo, total e % da PT. A aba de dano mostra também o crítico; a aba tank, o maior golpe e as mortes.
- **Tempo de DG** no canto, quando o jogo informa a dungeon pelo pacote da PT. Fica azul enquanto corre e verde quando para: ao matar um chefe, ao sair da DG ou depois de 3 min sem luta. Se a PT lutar de novo na mesma run, ele volta a contar.
- **Skills:** clique num jogador para abrir um painel ao lado, à direita, ou à esquerda se não couber na tela. A lista continua visível. Clique no mesmo jogador ou no painel para fechar; clique em outro para trocar.
- **Lutas anteriores:** use a roda do mouse sobre o overlay. As últimas 20 ficam guardadas.
- **Menu:** botão direito no topo do overlay (opacidade, tamanho, modo, diagnóstico, atalhos…).

### Atalhos (sem Shift, para não atrapalhar o dash)

| Ação | Padrão |
|---|---|
| Travar/destravar (clique passa direto para o jogo) | `Ctrl+Alt+L` |
| Trocar de aba (DPS → cura → tank) | `Ctrl+Alt+T` |
| Zerar | `Ctrl+Alt+R` |
| Copiar resumo para o Discord | `Ctrl+Alt+C` |
| Dano: só no chefe / tudo da luta | `Ctrl+Alt+M` |
| Mostrar só a PT | `Ctrl+Alt+P` |
| Mostrar/esconder | `Ctrl+Alt+H` |

Para trocar um atalho, use **Menu → Ferramentas → Editar atalhos**. Ele abre o `config.json`, onde dá para pôr, por exemplo, `"lock": "F9"` ou `"tab": "Alt+F10"`. Use `"none"` para desativar um atalho. A mudança vale na próxima vez que o programa for aberto.

### Jogando solo

O servidor só manda os nomes dos personagens ao teleportar ou entrar numa DG. Para saber qual jogador é você mesmo assim, o AetherMeter cruza o dano de cada um com os cooldowns das suas próprias skills, que só o seu cliente recebe. Depois de 3 ou 4 skills, ele já sabe quem é você e lembra o nome do seu personagem nas próximas vezes.

Com **"Mostrar só a minha PT"** ligado:

- **Em PT:** aparecem só os membros.
- **Solo:** aparece só você (no canto fica escrito `solo`).
- **Antes de te reconhecer:** aparecem todos, com `PT?` no canto.

### Sobre cura e tank

- **Tank:** conta todo dano de monstro em jogadores conhecidos. Dano PvP também entra.
- **Cura:** reconhece as skills de cura do Clérigo, do Chanter e do Elementalista. É a parte mais nova; se alguma cura não aparecer, grave uma sessão (Menu → Ferramentas → Gravar sessão) para ajustar.

## Quando um patch quebrar

1. Abra **Menu → Ferramentas → Gravar sessão** e lute um pouco.
2. Desligue a gravação. O `.pmrec` fica na pasta de logs (`%APPDATA%\AetherMeter`).
3. Reproduza com `AetherMeter.exe -replay arquivo.pmrec` para depurar sem o jogo.

## Compilar

Precisa só do Go 1.24 ou mais novo, sem dependências externas nem cgo.

```
go test ./...
build.bat
```

### Estrutura

```
cmd/aethermeter    main, instalação/Npcap (preflight)
internal/netcap    Npcap (wpcap.dll via syscall), IPv4/IPv6/TCP, remontagem, detecção do jogo
internal/proto     framing, containers LZ4, decodificação dos opcodes
internal/combat    lutas, DPS/HPS/DTPS, skills, PT, invocações, tempo de DG
internal/ui        desenho do overlay (formas com anti-aliasing + ícones), independente de plataforma
internal/glyph     ícones de classe e das abas (arte original, gerada por assets/make_glyphs.py)
internal/overlay   janelas Win32 (cabeçalho sempre clicável + corpo que deixa o clique passar), texto GDI
internal/setup     instalador embutido: atalhos, Apps & features, Npcap, migração do PartyMeter
internal/demo      luta simulada (-demo)
assets/            ícone do app, manifesto, gerador de ícones
```

Ícone e manifesto: `rsrc -manifest assets/aethermeter.manifest -ico assets/appicon.ico -o cmd/aethermeter/rsrc_windows_amd64.syso`

## Créditos e licença

O formato dos pacotes e as tabelas de dados vêm do trabalho da comunidade no [RATmeter (Kuroukihime/AIon2-Dps-Meter)](https://github.com/Kuroukihime/AIon2-Dps-Meter), licenciado sob GPL-3.0. Por isso, este projeto também é distribuído sob **GPL-3.0** (veja `LICENSE`). Os ícones são originais e não usam arte do jogo.
