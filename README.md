# gitenv

> Encrypted, Git-backed `.env` profiles with a terminal UI.

`gitenv` keeps your project `.env` files as **encrypted profiles** in a Git
repository and gives you a TUI to capture, switch, edit, and sync them across
machines. Secrets are encrypted with [age](https://age-encryption.org); Git
only ever stores ciphertext, so you can host the vault on any private
GitHub/GitLab/Gitea/self-hosted remote.

*[Versão em português mais abaixo.](#português-pt-br)*

## Features

- 🔐 **age-encrypted** profiles — Git sees only ciphertext, metadata, and wrapped keys.
- 🖥️ **TUI-first** workflow to capture, apply, edit, and sync `.env` files.
- 🗂️ **Multiple named profiles** per project (`dev`, `staging`, `prod`, …).
- 🧬 **Byte-for-byte capture** — comments, disabled lines, ordering, and line endings are preserved.
- 🔍 **Value-free diff viewer** with opt-in literal reveal (`x`), scoped to the current project.
- ✏️ **Built-in `.env` editor** with a live `git diff`-style view — no external editor required.
- 🔑 **Recoverable access** — master password, recovery key, or approval from an enrolled device.
- 💻 **Cross-platform** static binaries for Linux, macOS, and Windows.

## Install

### Linux & macOS

```bash
curl -fsSL https://raw.githubusercontent.com/EaeDave/gitenv/main/install.sh | bash
```

### Windows (PowerShell)

```powershell
irm https://raw.githubusercontent.com/EaeDave/gitenv/main/install.ps1 | iex
```

The installers detect your OS and architecture, download the matching static
binary from the latest GitHub release, verify its SHA-256 checksum, and install
to `~/.local/bin` (Linux/macOS) or `%LOCALAPPDATA%\gitenv\bin` (Windows). Set
`GITENV_VERSION` to pin a release and `GITENV_INSTALL_DIR` to change the target.

### From source

Requires Go 1.24+:

```bash
go install github.com/eaedave/gitenv/cmd/gitenv@latest
# or
go build -o gitenv ./cmd/gitenv
```

## Quick start

Run `gitenv` inside a project that has a `.env`:

```bash
cd ~/dev/my-api
gitenv
```

On first run the TUI walks you through:

1. creating a master-password-protected vault (or cloning an existing one);
2. configuring the vault's Git remote (optional);
3. unlocking with your master password, a pasted recovery key, or device approval;
4. linking the current project without overwriting its `.env`;
5. capturing an initial profile.

From then on, the main screen lets you apply, capture, create, and remove
profiles, sync with the remote, review changes, and edit `.env` files inline.

## TUI shortcuts

Profiles screen:

```text
enter   apply the selected profile
e       edit the project's .env (inline editor)
v       open the change viewer
s       sync with the remote (contextual)
c       capture the active profile
n       create a new profile
d       remove an inactive profile
r       reload status
p       browse all projects (when focused on one)
esc/q   back / quit
```

Change viewer:

```text
tab / shift+tab   select a project/profile
x                 reveal / hide literal values
e                 edit the selected .env
p / d             publish / discard the selected environment
↑↓ / jk           scroll        pgup/pgdn  page        home/end  jump
esc/q             back
```

Inline editor: `ctrl+s` save, `esc` cancel, `enter` new line.

## CLI

The TUI is the primary interface, but every core action is scriptable:

```text
gitenv init <vault-directory>
gitenv clone <git-url> <vault-directory>
gitenv identity export <backup-file>
gitenv identity import <backup-file>
gitenv device request <device-name>
gitenv device approve <request-id>
gitenv device activate <request-id>
gitenv link <project> <project-directory>
gitenv capture <project> <profile>
gitenv switch <project> <profile> [--force]
gitenv status
gitenv pull
gitenv push
gitenv version
```

## How it works

- Each profile is the encrypted snapshot of a `.env` at capture time. `capture`
  preserves the file byte-for-byte; `switch`/`apply` writes a profile back to the
  project's `.env`.
- The vault holds an age identity wrapped by your **master password**. The
  unlocked identity is cached in the OS keychain when available, with a
  restricted-permission local fallback.
- The **vault remote is independent** of your project remotes. `gitenv` never
  applies a profile or overwrites a `.env` during sync — pulling only updates the
  vault; applying is always an explicit action.
- A local `.env` with uncaptured changes is never silently overwritten: the CLI
  requires `--force`, the TUI requires explicit confirmation.
- Diffs are **value-free by default**. Literal values are only decrypted on an
  explicit reveal, kept in memory, and dropped when you leave the view.

## Security

- Git stores only ciphertext, metadata, and wrapped key material — never plaintext secrets.
- Losing **both** your master password and recovery key makes profiles cryptographically unrecoverable, by design.
- Keep your recovery key somewhere separate from the machine (password manager, offline copy).

## Development

```bash
go test ./...
go build ./...
```

Release binaries are built by `scripts/build-release.sh` and published as GitHub
release assets by `.github/workflows/release.yml` on `v*` tags.

---

## Português (pt-BR)

`gitenv` guarda seus arquivos `.env` como **perfis criptografados** num
repositório Git e oferece uma TUI para capturar, aplicar, editar e sincronizar
esses perfis entre computadores. Os segredos são criptografados com
[age](https://age-encryption.org); o Git só enxerga ciphertext, então o vault
pode ficar em qualquer remoto privado (GitHub, GitLab, Gitea, self-hosted).

### Instalar

Linux e macOS:

```bash
curl -fsSL https://raw.githubusercontent.com/EaeDave/gitenv/main/install.sh | bash
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/EaeDave/gitenv/main/install.ps1 | iex
```

Os instaladores detectam SO e arquitetura, baixam o binário estático da release
mais recente, verificam o checksum SHA-256 e instalam em `~/.local/bin` ou
`%LOCALAPPDATA%\gitenv\bin`. Use `GITENV_VERSION` para fixar uma versão e
`GITENV_INSTALL_DIR` para trocar o destino. Para compilar do fonte (Go 1.24+):
`go build -o gitenv ./cmd/gitenv`.

### Começando

Rode `gitenv` dentro de um projeto que já tenha `.env`:

```bash
cd ~/dev/minha-api
gitenv
```

No primeiro uso a TUI conduz: criar (ou clonar) um vault protegido por senha
mestra, configurar o remoto do vault, desbloquear (senha, recovery key ou
aprovação de dispositivo), vincular o projeto atual sem sobrescrever o `.env` e
capturar um perfil inicial. Depois é só aplicar, capturar, criar/remover perfis,
sincronizar, revisar mudanças e editar o `.env` inline.

### Como funciona

- Cada perfil é o snapshot criptografado de um `.env`. `capture` preserva o
  arquivo byte a byte; `switch`/`apply` grava um perfil de volta no `.env`.
- O vault guarda uma identidade age protegida pela **senha mestra**, cacheada no
  cofre de credenciais do SO quando disponível.
- O **remoto do vault é independente** dos remotos dos projetos. Sincronizar
  nunca aplica um perfil nem sobrescreve um `.env`; aplicar é sempre explícito.
- Um `.env` com mudanças não capturadas nunca é sobrescrito em silêncio.
- Diffs são **sem valores por padrão** — o plaintext só é descriptografado sob
  demanda (`x`), fica em memória e é descartado ao sair da tela.

### Segurança

O Git só armazena ciphertext, metadados e chaves embrulhadas. Perder **ao mesmo
tempo** a senha mestra e a recovery key torna os perfis irrecuperáveis, por
design — guarde a recovery key em local separado da máquina.
