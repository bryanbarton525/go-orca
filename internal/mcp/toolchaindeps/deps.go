// Package toolchaindeps provides shared dependency-install argv selection for
// first-party toolchain MCP servers operating on go-orca workflow workspaces.
package toolchaindeps

import (
	"os"
	"path/filepath"
)

var pnpmLockfileNames = []string{"pnpm-lock.yaml", "pnpm-lock.yml"}

// PnpmInstallArgv returns pnpm install arguments for workdir. go-orca
// implementation validation runs while Pod is still editing package.json, so
// install must tolerate missing or stale lockfiles (frozen-lockfile fails with
// ERR_PNPM_NO_LOCKFILE / ERR_PNPM_OUTDATED_LOCKFILE during remediation loops).
func PnpmInstallArgv(workdir string) []string {
	_ = workdir // reserved for future lockfile-aware behavior
	return []string{"pnpm", "install"}
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
