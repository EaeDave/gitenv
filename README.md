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
- 🖥️ **New computer, one keystroke** — the project list shows everything in the vault, clones or finds a missing repository, then links and applies its env file.
- 🗺️ **Per-project env path** — `.env`, `.env.local`, or a monorepo path like `apps/web/.env`.
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

### Updating

gitenv can update itself from GitHub Releases:

```bash
gitenv update          # install the latest release if newer
gitenv update --force  # reinstall the latest release regardless
```

When launched, the TUI checks for a newer release in the background. If one is
found it updates in place and relaunches automatically; you can also press `U`
to update on demand. Set `GITENV_NO_UPDATE=1` to disable the automatic check.

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

Press `?` on any main screen for the complete keymap plus a glossary of the
terms and status labels — the list below is only the everyday subset.

Projects screen:

```text
↑↓/jk   move through the scrollable project list
/       fuzzy-search projects by name, path, or repository
tab     cycle all / modified / missing project filters
enter   open the project, adopt it, or add the current folder when prompted
a       add the current folder as a project
c       capture the .env into a profile
s       sync with the remote (contextual)
v       open the change viewer
f       find local clones of vault projects
o       project options (env file, line endings)
b       save your recovery key
d       devices and pending approvals
g       sync repository settings
?       full keymap and glossary
r       reload        q  quit
```

Devices screen: `enter` approves the selected request; `x` rejects and removes
it after confirmation. Rejection grants no access, and that computer may request
approval again later.

Profiles screen:

```text
enter   apply the selected profile
e       edit the project's .env (inline editor)
c       capture the active profile
n       create a new profile
d       remove an inactive profile
s       sync with the remote (contextual)
o       project options (env file, line endings)
v       open the change viewer
?       full keymap and glossary
p       browse all projects (when focused on one)
r       reload        esc/q  back
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

Mouse: hover rows and buttons for feedback, click to select or activate,
double-click a project/profile to open/apply it, and use the wheel to navigate
lists. Mouse support is additive: every action remains available from the
keyboard. Some terminals require holding `shift` while selecting terminal text
when mouse tracking is active.

Capture preview: `enter` or `y` confirms; `n` or `esc` cancels.
Inline editor: click to move the cursor, use the wheel to scroll, `ctrl+s`
save, `esc` cancel, and `enter` new line. Hold `shift` while dragging to
select terminal text.
Everywhere: `ctrl+c` quits, `U` installs an offered update.

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
gitenv link <project> <project-directory> [--env-file <relative/path>] [--line-endings <preserve|native|lf|crlf>]
gitenv set <project> --env-file <relative/path> | --line-endings <preserve|native|lf|crlf>
gitenv projects
gitenv adopt <project> [directory] [--profile <name>]
gitenv discover
gitenv capture <project> <profile>
gitenv switch <project> <profile> [--force]
gitenv status
gitenv pull
gitenv push
gitenv update [--force]
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
- **Setting up a new computer**: install `gitenv`, clone the vault, import your
  recovery identity. The project list then shows every project the vault holds,
  badged by state — `●` linked, `◍` a clone found on this machine, `◌` missing,
  `○` no repository recorded. Press `enter` on a missing project to clone it
  (recorded URL, then `gh`, then HTTPS) and press `d` to scan for clones you
  already have. Either way gitenv links the directory and applies the env file
  in one step.
- Clones are matched by their **origin remote**, never by folder name, so two
  unrelated directories named `api` are never confused. The scan is bounded to
  common development roots, skips build and cache trees, and is cached.

### Upgrading from 0.2.x

0.3.0 changes the vault layout: project metadata moves out of the plaintext
manifest into per-project encrypted files. The first 0.3.0 launch upgrades an
existing vault in place and leaves the change staged — publish it with `s` in the
TUI or `gitenv push`.

Do it on **one** computer, then `gitenv pull` on the others. The upgrade assigns
fresh random identifiers, so two computers upgrading independently produce two
different layouts of the same content; gitenv refuses to upgrade when it can see
that the vault remote is ahead, and pulling first turns the upgrade into a no-op.
Older gitenv binaries reject a 0.3.0 vault with a clear "unsupported vault
version" message rather than misreading it.

## Security

- Git stores only ciphertext, metadata, and wrapped key material — never plaintext secrets.
- Losing **both** your master password and recovery key makes profiles cryptographically unrecoverable, by design.
- The vault repository reveals no project names, profile names, repository URLs
  or env paths: that metadata is encrypted per project under random identifiers,
  so a reader of the repository learns only how many projects exist.
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

Para atualizar: `gitenv update` (ou `--force`). Ao abrir, a TUI checa uma
release mais nova em segundo plano e, se houver, se atualiza e reabre sozinha —
ou aperte `U`. Use `GITENV_NO_UPDATE=1` para desativar a checagem automática.

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
- **Máquina nova ou formatada**: instale o `gitenv`, clone o vault e importe sua
  recovery key. A lista passa a mostrar **todos** os projetos do vault, com
  badge de estado — `●` vinculado, `◍` clone encontrado nesta máquina, `◌` sem
  cópia local, `○` sem repositório registrado. `enter` num projeto `◌` clona
  (URL registrada, depois `gh`, depois HTTPS) e `d` procura clones que você já
  tem. Nos dois casos ele vincula a pasta e aplica o env file de uma vez.
- Clones são casados pelo **remoto origin**, nunca pelo nome da pasta, então
  duas pastas `api` sem relação jamais se confundem. A varredura é limitada às
  raízes comuns de desenvolvimento, pula árvores de build/cache e é cacheada.
- O env file é **por projeto**: `.env`, `.env.local` ou um caminho de monorepo
  como `apps/web/.env` (`gitenv set <projeto> --env-file ...`). A política de
  fim de linha também é por projeto, então capturar no Windows não leva CRLF
  para um checkout Linux.
- O repositório do vault não revela nomes de projeto, de perfil, URLs de
  repositório nem caminhos de env: esses metadados ficam criptografados por
  projeto sob identificadores aleatórios.

### Atualizando da 0.2.x

A 0.3.0 muda o layout do vault: os metadados de projeto saem do manifesto em
texto puro para arquivos criptografados por projeto. A primeira execução da
0.3.0 atualiza o vault existente e deixa a mudança pendente — publique com `s`
na TUI ou `gitenv push`.

Faça isso em **um** computador e depois rode `gitenv pull` nos outros. A
atualização sorteia identificadores novos, então dois computadores atualizando
de forma independente geram dois layouts diferentes do mesmo conteúdo; o gitenv
recusa atualizar quando percebe que o remoto do vault está à frente, e puxar
primeiro transforma a atualização em no-op. Binários antigos recusam um vault
0.3.0 com uma mensagem clara de "unsupported vault version".

### Segurança

O Git só armazena ciphertext, metadados e chaves embrulhadas. Perder **ao mesmo
tempo** a senha mestra e a recovery key torna os perfis irrecuperáveis, por
design — guarde a recovery key em local separado da máquina.
