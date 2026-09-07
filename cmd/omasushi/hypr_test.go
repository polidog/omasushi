package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A pack's hypr: snippet is linked under omasushi.d, the loader lists what
// is there, and hyprland.lua gets the require line once.
func TestHyprSnippetsLoadFromHyprlandLua(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "") // no hyprctl: the real session must not be reloaded by a test
	writeFile(t, hyprlandLuaPath(), "require(\"hypr.bindings\")\n")

	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "herdr", ManifestFile), "hypr: hypr/herdr.lua\n")
	writeFile(t, filepath.Join(repo, "herdr", "hypr", "herdr.lua"), "-- herdr\n")
	writeFile(t, filepath.Join(repo, "apps", ManifestFile), "hypr: hypr/apps.lua\n")
	writeFile(t, filepath.Join(repo, "apps", "hypr", "apps.lua"), "-- apps\n")
	packs, err := packsFromDir(repo)
	if err != nil {
		t.Fatal(err)
	}

	actions, _ := Plan(packs, emptyState())
	var kinds []string
	for _, a := range actions {
		kinds = append(kinds, a.Kind)
	}
	want := "hypr-snippet,hypr-snippet,hypr-loader,hypr-reload"
	if got := strings.Join(kinds, ","); got != want {
		t.Fatalf("actions: %s, want %s", got, want)
	}
	// run the links and the loader (not the reload: no compositor here)
	for _, a := range actions[:3] {
		if err := a.Run(); err != nil {
			t.Fatal(a.Kind, err)
		}
	}

	base := filepath.Base(repo)
	for _, name := range []string{base + "-apps.lua", base + "-herdr.lua"} {
		if _, err := os.Readlink(filepath.Join(hyprSnippetsDir(), name)); err != nil {
			t.Errorf("snippet %s not linked: %v", name, err)
		}
	}
	loader, _ := os.ReadFile(hyprLoaderPath())
	if !strings.Contains(string(loader), base+"-apps.lua") || !strings.Contains(string(loader), base+"-herdr.lua") {
		t.Errorf("loader does not name both snippets:\n%s", loader)
	}
	if strings.Index(string(loader), "-apps.lua") > strings.Index(string(loader), "-herdr.lua") {
		t.Errorf("loader is in name order:\n%s", loader)
	}
	hl, _ := os.ReadFile(hyprlandLuaPath())
	if strings.Count(string(hl), hyprRequire) != 1 {
		t.Errorf("hyprland.lua must require the loader once:\n%s", hl)
	}

	// up to date now: nothing to do
	if actions, _ := Plan(packs, emptyState()); len(actions) != 0 {
		t.Errorf("second plan: %d actions", len(actions))
	}
	// and again with the require already there: still once
	if err := writeHyprLoader(); err != nil {
		t.Fatal(err)
	}
	hl, _ = os.ReadFile(hyprlandLuaPath())
	if strings.Count(string(hl), hyprRequire) != 1 {
		t.Errorf("require added twice:\n%s", hl)
	}

	// unlinking one pack drops its snippet and rewrites the loader without it
	if _, err := Unlink(packs[:1], false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(hyprSnippetsDir(), base+"-apps.lua")); !os.IsNotExist(err) {
		t.Error("apps snippet should be gone")
	}
	loader, _ = os.ReadFile(hyprLoaderPath())
	if strings.Contains(string(loader), "-apps.lua") || !strings.Contains(string(loader), "-herdr.lua") {
		t.Errorf("loader after unlink:\n%s", loader)
	}
}

// A machine with no hypr: pack is left alone: no loader, no line in hyprland.lua.
func TestHyprUntouchedWithoutSnippets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeFile(t, hyprlandLuaPath(), "require(\"hypr.bindings\")\n")
	packs := []Pack{{Name: "x", Manifest: &Manifest{Packages: Packages{Aur: []string{"a"}}}}}
	actions, _ := Plan(packs, emptyState())
	for _, a := range actions {
		if strings.HasPrefix(a.Kind, "hypr") {
			t.Errorf("unexpected %s", a.Kind)
		}
	}
	if _, err := os.Stat(hyprLoaderPath()); !os.IsNotExist(err) {
		t.Error("loader written for nothing")
	}
}
