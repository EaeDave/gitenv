package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eaedave/gitenv/internal/app"
	gitops "github.com/eaedave/gitenv/internal/git"
	"github.com/eaedave/gitenv/internal/tui"
	"github.com/eaedave/gitenv/internal/update"
	"github.com/eaedave/gitenv/internal/vault"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "gitenv:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return runTUI()
	}
	switch args[0] {
	case "init":
		return initCommand(args[1:])
	case "clone":
		return cloneCommand(args[1:])
	case "identity":
		return identityCommand(args[1:])
	case "device":
		return deviceCommand(args[1:])
	case "link":
		return linkCommand(args[1:])
	case "projects":
		return projectsCommand()
	case "adopt":
		return adoptCommand(args[1:])
	case "discover":
		return discoverCommand()
	case "set":
		return setCommand(args[1:])
	case "capture":
		return captureCommand(args[1:])
	case "apply", "switch":
		return applyCommand(args[1:])
	case "status":
		return statusCommand()
	case "pull":
		return syncCommand(false)
	case "push":
		return syncCommand(true)
	case "tui":
		return runTUI()
	case "update", "--update":
		return updateCommand(args[1:])
	case "help", "--help", "-h":
		usage()
		return nil
	case "version", "--version":
		fmt.Println("gitenv " + version)
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runTUI() error {
	cfg, err := vault.LoadLocal()
	if err != nil {
		return err
	}
	if _, err := vault.ResetMissingVault(&cfg); err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	restart, err := tui.Run(&cfg, cwd, version)
	if err != nil {
		return err
	}
	if restart != "" {
		return update.Restart(restart)
	}
	return nil
}

func updateCommand(args []string) error {
	force := false
	for _, arg := range args {
		if arg == "--force" || arg == "-f" {
			force = true
		}
	}
	return update.RunCLI(version, force)
}

func usage() {
	fmt.Print(`gitenv — encrypted, Git-backed .env profiles

Usage:
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
  gitenv tui
  gitenv update [--force]
`)
}

func initCommand(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: gitenv init <vault-directory>")
	}
	root, err := filepath.Abs(args[0])
	if err != nil {
		return err
	}
	identity, err := vault.LoadIdentity()
	if err != nil {
		identity, err = vault.GenerateIdentity()
		if err != nil {
			return err
		}
		if err := vault.SaveIdentity(identity); err != nil {
			return err
		}
	}
	if err := vault.Init(root, identity.Recipient().String()); err != nil {
		return err
	}
	if err := gitops.Init(root); err != nil {
		return err
	}
	cfg := vault.LocalConfig{VaultPath: root, Projects: map[string]vault.LocalProject{}}
	if err := vault.SaveLocal(cfg); err != nil {
		return err
	}
	identityPath, _ := vault.IdentityPath()
	fmt.Printf("Vault initialized: %s\nIdentity: %s\nBack it up now: gitenv identity export <safe-file>\n", root, identityPath)
	return nil
}

func cloneCommand(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: gitenv clone <git-url> <vault-directory>")
	}
	root, err := filepath.Abs(args[1])
	if err != nil {
		return err
	}
	if err := gitops.Clone(context.Background(), args[0], root); err != nil {
		return err
	}
	if _, err := vault.LoadManifest(root); err != nil {
		return fmt.Errorf("cloned repository is not a gitenv vault: %w", err)
	}
	cfg, err := vault.LoadLocal()
	if err != nil {
		return err
	}
	cfg.VaultPath = root
	if err := vault.SaveLocal(cfg); err != nil {
		return err
	}
	fmt.Printf("Vault cloned: %s\nImport your recovery identity before applying profiles.\n", root)
	return nil
}

