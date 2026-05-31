package toolchaindeps

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckFakeBuildScript(t *testing.T) {
	dir := t.TempDir()
	writeJSON := func(scripts string) {
		t.Helper()
		body := `{"name":"x","scripts":` + scripts + `}`
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	writeJSON(`{"build":"next build"}`)
	if ok, _ := CheckFakeBuildScript(dir); !ok {
		t.Fatal("expected real build to pass")
	}

	writeJSON(`{"build":"echo build successful"}`)
	if ok, issue := CheckFakeBuildScript(dir); ok || issue == "" {
		t.Fatalf("expected fake build to fail, ok=%v issue=%q", ok, issue)
	}

	writeJSON(`{"build":"next build && echo no tests configured"}`)
	if ok, issue := CheckFakeBuildScript(dir); !ok || issue != "" {
		t.Fatalf("expected real build chain to pass, ok=%v issue=%q", ok, issue)
	}
}

func TestCheckPostCSSDeps(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "postcss.config.js"),
		[]byte(`module.exports = { plugins: { tailwindcss: {}, autoprefixer: {} } };`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"),
		[]byte(`{"name":"x","devDependencies":{"next":"14"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, issue := CheckPostCSSDeps(dir); ok || !containsAll(issue, "tailwindcss", "autoprefixer") {
		t.Fatalf("expected missing deps issue, ok=%v issue=%q", ok, issue)
	}
}

func TestCheckConflictingNextPages(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"page.js", "page.tsx"} {
		if err := os.WriteFile(filepath.Join(app, name), []byte("export default function P(){}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if ok, issue := CheckConflictingNextPages(dir); ok || issue == "" {
		t.Fatalf("expected conflict, ok=%v issue=%q", ok, issue)
	}
}

func TestCheckAppPagesRouterConflict(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app", "page.tsx"), []byte("export default function P(){}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "pages"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pages", "index.js"), []byte("export default function I(){}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, issue := CheckAppPagesRouterConflict(dir); ok || issue == "" {
		t.Fatalf("expected router conflict, ok=%v issue=%q", ok, issue)
	}
}

func TestCheckAppPagesRouterConflict_AllowsNonRootAppRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app", "settings"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app", "settings", "page.tsx"), []byte("export default function P(){}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "pages"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pages", "index.js"), []byte("export default function I(){}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ok, issue := CheckAppPagesRouterConflict(dir); !ok || issue != "" {
		t.Fatalf("expected no root router conflict, ok=%v issue=%q", ok, issue)
	}
}

func TestNodeWorkspacePreflightIssue_aggregates(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"),
		[]byte(`{"name":"x","scripts":{"build":"echo ok"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	issue := NodeWorkspacePreflightIssue(dir)
	if issue == "" || !strings.Contains(issue, "[build script]") {
		t.Fatalf("expected aggregated issue, got %q", issue)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}
