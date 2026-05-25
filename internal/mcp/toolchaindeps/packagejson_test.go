package toolchaindeps

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidatePackageJSONBytes_rejectsCommentPrefix(t *testing.T) {
	raw := []byte("// Contents of updated package.json are correctly formatted JSON\n{\"name\":\"x\"}\n")
	ok, issue := ValidatePackageJSONBytes(raw, "package.json")
	if ok {
		t.Fatal("expected invalid")
	}
	if issue == "" || !contains(issue, "comment") {
		t.Fatalf("issue = %q", issue)
	}
}

func TestValidatePackageJSONBytes_acceptsValidJSON(t *testing.T) {
	raw := []byte("{\n  \"name\": \"rss-newspaper\",\n  \"private\": true\n}\n")
	ok, issue := ValidatePackageJSONBytes(raw, "package.json")
	if !ok {
		t.Fatalf("expected valid, issue=%q", issue)
	}
}

func TestCheckPackageJSON_missingIsOK(t *testing.T) {
	dir := t.TempDir()
	ok, issue := CheckPackageJSON(dir)
	if !ok || issue != "" {
		t.Fatalf("missing package.json should be ok for greenfield; ok=%v issue=%q", ok, issue)
	}
}

func TestCheckPackageJSON_rejectsOnDiskComment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, PackageJSONRelPath)
	if err := os.WriteFile(path, []byte("// bad\n{\"name\":\"x\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, issue := CheckPackageJSON(dir)
	if ok {
		t.Fatal("expected invalid")
	}
	if issue == "" {
		t.Fatal("expected issue")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