func identityCommand(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: gitenv identity export|import <file>")
	}
	identityPath, err := vault.IdentityPath()
	if err != nil {
		return err
	}
	target, err := filepath.Abs(args[1])
	if err != nil {
		return err
	}
	switch args[0] {
	case "export":
		if err := app.ExportIdentity(target); err != nil {
			return err
		}
		fmt.Printf("Recovery identity exported to %s; store it outside Git.\n", target)
		return nil
	case "import":
		if err := app.ImportIdentity(target); err != nil {
			return err
		}
		fmt.Printf("Identity imported into %s\n", identityPath)
		return nil
	default:
		return errors.New("usage: gitenv identity export|import <file>")
	}
}

func deviceCommand(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: gitenv device request <name>|approve <request-id>|activate <request-id>")
	}
	cfg, err := configured()
	if err != nil {
		return err
	}
	switch args[0] {
	case "request":
		request, err := app.RequestDeviceEnrollment(&cfg, args[1])
		if err != nil {
			return err
		}
		fmt.Printf("Enrollment requested and pushed: %s\nApprove it on an authorized device, then activate it here.\n", request.ID)
		return nil
	case "approve":
		if err := app.ApproveDeviceEnrollment(cfg, args[1]); err != nil {
			return err
		}
		fmt.Printf("Enrollment approved and pushed: %s\nThe new device can activate now.\n", args[1])
		return nil
	case "activate":
		if err := app.ActivateDeviceEnrollment(&cfg, args[1], vault.StoreIdentityKeychain); err != nil {
			return err
		}
		fmt.Printf("Device activated: %s\n", args[1])
		return nil
	default:
		return errors.New("usage: gitenv device request <name>|approve <request-id>|activate <request-id>")
	}
}

func linkCommand(args []string) error {
	envFile, args, hasEnvFile, err := extractFlag(args, "--env-file")
	if err != nil {
		return err
	}
	lineEndings, args, hasLineEndings, err := extractFlag(args, "--line-endings")
	if err != nil {
		return err
	}
	if err := rejectUnknownFlags(args); err != nil {
		return err
	}
	if len(args) != 2 {
		return errors.New("usage: gitenv link <project> <project-directory> [--env-file <relative/path>] [--line-endings <preserve|native|lf|crlf>]")
	}
	cfg, err := configured()
	if err != nil {
		return err
	}
	if err := vault.Link(&cfg, args[0], args[1]); err != nil {
		return err
	}
	if err := vault.SaveLocal(cfg); err != nil {
		return err
	}
	if hasEnvFile {
		if err := app.SetProjectEnvFile(cfg, args[0], envFile); err != nil {
			return err
		}
	}
	if hasLineEndings {
		policy, err := vault.ParseLineEndingPolicy(lineEndings)
		if err != nil {
			return err
		}
		if err := app.SetProjectLineEndings(cfg, args[0], policy); err != nil {
			return err
		}
	}
	// Record the directory's origin remote now, not at capture time: the
	// canonical identity is what lets another machine find or clone this
	// project, and a project linked without it stays undiscoverable.
	recorded, err := app.TryAttachRepository(cfg, args[0])
	if err != nil {
		return err
	}
	fmt.Printf("Linked %s -> %s\n", args[0], cfg.Projects[args[0]].Path)
	if !recorded {
		fmt.Println("No origin remote found; this project cannot be cloned on another computer until you add one and run gitenv link again.")
	}
	return nil
}

func captureCommand(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: gitenv capture <project> <profile>")
	}
	cfg, err := configured()
	if err != nil {
		return err
	}
	if err := vault.Capture(&cfg, args[0], args[1]); err != nil {
		return err
	}
	fmt.Printf("Captured %s/%s\n", args[0], args[1])
	return nil
}

func applyCommand(args []string) error {
	force := false
	values := make([]string, 0, 2)
	for _, arg := range args {
		if arg == "--force" {
			force = true
			continue
		}
		values = append(values, arg)
	}
	if len(values) != 2 {
		return errors.New("usage: gitenv switch <project> <profile> [--force]")
	}
	cfg, err := configured()
	if err != nil {
		return err
	}
	if err := vault.Apply(&cfg, values[0], values[1], force); err != nil {
		return err
	}
	fmt.Printf("Applied %s/%s\n", values[0], values[1])
	return nil
}

