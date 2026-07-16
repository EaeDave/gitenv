<!-- business-readme:context:start -->
# LLM Context

<!-- Keep only non-inferable context. Point to code/tests instead of duplicating behavior. -->

## Current business rule map

- Encrypted profile lifecycle and byte preservation → `internal/vault/service.go`, `internal/vault/service_test.go`.
- Protected access, migration and identity selection → `internal/vault/access.go`, `internal/vault/crypto.go`, `internal/app/roadmap_test.go`.
- Device enrollment and recovery → `internal/vault/enrollment.go`, `internal/app/device.go`, `internal/vault/enrollment_test.go`.
- Vault remote management and repository identity → `internal/app/remote.go`, `internal/git/normalize.go`, focused tests beside them.
- TUI access gates and user workflows → `internal/tui/tui.go`, `internal/tui/tui_test.go`.

## Non-inferable technical facts

- Tests that touch credentials must keep `keyring.MockInit()` in each relevant package; isolated config directories alone do not isolate an OS keychain.
- `GITENV_CONFIG_DIR` is also a keychain namespace boundary, not only a filesystem override.

## Conflicts and unknowns

None observed.

## Durable decisions and gotchas

- Never run identity-writing smoke tests against the default workstation keychain. Use an overridden config directory and confirm the package uses the mocked keyring in automated tests.
<!-- business-readme:context:end -->
