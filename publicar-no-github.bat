@echo off
chcp 65001 >nul
setlocal EnableExtensions
title Publicar AetherMeter no GitHub
cd /d "%~dp0"

echo.
echo  === Publicar o AetherMeter no GitHub (repositorio + site + release) ===
echo.

rem ---- Git ----------------------------------------------------------------
where git >nul 2>&1
if errorlevel 1 (
  echo  [!] O Git nao esta instalado. Instalando pelo winget...
  winget install --id Git.Git -e --accept-source-agreements --accept-package-agreements
  set "PATH=%PATH%;%ProgramFiles%\Git\cmd"
)
where git >nul 2>&1 || (echo  [x] Nao achei o Git. Instale em https://git-scm.com e rode de novo. & pause & exit /b 1)

rem ---- GitHub CLI -----------------------------------------------------------
where gh >nul 2>&1
if errorlevel 1 (
  echo  [!] O GitHub CLI nao esta instalado. Instalando pelo winget...
  winget install --id GitHub.cli -e --accept-source-agreements --accept-package-agreements
  set "PATH=%PATH%;%ProgramFiles%\GitHub CLI"
)
where gh >nul 2>&1 || (echo  [x] Nao achei o GitHub CLI. Instale em https://cli.github.com e rode de novo. & pause & exit /b 1)

gh auth status >nul 2>&1
if errorlevel 1 (
  echo  Vai abrir o navegador pra voce entrar no GitHub. Siga as instrucoes.
  gh auth login --web --git-protocol https || (echo  [x] Login cancelado. & pause & exit /b 1)
)
for /f "delims=" %%u in ('gh api user --jq .login') do set "OWNER=%%u"
echo  Conta do GitHub: %OWNER%

rem ---- repositorio local ------------------------------------------------------
if not exist .git (
  git init -b main >nul
)
git config user.name >nul 2>&1 || git config user.name "%OWNER%"
git config user.email >nul 2>&1 || git config user.email "%OWNER%@users.noreply.github.com"
git add -A
git commit -m "AetherMeter 0.2.2 + site (GitHub Pages em /docs)" >nul 2>&1
git branch -M main

rem ---- repositorio no GitHub ----------------------------------------------------
gh repo view "%OWNER%/aethermeter" >nul 2>&1
if errorlevel 1 (
  echo  Criando o repositorio %OWNER%/aethermeter ...
  gh repo create "%OWNER%/aethermeter" --public --description "AetherMeter: DPS, cura e tank da PT para Aion 2 (overlay para Windows)" --homepage "https://%OWNER%.github.io/aethermeter/" || (echo  [x] Nao consegui criar o repositorio. & pause & exit /b 1)
)
git remote remove origin >nul 2>&1
git remote add origin "https://github.com/%OWNER%/aethermeter.git"
echo  Enviando os arquivos...
gh auth setup-git >nul 2>&1
git push -u origin main || (echo  [x] O envio falhou. & pause & exit /b 1)

rem ---- GitHub Pages (pasta /docs) -----------------------------------------------
echo  Ligando o GitHub Pages...
gh api -X POST "repos/%OWNER%/aethermeter/pages" -f "source[branch]=main" -f "source[path]=/docs" >nul 2>&1
gh api -X PUT "repos/%OWNER%/aethermeter/pages" -f "source[branch]=main" -f "source[path]=/docs" >nul 2>&1

rem ---- release com o zip -------------------------------------------------------------
gh release view v0.2.2 -R "%OWNER%/aethermeter" >nul 2>&1
if errorlevel 1 (
  echo  Criando o release v0.2.2...
  gh release create v0.2.2 "docs\downloads\AetherMeter-0.2.2.zip" -R "%OWNER%/aethermeter" --title "AetherMeter 0.2.2" --notes "Baixe o AetherMeter-0.2.2.zip, extraia e abra o AetherMeter.exe. Veja o site: https://%OWNER%.github.io/aethermeter/"
)

echo.
echo  Pronto!
echo   Repositorio: https://github.com/%OWNER%/aethermeter
echo   Site:        https://%OWNER%.github.io/aethermeter/   (pode levar 1-2 minutos pra aparecer)
echo.
start "" "https://github.com/%OWNER%/aethermeter"
pause
