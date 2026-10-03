# Site do AetherMeter (GitHub Pages)

Site estático: HTML, CSS e JS num arquivo só, mais imagens e o ZIP. Não tem build.

```
docs/
  index.html                       a página
  favicon.ico, img/                ícones, prints do overlay, og.png (prévia no Discord/WhatsApp)
  downloads/AetherMeter-1.0.0.zip  o arquivo que o botão baixa
  .nojekyll                        faz o GitHub servir os arquivos como estão
```

## Publicar

1. Suba o projeto num repositório no GitHub, com esta pasta `docs/` junto.
2. No repositório, vá em **Settings → Pages**.
3. Em **Build and deployment**, escolha **Deploy from a branch**, branch `main` e pasta `/docs`, e salve.
4. Em um ou dois minutos o site fica no ar em `https://SEU-USUARIO.github.io/NOME-DO-REPO/`.

O link "código no GitHub" do rodapé é montado sozinho a partir do endereço do site.

## Lançar uma versão nova

1. Copie o zip novo para `downloads/` e apague o antigo.
2. No `index.html`, troque `1.0.0` pela versão nova (busca e substitui). Isso cobre os botões e o link do arquivo.
3. Atualize o SHA-256 que aparece embaixo do botão. No PowerShell:
   `Get-FileHash downloads\AetherMeter-X.Y.Z.zip`

Se preferir não guardar o zip no repositório, use o GitHub Releases. Anexe o zip num release e troque os links `downloads/AetherMeter-1.0.0.zip` por
`https://github.com/SEU-USUARIO/NOME-DO-REPO/releases/latest/download/AetherMeter.zip`, mantendo o nome do arquivo anexado sempre `AetherMeter.zip`.

## Prévia no Discord

Algumas plataformas só mostram a imagem de prévia se o endereço dela for completo. Depois de publicar, troque no `index.html`:

```html
<meta property="og:image" content="img/og.png">
```

por

```html
<meta property="og:image" content="https://SEU-USUARIO.github.io/NOME-DO-REPO/img/og.png">
```