func statusCommand() error {
	cfg, err := configured()
	if err != nil {
		return err
	}
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(manifest.Projects)+len(cfg.Projects))
	seen := map[string]bool{}
	for name := range manifest.Projects {
		seen[name] = true
		names = append(names, name)
	}
	for name := range cfg.Projects {
		if !seen[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		state, err := vault.Status(cfg, name)
		if err != nil {
			return err
		}
		active := cfg.Projects[name].ActiveProfile
		if active == "" {
			active = "-"
		}
		fmt.Printf("%-24s %-16s %s\n", name, active, state)
	}
	gitStatus, err := gitops.Status(cfg.VaultPath)
	if err != nil {
		return err
	}
	if gitStatus == "" {
		gitStatus = "clean"
	}
	fmt.Printf("git: %s\n", gitStatus)
	return nil
}

func syncCommand(push bool) error {
	cfg, err := configured()
	if err != nil {
		return err
	}
	if push {
		if err := gitops.CommitAndPush(cfg.VaultPath, "gitenv: update encrypted profiles"); err != nil {
			return err
		}
		fmt.Println("Vault pushed")
		return nil
	}
	if err := gitops.Pull(cfg.VaultPath); err != nil {
		return err
	}
	fmt.Println("Vault updated; local .env files were not overwritten")
	return nil
}

func configured() (vault.LocalConfig, error) {
	cfg, err := vault.LoadLocal()
	if err != nil {
		return vault.LocalConfig{}, err
	}
	if cfg.VaultPath == "" {
		return vault.LocalConfig{}, errors.New("no vault configured; run gitenv init or gitenv clone")
	}
	// Upgrade a v2 vault to v3 before any manifest read, from whichever CLI
	// command resolved the vault. The rewrite dirties gitenv.json, so tell the
	// user to publish it.
	changed, err := app.EnsureVaultUpgraded(cfg)
	if err != nil {
		return vault.LocalConfig{}, err
	}
	if changed {
		fmt.Println("Vault upgraded to the latest format; run gitenv push to publish it.")
	}
	return cfg, nil
}

func projectsCommand() error {
	cfg, err := configured()
	if err != nil {
		return err
	}
	manifest, err := vault.LoadManifest(cfg.VaultPath)
	if err != nil {
		return err
	}
	statuses, err := collectStatuses(cfg, manifest)
	if err != nil {
		return err
	}
	for _, state := range app.ProjectStates(cfg, manifest, statuses) {
		// Show where the project lives locally, falling back to the recorded
		// repository identity for projects that have no clone on this machine.
		location := state.Path
		if location == "" {
			location = state.Identity
		}
		if location == "" {
			location = "-"
		}
		profile := state.ActiveProfile
		if profile == "" {
			profile = "-"
		}
		fmt.Printf("%-24s %-10s %-40s %s\n", state.Name, state.Kind, location, profile)
	}
	return nil
}

func adoptCommand(args []string) error {
	profile, args, _, err := extractFlag(args, "--profile")
	if err != nil {
		return err
	}
	if err := rejectUnknownFlags(args); err != nil {
		return err
	}
	if len(args) < 1 || len(args) > 2 {
		return errors.New("usage: gitenv adopt <project> [directory] [--profile <name>]")
	}
	cfg, err := configured()
	if err != nil {
		return err
	}
	name := args[0]
	if len(args) == 2 {
		if err := app.AdoptProject(&cfg, name, args[1], profile); err != nil {
			return err
		}
		fmt.Printf("Adopted %s -> %s\n", name, cfg.Projects[name].Path)
		return nil
	}
	dest := app.SuggestCloneDest(cfg, name)
	fmt.Printf("Cloning %s into %s\n", name, dest)
	// A CLI may legitimately block on the network, so no timeout is imposed.
	outcome, err := app.CloneAndAdopt(context.Background(), &cfg, name, dest, profile)
	if err != nil {
		return err
	}
	fmt.Printf("Adopted %s via %s (%s) -> %s\n", name, outcome.Method, outcome.URL, cfg.Projects[name].Path)
	return nil
}

