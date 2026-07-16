<!-- clean-code-agents:start -->
## Agent rules — non-inferable project facts only

### Commands
- Targeted TUI tests: `go test ./internal/tui`
- Full test suite: `go test ./...`
- Static analysis: `go vet ./...`
- Build: `go build ./...`
- Format changed Go files: `gofmt -w <files>`

### Non-standard practices and failure points
- Credential tests must call `keyring.MockInit()` in each relevant package; an isolated config directory does not isolate the OS keychain.
- `GITENV_CONFIG_DIR` namespaces both local files and keychain entries. Never run identity-writing smoke tests against the default workstation config.
<!-- clean-code-agents:end -->
