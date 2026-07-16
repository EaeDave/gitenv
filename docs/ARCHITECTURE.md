# Architecture & contributor notes

A pointer map to where behavior lives, plus non-obvious facts worth knowing
before changing things. Points at code/tests instead of duplicating behavior.

## Module map

- Encrypted profile lifecycle, authenticated reads, byte preservation → `internal/vault/service.go` (+ `service_test.go`); value-free structural env comparison → `internal/envdiff/envdiff.go` (+ `envdiff_test.go`).
- Protected access, migration, identity selection → `internal/vault/access.go`, `internal/vault/crypto.go`, `internal/app/roadmap_test.go`.
- Device enrollment and recovery → `internal/vault/enrollment.go`, `internal/app/device.go`, `internal/vault/enrollment_test.go`.
- Vault remote management and repository identity → `internal/app/remote.go`, `internal/git/normalize.go` (+ tests beside them).
- Non-mutating Git snapshot inspection and sync inventory → `internal/git/sync.go`, `internal/git/revision.go`, `internal/app/sync_inventory.go`; value-free vault snapshot comparison → `internal/vault/diff.go` (+ `diff_test.go`).
- TUI (access gates, sync inventory, diff viewer, per-environment publish/discard, inline `.env` editor, capture preview) → `internal/tui/tui.go`, `internal/tui/keys.go`, `internal/tui/sync.go`, `internal/tui/sync_inventory_view.go`, `internal/tui/sync_diff_view.go`, `internal/tui/sync_diff_actions.go`, `internal/tui/editor.go`, `internal/tui/editor_view.go`, `internal/tui/capture.go`; per-environment publish/discard orchestration → `internal/app/diff_actions.go`; local `.env` read/write for the editor → `internal/app/editor.go`; rendering/visual contracts → `internal/tui/view.go`, `internal/tui/capture_view.go`, `internal/tui/theme.go` (+ `tui_test.go`, `editor_test.go`, `focus_test.go`).
- Cross-platform build & distribution → `scripts/build-release.sh`, `.github/workflows/release.yml`, `install.sh`, `install.ps1`.
- Self-update → `internal/update/` (`update.go` shared logic; `update_unix.go`/`update_windows.go` platform replace + relaunch). `LatestTag` reads the `releases/latest` redirect (no GitHub API, no rate limit); `Apply` downloads the platform asset, verifies its SHA-256 against `checksums.txt`, and replaces the running binary in place (Unix rename-over-inode; Windows rename-aside dance). Unix relaunch uses `syscall.Exec` (same PID); Windows spawns a new process and exits. The TUI checks in the background on launch (`checkUpdateCmd`), auto-updates on an idle landing screen (`canAutoUpdate`) or via `U`, and returns a restart path from `tui.Run` that `main` hands to `update.Restart`. `GITENV_NO_UPDATE` opts out; dev builds (unparseable version) never self-update.

## Non-obvious facts

- Tests that touch credentials must call `keyring.MockInit()` in each relevant package; an isolated config directory alone does not isolate an OS keychain.
- `GITENV_CONFIG_DIR` is also a keychain namespace boundary, not only a filesystem override.

## Decisions & gotchas

- Never run identity-writing smoke tests against the default workstation keychain. Use an overridden config directory and confirm the package uses the mocked keyring in automated tests.
- Remote status checks use an eight-second deadline and only fetch refs; pull, push, and `.env` application remain explicit user actions.
- Literal values in the TUI diff are opt-in via `x`; plaintext is loaded asynchronously only for the reveal and must be dropped when hidden or when leaving the viewer.
- Publishing one environment from the diff viewer fails closed unless the vault is already synchronized and clean, so a single-profile capture never rides along with unrelated staged vault changes.
- The in-TUI `.env` editor uses `bubbles/textarea`, whose sanitizer rewrites tabs, drops control characters, and collapses lone carriage returns. `inlineEditableEnv` refuses tabs/control/lone-CR/non-UTF-8 up front, and the buffer is reconstructed with the original newline style plus trailing-newline state, so an unedited round-trip is byte-identical. The editor shows a live `git diff`-style literal diff (`envdiff.CompareLines`) of the buffer against the active profile decrypted at open (`app.ReadActiveProfileEnv`); that baseline plaintext lives only in model memory and is dropped on close.
- Per-profile status (`vault.ProfileStatuses`) compares the live `.env` checksum against each stored profile: only the active profile is ever `modified`; inactive profiles are `current` when the on-disk bytes match their snapshot, otherwise unmarked. "Both modified" is unreachable because one local `.env` maps to the active profile.
- Current-project focus mode: launched inside a linked project, the TUI lands on that project's profiles screen and hides the multi-project list until `p` unlocks browsing (`m.browseProjects`). The one-shot landing is gated by `m.landed` in the `reloadMsg` handler so migration/unlock defer it (`isFocusedProject`, `focus_test.go`).
- Diff viewer scope: `syncDiffScope()` returns the current project when `isFocusedProject()`, else "" (all). It filters the value-free inventory at render and is threaded through `revealSyncDiffCmd` → `app.RevealSyncLineDiff(scope)` → `vault.CompareVaultSnapshotLines(scope)`, so a focused reveal only decrypts the scoped project. `syncDiffReturn`/`syncDiffReturnScreen()` send esc and post-action back to the origin screen (profiles vs project list).
- Distribution: `scripts/build-release.sh` cross-compiles static binaries (`CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w -X main.version=<tag>"`) named `gitenv_<os>_<arch>[.exe]` for linux/darwin/windows × amd64/arm64 into `dist/` plus `checksums.txt`. `.github/workflows/release.yml` publishes them as release assets on `v*` tags. Installers fetch from `releases/latest/download` (or `GITENV_VERSION`) and verify the SHA-256 checksum. `main.version` is the single injected version string.
- `gitops.CommitAndPush` commits vault changes with an explicit identity (`-c user.name=gitenv -c user.email=gitenv@localhost`) so publishing never depends on the user's global git config (CI has none). Tests that drive their own git repos set identity via their `runGit`/`commitVault` helpers.
