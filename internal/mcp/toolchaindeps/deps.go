// Package toolchaindeps provides shared dependency-install argv selection for
// first-party toolchain MCP servers operating on go-orca workflow workspaces.
package toolchaindeps

import (
	"os"
	"path/filepath"
)

var pnpmLockfileNames = []string{"pnpm-lock.yaml", "pnpm-lock.yml"}

// PnpmInstallArgv returns pnpm install arguments for workdir. Greenfield
// workspaces often have package.json but no lockfile; frozen-lockfile fails
// with ERR_PNPM_NO_LOCKFILE until a lockfile is committed.
func PnpmInstallArgv(workdir string) []string {
	argv := []string{"pnpm", "install"}
	if HasPnpmLockfile(workdir) {
		argv = append(argv, "--frozen-lockfile")
	}
	return argv
}

// HasPnpmLockfile reports whether workdir contains a pnpm lockfile.
func HasPnpmLockfile(workdir string) bool {
	for _, name := range pnpmLockfileNames {
		if _, err := os.Stat(filepath.Join(workdir, name)); err == nil {
			return true
		}
	}
	return false
}

// NpmInstallArgv returns npm install/ci arguments for workdir.
func NpmInstallArgv(workdir string) []string {
	if _, err := os.Stat(filepath.Join(workdir, "package-lock.json")); err == nil {
		return []string{"npm", "ci"}
	}
	return []string{"npm", "install"}
}