func discoverCommand() error {
	cfg, err := configured()
	if err != nil {
		return err
	}
	found, err := app.RunDiscovery(context.Background(), &cfg)
	if err != nil {
		return err
	}
	identities := make([]string, 0, len(found))
	for identity := range found {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	total := 0
	for _, identity := range identities {
		fmt.Println(identity)
		for _, path := range found[identity] {
			fmt.Printf("  %s\n", path)
		}
		total += len(found[identity])
	}
	fmt.Printf("%d repositories found across %d identities; results are cached for the TUI.\n", total, len(identities))
	return nil
}

func setCommand(args []string) error {
	envFile, args, hasEnvFile, err := extractFlag(args, "--env-file")
	if err != nil {
		return err
	}
	lineEndings, args, hasLineEndings, err := extractFlag(args, "--line-endings")
	if err != nil {
		return err
	}
	if err := rejectUnknownFlags(args); err != nil {
		return err
	}
	if len(args) != 1 || (!hasEnvFile && !hasLineEndings) {
		return errors.New("usage: gitenv set <project> --env-file <relative/path> | --line-endings <preserve|native|lf|crlf>")
	}
	cfg, err := configured()
	if err != nil {
		return err
	}
	name := args[0]
	if hasEnvFile {
		if err := app.SetProjectEnvFile(cfg, name, envFile); err != nil {
			return err
		}
		fmt.Printf("Set env file for %s: %s\n", name, envFile)
	}
	if hasLineEndings {
		policy, err := vault.ParseLineEndingPolicy(lineEndings)
		if err != nil {
			return err
		}
		if err := app.SetProjectLineEndings(cfg, name, policy); err != nil {
			return err
		}
		fmt.Printf("Set line endings for %s: %s\n", name, policy)
	}
	return nil
}

// collectStatuses computes the vault.Status of every project the vault knows or
// this machine links, keyed by name for app.ProjectStates.
func collectStatuses(cfg vault.LocalConfig, manifest vault.Manifest) (map[string]string, error) {
	statuses := make(map[string]string, len(manifest.Projects)+len(cfg.Projects))
	record := func(name string) error {
		if _, ok := statuses[name]; ok {
			return nil
		}
		state, err := vault.Status(cfg, name)
		if err != nil {
			return err
		}
		statuses[name] = state
		return nil
	}
	for name := range manifest.Projects {
		if err := record(name); err != nil {
			return nil, err
		}
	}
	for name := range cfg.Projects {
		if err := record(name); err != nil {
			return nil, err
		}
	}
	return statuses, nil
}

// extractFlag pulls a "--name value" pair out of args, returning the value, the
// remaining args, and whether it was present. Flag parsing here is hand-rolled
// to stay dependency-free, mirroring applyCommand's --force scan.
func extractFlag(args []string, name string) (string, []string, bool, error) {
	rest := make([]string, 0, len(args))
	value := ""
	found := false
	for i := 0; i < len(args); i++ {
		if args[i] == name {
			if i+1 >= len(args) {
				return "", nil, false, fmt.Errorf("%s requires a value", name)
			}
			value = args[i+1]
			found = true
			i++
			continue
		}
		rest = append(rest, args[i])
	}
	return value, rest, found, nil
}

// rejectUnknownFlags fails on any leftover argument that looks like a flag, so a
// mistyped flag surfaces as a clear error instead of a silent positional.
func rejectUnknownFlags(args []string) error {
	for _, arg := range args {
		if strings.HasPrefix(arg, "--") {
			return fmt.Errorf("unknown flag %q", arg)
		}
	}
	return nil
}
