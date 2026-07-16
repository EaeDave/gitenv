//go:build windows

package update

import (
	"fmt"
	"os"
	"os/exec"
)

// swapBinary replaces the running executable on Windows, where an in-use .exe
// cannot be overwritten but can be renamed. Move the running binary aside, put
// the new one in place, then best-effort delete the old copy.
func swapBinary(newPath, dest string) error {
	old := dest + ".old"
	_ = os.Remove(old)
	if _, err := os.Stat(dest); err == nil {
		if err := os.Rename(dest, old); err != nil {
			return fmt.Errorf("could not move the running binary aside at %s (close gitenv and retry): %w", dest, err)
		}
	}
	if err := os.Rename(newPath, dest); err != nil {
		// Roll back so the current process still has a binary on disk.
		_ = os.Rename(old, dest)
		return fmt.Errorf("install to %s: %w", dest, err)
	}
	_ = os.Remove(old)
	return nil
}

func setExecutable(string) error { return nil }

// Restart launches the updated binary in a new process and exits the current
// one, because Windows cannot replace a running process image in place.
func Restart(path string) error {
	cmd := exec.Command(path, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to launch %s: %w", path, err)
	}
	os.Exit(0)
	return nil
}
