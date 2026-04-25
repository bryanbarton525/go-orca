// Package capabilities defines the canonical capability names and structured
// result shapes that every first-party go-orca MCP server returns.
//
// The workflow engine resolves a (toolchain, capability) pair to an underlying
// MCP tool name through the registry; the underlying tool MUST return a JSON
// payload that unmarshals into [Result] (for validation/lint/etc. capabilities)
// or [CheckpointResult] (for git_checkpoint / git_push_checkpoint).
package capabilities

import "encoding/json"

// Capability names. Servers may implement a subset; toolchain config maps each
// name to the actual MCP tool that backs it.
const (
	InitProject         = "init_project"
	InstallDependencies = "install_dependencies"
	TidyDependencies    = "tidy_dependencies"
	FormatCode          = "format_code"
	RunTests            = "run_tests"
	RunBuild            = "run_build"
	RunLint             = "run_lint"
	Typecheck           = "typecheck"
	SecurityScan        = "security_scan"
	GitStatus           = "git_status"
	GitCheckpoint       = "git_checkpoint"
	GitPushCheckpoint   = "git_push_checkpoint"
)

// Args is the standard envelope every capability tool receives.
// The engine populates these fields when invoking via the registry.
type Args struct {
	WorkflowID    string `json:"workflow_id,omitempty"`
	Phase         string `json:"phase,omitempty"`
	Capability    string `json:"capability,omitempty"`
	ToolchainID   string `json:"toolchain_id,omitempty"`
	WorkspacePath string `json:"workspace_path,omitempty"`
	RepoURL       string `json:"repo_url,omitempty"`
	Branch        string `json:"branch,omitempty"`
	Push          bool   `json:"push,omitempty"`
}

// Result is the structured outcome of a non-checkpoint capability invocation.
type Result struct {
	Passed   bool           `json:"passed"`
	Success  bool           `json:"success"`
	Stdout   string         `json:"stdout,omitempty"`
	Stderr   string         `json:"stderr,omitempty"`
	Output   string         `json:"output,omitempty"`
	Error    string         `json:"error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// CheckpointResult is the structured outcome of git_checkpoint / git_push_checkpoint.
type CheckpointResult struct {
	CommitSHA string `json:"commit_sha,omitempty"`
	Branch    string `json:"branch,omitempty"`
	Message   string `json:"message,omitempty"`
	Pushed    bool   `json:"pushed"`
}

// MarshalResult marshals r to JSON.  Intended for MCP tool handlers that need
// to embed the result as a TextContent payload.
func MarshalResult(r Result) ([]byte, error) { return json.Marshal(r) }

// MarshalCheckpoint marshals r to JSON.
func MarshalCheckpoint(r CheckpointResult) ([]byte, error) { return json.Marshal(r) }
