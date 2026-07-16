package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

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
  gitenv link <project> <project-directory>
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
	if err := gitops.Clone(args[0], root); err != nil {
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
	if len(args) != 2 {
		return errors.New("usage: gitenv link <project> <project-directory>")
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
	fmt.Printf("Linked %s -> %s\n", args[0], cfg.Projects[args[0]].Path)
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
	return cfg, nil
}
