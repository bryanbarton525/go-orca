package toolchaindeps

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PackageJSONRelPath is the manifest path validated before Node/Next installs.
const PackageJSONRelPath = "package.json"

// CheckPackageJSON reports whether workdir/package.json exists and is strict RFC 8259 JSON.
// JavaScript-style comments, trailing commas, and prose prefixes are rejected because
// pnpm/npm parse the file as JSON only.
func CheckPackageJSON(workdir string) (ok bool, issue string) {
	path := filepath.Join(workdir, PackageJSONRelPath)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, "" // greenfield: install may scaffold or fail with a clear missing-file error
		}
		return false, fmt.Sprintf("package.json: read %s: %v", path, err)
	}
	return ValidatePackageJSONBytes(data, path)
}

// ValidatePackageJSONBytes validates raw package.json contents.
func ValidatePackageJSONBytes(data []byte, label string) (bool, string) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return false, fmt.Sprintf("%s is empty", label)
	}
	if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
		return false, fmt.Sprintf("%s starts with a comment; JSON does not allow // or /* */ — remove comment lines and write strict JSON only", label)
	}
	if strings.Contains(trimmed, "// Contents of") || strings.Contains(trimmed, "// Content") {
		return false, fmt.Sprintf("%s contains prose/comment prefixes (e.g. \"// Contents of...\"); rewrite as pure JSON with no leading comment lines", label)
	}
	var probe any
	if err := json.Unmarshal(data, &probe); err != nil {
		return false, fmt.Sprintf("%s is not valid JSON: %v — rewrite the file as strict JSON (no comments, no trailing commas, double-quoted keys)", label, err)
	}
	if _, ok := probe.(map[string]any); !ok {
		return false, fmt.Sprintf("%s must be a JSON object at the root", label)
	}
	return true, ""
}

// PreflightIssue formats a workspace preflight blocker for the engine validation loop.
func PackageJSONPreflightIssue(workdir string) string {
	ok, issue := CheckPackageJSON(workdir)
	if ok || issue == "" {
		return ""
	}
	return fmt.Sprintf("[package.json] %s — fix the manifest before re-running install_dependencies or assigning another install task", issue)
}
