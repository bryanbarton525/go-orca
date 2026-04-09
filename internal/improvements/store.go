package improvements

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-orca/go-orca/internal/state"
)

// Manifest records the outcome of a direct-apply improvement operation.
// Written to manifests/<workflowID>.json after a successful promotion.
type Manifest struct {
	WorkflowID    string    `json:"workflow_id"`
	ComponentType string    `json:"component_type"`
	ComponentName string    `json:"component_name"`
	ChangeType    string    `json:"change_type"`
	ApplyMode     string    `json:"apply_mode"`
	Files         []string  `json:"files"`
	AppliedAt     time.Time `json:"applied_at"`
	Status        string    `json:"status"`
	Message       string    `json:"message,omitempty"`
}

// FileStore manages the improvements directory layout:
//
//	<root>/staging/<workflowID>/...   — files written during staging
//	<root>/active/...                 — live customization source (scanned by Registry)
//	<root>/disabled/<workflowID>/...  — rolled-back improvements
//	<root>/manifests/<workflowID>.json — apply outcome records
type FileStore struct {
	root string
}

// NewFileStore creates a FileStore rooted at root (e.g. artifacts/improvements).
func NewFileStore(root string) *FileStore {
	return &FileStore{root: root}
}

// Stage writes improvement files into staging/<workflowID>/.
func (s *FileStore) Stage(workflowID string, files []state.ImprovementFile) error {
	for _, f := range files {
		dst := StagingPath(s.root, workflowID, f.Path)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("improvements/store: mkdir staging %s: %w", dst, err)
		}
		if err := os.WriteFile(dst, []byte(f.Content), 0o644); err != nil {
			return fmt.Errorf("improvements/store: write staging %s: %w", dst, err)
		}
	}
	return nil
}

// Promote copies staged files for workflowID into active/, then removes the
// staging directory.  Returns the list of absolute active paths written.
func (s *FileStore) Promote(workflowID string, files []state.ImprovementFile) ([]string, error) {
	var appliedPaths []string
	for _, f := range files {
		src := StagingPath(s.root, workflowID, f.Path)
		dst := ActivePath(s.root, f.Path)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return appliedPaths, fmt.Errorf("improvements/store: mkdir active %s: %w", dst, err)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return appliedPaths, fmt.Errorf("improvements/store: read staged %s: %w", src, err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return appliedPaths, fmt.Errorf("improvements/store: write active %s: %w", dst, err)
		}
		appliedPaths = append(appliedPaths, dst)
	}
	// Remove staging directory on success (best-effort; non-fatal).
	_ = os.RemoveAll(filepath.Join(s.root, "staging", workflowID))
	return appliedPaths, nil
}

// Rollback moves staged files for workflowID into disabled/ and removes the
// staging directory.  Errors during rollback are silently ignored to avoid
// masking the original promote failure.
func (s *FileStore) Rollback(workflowID string, files []state.ImprovementFile) {
	for _, f := range files {
		src := StagingPath(s.root, workflowID, f.Path)
		dst := filepath.Join(s.root, "disabled", workflowID, f.Path)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			continue
		}
		_ = os.Rename(src, dst)
	}
	_ = os.RemoveAll(filepath.Join(s.root, "staging", workflowID))
}

// WriteManifest writes a JSON manifest record to manifests/<workflowID>.json.
func (s *FileStore) WriteManifest(_ context.Context, m Manifest) error {
	dir := filepath.Join(s.root, "manifests")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("improvements/store: mkdir manifests: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("improvements/store: marshal manifest: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, m.WorkflowID+".json"), data, 0o644)
}
