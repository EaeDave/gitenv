//go:build !windows

package update

import (
	"fmt"
	"os"
	"syscall"
)

// swapBinary replaces the running executable. On Unix a running binary's inode
// stays valid after the file is renamed away, so an atomic rename over dest is
// safe and takes effect on the next launch.
func swapBinary(newPath, dest string) error {
	if err := os.Rename(newPath, dest); err != nil {
		return fmt.Errorf("install to %s: %w", dest, err)
	}
	return nil
}

func setExecutable(path string) error {
	return os.Chmod(path, 0o755)
}

// Restart replaces the current process image with the binary at path, keeping
// the same arguments and environment. It does not return on success.
func Restart(path string) error {
	args := append([]string{path}, os.Args[1:]...)
	return syscall.Exec(path, args, os.Environ())
}
