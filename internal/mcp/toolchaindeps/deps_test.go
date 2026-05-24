package toolchaindeps

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPnpmInstallArgv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := PnpmInstallArgv(dir)
	want := []string{"pnpm", "install"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("[%d] got %q want %q", i, got[i], want[i])
		}
	}
	// Stale lockfiles must not trigger frozen-lockfile during Pod remediation.
	if err := os.WriteFile(filepath.Join(dir, "pnpm-lock.yaml"), []byte("lockfileVersion: '9.0'\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := PnpmInstallArgv(dir); len(got) != 2 || got[1] != "install" {
		t.Fatalf("with lock got %v", got)
	}
}

func TestNpmInstallArgv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := NpmInstallArgv(dir); len(got) != 2 || got[1] != "install" {
		t.Fatalf("got %v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := NpmInstallArgv(dir); len(got) != 2 || got[1] != "ci" {
		t.Fatalf("got %v", got)
	}
}
