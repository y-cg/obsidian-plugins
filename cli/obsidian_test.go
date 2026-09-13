package main

import (
	"os"
	"path/filepath"
	"testing"
)

func strptr(s string) *string { return &s }

func TestUpToDate(t *testing.T) {
	entry := lockEntry{Repo: "a/b", Version: "1.0.0"}

	tests := []struct {
		name   string
		entry  lockEntry
		d      declaration
		update bool
		want   bool
	}{
		{"empty lock", lockEntry{}, declaration{}, false, false},
		{"id-only matches anything", entry, declaration{}, false, true},
		{"explicit repo matches", entry, declaration{Repo: strptr("a/b")}, false, true},
		{"explicit repo differs", entry, declaration{Repo: strptr("c/d")}, false, false},
		{"explicit version matches", entry, declaration{Version: strptr("1.0.0")}, false, true},
		{"explicit version differs", entry, declaration{Version: strptr("2.0.0")}, false, false},
		{"update forces re-resolve", entry, declaration{}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := upToDate(tt.entry, tt.d, tt.update); got != tt.want {
				t.Errorf("upToDate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSplitRepo(t *testing.T) {
	owner, name, err := splitRepo("blacksmithgu/obsidian-dataview")
	if err != nil || owner != "blacksmithgu" || name != "obsidian-dataview" {
		t.Fatalf("splitRepo() = %q, %q, %v", owner, name, err)
	}
	for _, bad := range []string{"", "noslash", "/name", "owner/"} {
		if _, _, err := splitRepo(bad); err == nil {
			t.Errorf("splitRepo(%q) succeeded, want error", bad)
		}
	}
}

func TestBelongsTo(t *testing.T) {
	dirs := []string{"/nix/store/aaa-plugin", "/nix/store/bbb-plugin"}
	if !belongsTo("/nix/store/aaa-plugin/main.js", dirs) {
		t.Error("expected match for declared dir")
	}
	if belongsTo("/nix/store/ccc-plugin/main.js", dirs) {
		t.Error("unexpected match for undeclared dir")
	}
	// A sibling whose name extends a declared dir must not match.
	if belongsTo("/nix/store/aaa-plugin-extra/main.js", dirs) {
		t.Error("matched a sibling prefix")
	}
}

func TestSweepKeepsDeclaredAndUserFiles(t *testing.T) {
	pluginsDir := filepath.Join(t.TempDir(), "plugins")
	old := filepath.Join(pluginsDir, "old")
	keep := filepath.Join(pluginsDir, "keep")
	for _, dir := range []string{old, keep} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustSymlink(t, "/nix/store/aaa-old/main.js", filepath.Join(old, "main.js"))
	mustSymlink(t, "/nix/store/bbb-keep/main.js", filepath.Join(keep, "main.js"))
	if err := os.WriteFile(filepath.Join(old, "data.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := sweep(pluginsDir, map[string]*string{"keep": strptr("/nix/store/bbb-keep")}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Lstat(filepath.Join(old, "main.js")); !os.IsNotExist(err) {
		t.Error("stale store symlink was not swept")
	}
	if _, err := os.Stat(filepath.Join(old, "data.json")); err != nil {
		t.Error("user-written data.json must survive the sweep")
	}
	if _, err := os.Lstat(filepath.Join(keep, "main.js")); err != nil {
		t.Error("declared symlink was swept")
	}
}

func TestInstallLinksFilesAndSkipsRealOnes(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	pluginsDir := filepath.Join(root, "plugins")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"main.js", "manifest.json"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	plugins := map[string]*string{"p": strptr(src)}
	if err := install(pluginsDir, plugins); err != nil {
		t.Fatal(err)
	}
	target, err := os.Readlink(filepath.Join(pluginsDir, "p", "main.js"))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(src, "main.js"); target != want {
		t.Errorf("symlink target = %q, want %q", target, want)
	}

	// A real file in the vault must not be clobbered by a later link.
	real := filepath.Join(pluginsDir, "p", "manifest.json")
	if err := os.Remove(real); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte("handwritten"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := install(pluginsDir, plugins); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(real)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "handwritten" {
		t.Errorf("real file was overwritten: %q", got)
	}
}

func mustSymlink(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}
