# Assinatura e notarização dos binários macOS

Sem isso, o macOS Gatekeeper bloqueia o binário baixado com "Apple não pode
verificar que este app está livre de malware". A release (`.github/workflows/release.yml`
+ `.goreleaser.yaml`) já está pronta para assinar e notarizar automaticamente
assim que os 5 secrets abaixo existirem no repo — até lá, é um no-op: a
release continua saindo normal, só sem assinatura.

Usa [`anchore/quill`](https://github.com/anchore/quill) via o suporte
nativo do GoReleaser (`notarize:` no `.goreleaser.yaml`) — cross-platform,
roda no `ubuntu-latest` que o job `binaries` já usa, sem precisar de runner
macOS.

**Pré-requisito**: conta paga no [Apple Developer Program](https://developer.apple.com/programs/)
(US$99/ano). Sem isso não tem como gerar os artefatos abaixo — é a Apple
quem assina/notariza, não o GoReleaser.

## 1. Certificado "Developer ID Application" (assinatura)

1. No Keychain Access (app nativo do macOS): menu **Certificate Assistant**
   → **Request a Certificate From a Certificate Authority…** → salva um
   `.certSigningRequest`.
2. Em [developer.apple.com/account/resources/certificates](https://developer.apple.com/account/resources/certificates)
   → **+** → **Developer ID Application** (não confundir com "Apple
   Distribution", que é para App Store) → sobe o CSR → baixa o `.cer`
   gerado → dá duplo-clique nele para instalar no Keychain.
3. No Keychain Access, ache o certificado (categoria "My Certificates"),
   expanda a seta pra ver a chave privada associada, selecione os dois
   (certificado + chave) → botão direito → **Export 2 items…** → salva
   como `Certificates.p12`, define uma senha de exportação — isso vira o
   `MACOS_SIGN_PASSWORD`.
4. `base64 -i Certificates.p12 -o cert.b64`

## 2. Chave de API do App Store Connect (notarização)

1. Em [appstoreconnect.apple.com/access/api](https://appstoreconnect.apple.com/access/api)
   → aba **Team Keys** → **+**. O papel **Developer** já é suficiente
   (não precisa ser Admin).
2. Baixa o `.p8` **na hora** — a Apple só deixa baixar uma vez.
3. Anota o **Key ID** (aparece na lista e no nome do arquivo
   `AuthKey_<KEYID>.p8`) e o **Issuer ID** (UUID no topo da página).
4. `base64 -i AuthKey_XXXXXXXXXX.p8 -o key.b64`

## 3. Configurar os secrets do repo

Rode no seu terminal (assim o conteúdo sensível nunca passa por um chat ou
PR):

```bash
gh secret set MACOS_SIGN_P12 < cert.b64
gh secret set MACOS_SIGN_PASSWORD --body "SUA_SENHA_DE_EXPORT"
gh secret set MACOS_NOTARY_KEY < key.b64
gh secret set MACOS_NOTARY_KEY_ID --body "SEU_KEY_ID"
gh secret set MACOS_NOTARY_ISSUER_ID --body "SEU_ISSUER_ID"
```

## Verificação

1. Disparar uma release de teste (`vX.Y.Z-rcN`) e conferir no log do job
   `binaries` que a etapa de notarização do GoReleaser rodou sem erro.
2. Baixar o `.tar.gz` **pelo navegador** num Mac (só assim ele ganha o
   atributo de quarentena de verdade) e confirmar que o binário abre sem
   o aviso do Gatekeeper.
3. Ou, sem precisar baixar de novo: `codesign -dv --verbose=4
   ./netsk8-navigator` deve mostrar `Authority=Developer ID Application: ...`
   e `Timestamp=...`.

**Nota**: `spctl -a -vvv --type execute` costuma responder `rejected (the
code is valid but does not seem to be an app)` para este binário — isso é
esperado e não indica falha. Esse assessment do `spctl` foi desenhado para
bundles `.app`, não para executáveis Unix soltos. Da mesma forma, dar
**duplo-clique no binário pelo Finder** pode disparar o aviso de malware do
Gatekeeper mesmo com tudo assinado/notarizado corretamente — binários soltos
(diferente de `.app`/`.pkg`/`.dmg`) não suportam "staple" do ticket de
notarização, e o Finder lida mal com a execução direta de um Mach-O sem
bundle. **O jeito certo de rodar é pelo Terminal** (`chmod +x
netsk8-navigator && ./netsk8-navigator`) — nesse caminho o Gatekeeper valida
o ticket online normalmente e libera sem aviso algum.

## App bundle (.dmg) para duplo-clique

Os binários crus assinados/notarizados acima funcionam bem via Terminal, mas
**não podem receber "staple"** do ticket de notarização (isso só existe para
`.app`/`.pkg`/`.dmg`) — então dar duplo-clique neles no Finder ainda pode
disparar o aviso do Gatekeeper. Pra resolver isso de vez, o job
`macos-app-bundle` (`.github/workflows/release.yml`, roda em `macos-latest`
— grátis pra repositórios públicos) empacota os binários darwin num `.app`
de verdade:

1. Funde os binários amd64+arm64 num binário universal (`lipo`).
2. Monta `Netsk8 Navigator.app` via `packaging/macos/build-app.sh` — ícone
   gerado a partir do `logo.png` da raiz (`sips`+`iconutil`, nativos do
   macOS), `Info.plist`, e um launcher (`packaging/macos/launcher.sh`) que
   abre uma janela do Terminal com os logs + o navegador na UI.
3. Assina o bundle inteiro com `codesign` de verdade (não o `quill` usado
   acima — `quill` só assina binários soltos, não sabe gerar o selo de
   bundle `_CodeSignature/CodeResources` que cobre `Info.plist`+
   `Resources/`, e esse selo só o `codesign` real sabe gerar, daí precisar
   de um runner macOS pra esse job específico).
4. Notariza (`notarytool submit --wait`) e dá staple (`stapler staple`) no
   `.app` — com o ticket staplado, o Gatekeeper valida offline, sem
   depender de internet no Mac do usuário.
5. Empacota num `.dmg` (`packaging/macos/make-dmg.sh` — `.app` + atalho pra
   `/Applications`) e anexa à mesma release do GitHub.

**Reaproveita os mesmos 5 secrets** já configurados acima — nenhum secret
novo é necessário.

Pra reproduzir localmente (útil pra debugar sem esperar o CI), com a
identidade já instalada no seu Keychain:

```bash
./packaging/macos/build-app.sh \
  /caminho/pro/netsk8-navigator \
  0.0.2-teste \
  ./dist-app \
  "Developer ID Application: Seu Nome (TEUTEUTEUTE)"

ditto -c -k --keepParent "./dist-app/Netsk8 Navigator.app" ./dist-app/app.zip
xcrun notarytool submit ./dist-app/app.zip \
  --key AuthKey_XXXXXXXXXX.p8 --key-id SEU_KEY_ID --issuer SEU_ISSUER_ID --wait
xcrun stapler staple "./dist-app/Netsk8 Navigator.app"

./packaging/macos/make-dmg.sh "./dist-app/Netsk8 Navigator.app" \
  ./dist-app/netsk8-navigator.dmg "Netsk8 Navigator"
```

## Rotação / expiração

O certificado "Developer ID Application" dura 5 anos; a chave de API do
App Store Connect não expira sozinha, mas pode ser revogada manualmente.
Se precisar trocar qualquer um, repita os passos acima e rode `gh secret
set` de novo — sobrescreve o valor existente.
