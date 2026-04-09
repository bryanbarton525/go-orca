package improvements

import (
	"fmt"
	"path/filepath"
	"strings"
)

// blockedPrefixes are relative path prefixes that may never be written via the
// direct-apply path.  Persona base-file changes require a PR workflow.
var blockedPrefixes = []string{
	"personas/",
}

// ValidatePath returns an error when relPath is unsafe to write inside the
// improvements directory.  It rejects:
//   - absolute paths
//   - paths that contain ".." traversal components
//   - paths whose prefix is in blockedPrefixes
func ValidatePath(relPath string) error {
	if filepath.IsAbs(relPath) {
		return fmt.Errorf("path policy: absolute path not allowed: %q", relPath)
	}
	// Normalise to forward-slash for cross-platform safety.
	cleaned := filepath.ToSlash(filepath.Clean(relPath))
	if strings.Contains(cleaned, "..") {
		return fmt.Errorf("path policy: path traversal not allowed: %q", relPath)
	}
	for _, prefix := range blockedPrefixes {
		if strings.HasPrefix(cleaned+"/", prefix) || strings.HasPrefix(cleaned, prefix) {
			return fmt.Errorf("path policy: writes to %q are blocked; persona changes require a PR workflow", prefix)
		}
	}
	return nil
}

// ActivePath returns the full absolute path for a file in the active/
// subdirectory of the improvements root.
func ActivePath(root, relPath string) string {
	return filepath.Join(root, "active", relPath)
}

// StagingPath returns the full absolute path for a file in the staging/
// subdirectory of the improvements root, scoped to a workflow ID.
func StagingPath(root, workflowID, relPath string) string {
	return filepath.Join(root, "staging", workflowID, relPath)
}
