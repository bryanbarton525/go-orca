// Package customization discovers and manages GitHub-compatible skill, agent,
// and prompt files from one or more configured sources.
//
// Source types:
//   - filesystem: scans a local directory tree
//   - repo:       scans a checked-out repo's .github / .agents / .claude directories
//   - builtin:    in-process defaults shipped with gorca
//
// Resolution order (highest to lowest precedence):
//
//	workflow/repo → team → org → global → builtin
//
// Each source has: name, type, root, precedence, enabled_types, scope_slug.
// Snapshots are taken at workflow start so live changes don't affect running
// workflows.
package customization

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Kind classifies a customization file.
type Kind string

const (
	KindSkill  Kind = "skill"  // SKILL.md
	KindAgent  Kind = "agent"  // *.agent.md
	KindPrompt Kind = "prompt" // *.prompt.md
)

// Source describes a single customization source root.
type Source struct {
	Name         string // display name
	Type         string // "filesystem" | "repo" | "builtin"
	Root         string // absolute path
	Precedence   int    // lower = higher priority (0 = highest)
	EnabledTypes []Kind // which kinds to load from this source; empty = all
	ScopeSlug    string // scope this source applies to
}

// Item is a resolved customization file.
type Item struct {
	Kind       Kind
	Name       string // derived from filename without extension markers
	Content    string
	SourceName string
	Path       string
	Precedence int
	// Metadata holds key/value pairs parsed from the file's YAML frontmatter
	// (the --- ... --- block at the top of the file). The Content field has the
	// frontmatter stripped so only the markdown body is injected into prompts.
	Metadata map[string]string
}

// Snapshot is the resolved set of customization items for a single workflow run.
// It is immutable once built.
type Snapshot struct {
	Skills  []*Item
	Agents  []*Item
	Prompts []*Item
}

// SkillsContext returns all skill content concatenated for injection into prompts.
func (sn *Snapshot) SkillsContext() string {
	return joinContent(sn.Skills)
}

// AgentsContext returns the highest-precedence agent content (if any).
func (sn *Snapshot) AgentsContext() string {
	if len(sn.Agents) == 0 {
		return ""
	}
	return sn.Agents[0].Content
}

// PromptsContext returns all prompt content concatenated.
func (sn *Snapshot) PromptsContext() string {
	return joinContent(sn.Prompts)
}

func joinContent(items []*Item) string {
	if len(items) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, it := range items {
		sb.WriteString(fmt.Sprintf("## %s\n%s\n\n", it.Name, it.Content))
	}
	return strings.TrimSpace(sb.String())
}

// Registry holds sources and produces snapshots.
type Registry struct {
	mu       sync.RWMutex
	sources  []Source
	builtins []*Item
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// RegisterBuiltin registers one or more items that are returned by any source
// of type "builtin".  Calling this multiple times appends to the list.
func (r *Registry) RegisterBuiltin(items ...*Item) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, it := range items {
		it.SourceName = "builtin"
		r.builtins = append(r.builtins, it)
	}
}

// AddSource registers a new source.
func (r *Registry) AddSource(s Source) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sources = append(r.sources, s)
	sort.Slice(r.sources, func(i, j int) bool {
		return r.sources[i].Precedence < r.sources[j].Precedence
	})
}

// Snapshot scans all sources and returns an immutable snapshot.
// scopeSlug filters sources; pass "" to include all sources.
func (r *Registry) Snapshot(scopeSlug string) (*Snapshot, error) {
	r.mu.RLock()
	sources := make([]Source, len(r.sources))
	copy(sources, r.sources)
	builtinItems := make([]*Item, len(r.builtins))
	copy(builtinItems, r.builtins)
	r.mu.RUnlock()

	var items []*Item
	for _, src := range sources {
		if scopeSlug != "" && src.ScopeSlug != "" && src.ScopeSlug != scopeSlug {
			continue
		}
		found, err := scanSource(src, builtinItems)
		if err != nil {
			return nil, fmt.Errorf("customization: scan source %s: %w", src.Name, err)
		}
		items = append(items, found...)
	}

	// Deduplicate by Kind+Name, keeping highest-precedence (lowest Precedence int).
	snap := dedup(items)
	return snap, nil
}

// ─── Internal scan ────────────────────────────────────────────────────────────

