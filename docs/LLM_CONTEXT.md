<!-- business-readme:context:start -->
# LLM Context

<!-- Keep only non-inferable context. Point to code/tests instead of duplicating behavior. -->

## Current business rule map

- Encrypted profile lifecycle, authenticated reads and byte preservation → `internal/vault/service.go`, `internal/vault/service_test.go`; value-free structural env comparison → `internal/envdiff/envdiff.go`, `internal/envdiff/envdiff_test.go`.
- Protected access, migration and identity selection → `internal/vault/access.go`, `internal/vault/crypto.go`, `internal/app/roadmap_test.go`.
- Device enrollment and recovery → `internal/vault/enrollment.go`, `internal/app/device.go`, `internal/vault/enrollment_test.go`.
- Vault remote management and repository identity → `internal/app/remote.go`, `internal/git/normalize.go`, focused tests beside them.
- Non-mutating Git snapshot inspection and sync inventory → `internal/git/sync.go`, `internal/git/revision.go`, `internal/app/sync_inventory.go`, focused tests beside them; value-free vault snapshot comparison → `internal/vault/diff.go`, `internal/vault/diff_test.go`.
- TUI access gates, automatic sync inventory and capture-preview workflows → `internal/tui/tui.go`, `internal/tui/keys.go`, `internal/tui/sync.go`, `internal/tui/sync_inventory_view.go`, `internal/tui/capture.go`; responsive rendering and visual contracts → `internal/tui/view.go`, `internal/tui/capture_view.go`, `internal/tui/theme.go`, `internal/tui/tui_test.go`.

## Non-inferable technical facts

- Tests that touch credentials must keep `keyring.MockInit()` in each relevant package; isolated config directories alone do not isolate an OS keychain.
- `GITENV_CONFIG_DIR` is also a keychain namespace boundary, not only a filesystem override.

## Conflicts and unknowns

None observed.

## Durable decisions and gotchas

- Never run identity-writing smoke tests against the default workstation keychain. Use an overridden config directory and confirm the package uses the mocked keyring in automated tests.
- Remote status checks use an eight-second deadline and only fetch refs; pull, push and `.env` application remain explicit user actions.
<!-- business-readme:context:end -->
