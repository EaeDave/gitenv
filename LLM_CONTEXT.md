<!-- business-readme:context:start -->
# LLM Context

## Current business rule map

- Vault/project/profile model: `internal/vault/model.go`.
- Local paths versus synchronized manifest: `internal/vault/store.go`.
- Capture, apply protection and byte-preserving behavior: `internal/vault/service.go`, defended by `service_test.go`.
- Age identity, encryption and decryption: `internal/vault/crypto.go`.
- Shared CLI/TUI workflows and current-project detection: `internal/app/app.go`, defended by `app_test.go`.
- Fast-forward-only Git synchronization: `internal/git/git.go`, defended by `git_test.go`.
- TUI-first onboarding, project/profile management, sync, remote/recovery flows and overwrite confirmation: `internal/tui/tui.go`, defended by `tui_test.go`.
- Public command contract: `cmd/gitenv/main.go`.

## Technical map for future LLMs

- Entry point: `cmd/gitenv/main.go`.
- Build: `go build -o gitenv ./cmd/gitenv`.
- Test: `go test ./...`.
- Config location: OS user config directory under `gitenv`; override with `GITENV_CONFIG_DIR` for isolated execution/tests.
- The vault contains `gitenv.json` and `projects/<project>/<profile>.env.age`.
- Never parse/reformat plaintext `.env` during capture/apply; whole-document age encryption preserves exact bytes.
- Git is invoked directly without a shell and terminal credential prompts are disabled for deterministic failures.

## Conflicts and unknowns

- OS credential-store integration is not implemented; identity is currently a permission-restricted file plus explicit export/import recovery.
- Multi-recipient device enrollment, semantic env diff, conflict merge and packaged releases remain outside this MVP.

## History

- 2026-07-15: Defined MVP rules and implemented encrypted profiles, local project mappings, safe switching, Git synchronization, recovery import/export and operational TUI. Sources inspected: product discussion, Dotenvx, SOPS, age, Infisical, git-env-vault and envault documentation; implementation and tests listed above.
- 2026-07-15: Standardized the canonical product name as `gitenv`, including module/import path, command directory, binary, config namespace, vault manifest and documentation. No compatibility aliases retained because the project is pre-release.
- 2026-07-15: Promoted the TUI to the primary interface. Added create/clone onboarding, Git-root `.env` detection, contextual project registration, profile creation, pull/push, remote setup and recovery export. Verified through model tests and a real PTY flow against a bare Git remote.
- 2026-07-15: Hardened new-computer onboarding: when the vault already contains a project, the TUI suggests its name/profile and links plus applies the encrypted profile instead of recapturing the local `.env`. Verified the clone remained Git-clean.
- 2026-07-15: Fixed stale local config after a vault is deleted/unmounted. TUI now treats a missing `gitenv.json` as unconfigured for the current session and opens onboarding without persisting removal of the original path.
- 2026-07-15: Added TUI profile removal with explicit confirmation, active-profile protection, manifest/file rollback safety and cursor clamping after list shrink. Verified in tests and a real PTY by cancelling once, deleting the last inactive profile, then safely rejecting deletion of the remaining active profile.
<!-- business-readme:context:end -->