func scanSource(src Source, builtins []*Item) ([]*Item, error) {
	if src.Type == "builtin" {
		// Return registered builtin items, filtered by this source's enabled types.
		var out []*Item
		for _, it := range builtins {
			if !kindEnabled(src.EnabledTypes, it.Kind) {
				continue
			}
			cp := *it
			cp.Precedence = src.Precedence
			out = append(out, &cp)
		}
		return out, nil
	}

	if _, err := os.Stat(src.Root); os.IsNotExist(err) {
		return nil, nil // missing roots are silently skipped
	}

	var items []*Item
	err := filepath.WalkDir(src.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name := d.Name()
		kind, ok := classifyFile(name)
		if !ok {
			return nil
		}
		if !kindEnabled(src.EnabledTypes, kind) {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		raw := string(data)
		meta := parseFrontmatter(raw)
		body := stripFrontmatter(raw)

		// For skills, prefer the name from frontmatter if present.
		itemName := deriveName(name, path, src.Root, kind)
		if kind == KindSkill {
			if fmName, ok := meta["name"]; ok && fmName != "" {
				itemName = fmName
			}
		}

		items = append(items, &Item{
			Kind:       kind,
			Name:       itemName,
			Content:    body,
			SourceName: src.Name,
			Path:       path,
			Precedence: src.Precedence,
			Metadata:   meta,
		})
		return nil
	})
	return items, err
}

func classifyFile(name string) (Kind, bool) {
	lower := strings.ToLower(name)
	switch {
	case lower == "skill.md":
		return KindSkill, true
	case strings.HasSuffix(lower, ".agent.md"):
		return KindAgent, true
	case strings.HasSuffix(lower, ".prompt.md"):
		return KindPrompt, true
	}
	return "", false
}

func deriveName(filename, filePath, sourceRoot string, kind Kind) string {
	switch kind {
	case KindAgent:
		name := strings.TrimSuffix(filename, ".md")
		return strings.TrimSuffix(name, ".agent")
	case KindPrompt:
		name := strings.TrimSuffix(filename, ".md")
		return strings.TrimSuffix(name, ".prompt")
	case KindSkill:
		// For package-style layouts the SKILL.md lives inside a named directory:
		//   skills/my-skill/SKILL.md  →  name "my-skill"  (parent dir)
		// A flat SKILL.md sitting directly in the source root keeps "skill" for
		// backward compatibility with existing configurations.
		parentDir := filepath.Dir(filePath)
		if parentDir == sourceRoot {
			return "skill"
		}
		return filepath.Base(parentDir)
	}
	return filename
}

// parseFrontmatter extracts key/value pairs from a YAML frontmatter block.
// The block must start at position 0 and be delimited by "---\n" on both ends.
// Only simple "key: value" lines are parsed; nested YAML is not supported.
// Returns an empty map when no valid frontmatter is present.
func parseFrontmatter(content string) map[string]string {
	m := make(map[string]string)
	if !strings.HasPrefix(content, "---\n") {
		return m
	}
	rest := content[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return m
	}
	body := rest[:end]
	for _, line := range strings.Split(body, "\n") {
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		if key != "" {
			m[key] = val
		}
	}
	return m
}

// stripFrontmatter removes a leading YAML frontmatter block from content.
// The block must start at position 0 and be delimited by "---\n" on both ends.
// Returns the original content unchanged when no valid frontmatter is detected.
func stripFrontmatter(content string) string {
	if !strings.HasPrefix(content, "---\n") {
		return content
	}
	rest := content[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return content
	}
	// skip past the closing "---" line and one trailing newline
	after := rest[end+4:] // len("\n---") == 4
	return strings.TrimLeft(after, "\n")
}

func kindEnabled(enabled []Kind, kind Kind) bool {
	if len(enabled) == 0 {
		return true
	}
	for _, k := range enabled {
		if k == kind {
			return true
		}
	}
	return false
}

// dedup keeps one item per (Kind, Name), choosing the highest precedence.
func dedup(items []*Item) *Snapshot {
	// Sort by precedence ascending (lower = more important).
	sort.Slice(items, func(i, j int) bool {
		return items[i].Precedence < items[j].Precedence
	})

	seen := map[string]bool{}
	snap := &Snapshot{}
	for _, it := range items {
		key := string(it.Kind) + "|" + it.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		switch it.Kind {
		case KindSkill:
			snap.Skills = append(snap.Skills, it)
		case KindAgent:
			snap.Agents = append(snap.Agents, it)
		case KindPrompt:
			snap.Prompts = append(snap.Prompts, it)
		}
	}
	return snap
}
