package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This machine's file is the top of the stack: the packs it uses come first,
// and its own entries win the merge.
func TestLocalLayers(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	theirs := t.TempDir()
	writeFile(t, filepath.Join(theirs, ManifestFile), "packages:\n  aur: [theirs]\nomarchy:\n  font: Theirs\n")

	local := &Local{Manifest: Manifest{
		Use:      []string{theirs},
		Packages: Packages{Aur: []string{"work-vpn"}},
		Omarchy:  Omarchy{Font: "This Machine"},
	}}
	ps, err := activePacks(local, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := names(ps); len(got) != 2 || got[0] != filepath.Base(theirs) || got[1] != LocalName {
		t.Fatalf("layer order: %v, want theirs, local", got)
	}
	var merged Manifest
	for _, p := range ps {
		merged = merged.merge(*p.Manifest)
	}
	if merged.Omarchy.Font != "This Machine" {
		t.Errorf("this machine must win: font %q", merged.Omarchy.Font)
	}
	for _, want := range []string{"theirs", "work-vpn"} {
		if !contains(merged.Packages.Aur, want) {
			t.Errorf("%q missing from %v", want, merged.Packages.Aur)
		}
	}
	// its files: are rooted beside it, so they live in ~/.config/omasushi
	if ps[1].Dir != localDir() {
		t.Errorf("local dir: %s, want %s", ps[1].Dir, localDir())
	}
}

// A blank file steps aside for a pack in the working directory, which is
// how a checkout is driven in place.
func TestBlankLocalFallsBackToCwd(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if !(&Local{}).blank() {
		t.Error("an empty file is blank")
	}
	if (&Local{Manifest: Manifest{Use: []string{"x"}}}).blank() {
		t.Error("a use: makes it not blank")
	}
	if (&Local{Manifest: Manifest{Packages: Packages{Aur: []string{"p"}}}}).blank() {
		t.Error("its own packages make it not blank")
	}
}

// use records the source as typed and never twice; remove drops it and
// refuses what is not there.
func TestLocalAddAndRemove(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	theirs := t.TempDir()
	writeFile(t, filepath.Join(theirs, ManifestFile), "name: theirs\n")

	local := &Local{}
	if _, err := local.Add(theirs); err != nil {
		t.Fatal(err)
	}
	if _, err := local.Add(theirs); err != nil { // again: no duplicate
		t.Fatal(err)
	}
	if len(local.Use) != 1 || local.Use[0] != theirs {
		t.Fatalf("use: %+v", local.Use)
	}
	back, err := LoadLocal()
	if err != nil || len(back.Use) != 1 {
		t.Fatalf("reload: %v %+v", err, back)
	}
	if err := local.Remove(filepath.Base(theirs)); err != nil {
		t.Fatal(err)
	}
	if len(local.Use) != 0 {
		t.Fatalf("after remove: %+v", local.Use)
	}
	if err := local.Remove("nobody/nothing"); err == nil {
		t.Error("removing what is not in use: want error")
	}
}

// recipe: from before packs is folded into use: once, and the file rewritten
// without it.
func TestLocalMigratesRecipe(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeFile(t, localPath(), "recipe: ~/src/omakase\nuse:\n  - someone/base\npackages:\n  aur: [work-vpn]\n")
	l, err := LoadLocal()
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Use) != 2 || l.Use[0] != "someone/base" || l.Use[1] != "~/src/omakase" {
		t.Fatalf("use: %+v", l.Use)
	}
	b, _ := os.ReadFile(localPath())
	if strings.Contains(string(b), "recipe:") {
		t.Errorf("recipe: must be gone from the file:\n%s", b)
	}
	if !strings.Contains(string(b), "work-vpn") {
		t.Errorf("the machine's own entries must survive:\n%s", b)
	}
}

// The managed checkouts move from the old omakases directory, once.
func TestMigrateCheckouts(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	old := filepath.Join(data, "omasushi", "omakases", "polidog", "omakase")
	writeFile(t, filepath.Join(old, ManifestFile), "name: x\n")
	migrateCheckouts()
	if _, err := os.Stat(filepath.Join(packsDir(), "polidog", "omakase", ManifestFile)); err != nil {
		t.Errorf("checkout not moved: %v", err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("old directory still there")
	}
}

// The file survives a save/load round trip.
func TestLocalRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	l := &Local{Manifest: Manifest{
		Use:      []string{"someone/big/kitty"},
		Packages: Packages{Aur: []string{"work-vpn"}},
		Files:    map[string]string{"files/work/gitconfig": "~/.config/git/config.work"},
	}}
	if err := l.Save(); err != nil {
		t.Fatal(err)
	}
	back, err := LoadLocal()
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Use) != 1 || back.Use[0] != "someone/big/kitty" {
		t.Fatalf("round trip: %+v", back)
	}
	if back.Files["files/work/gitconfig"] != "~/.config/git/config.work" {
		t.Fatalf("files lost: %+v", back.Files)
	}
}

// This machine's own file is never publishable: it is the layer that stays.
func TestPublishRefusesLocal(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeFile(t, localPath(), "packages:\n  aur: [work-vpn]\n")
	if _, _, err := publishDir(localDir()); err == nil || !strings.Contains(err.Error(), "never published") {
		t.Errorf("publishing this machine's file: got %v", err)
	}
	if _, _, err := publishTarget(&Local{}, "", ""); err == nil {
		t.Error("publish with nothing here: want error")
	}
}

// export writes into the pack directory named, or this machine's file with
// --local; a repository of several packs must be narrowed to one.
func TestExportTarget(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, ManifestFile), "name: repo\n")
	writeFile(t, filepath.Join(repo, "ime", ManifestFile), "packages:\n  pacman: [fcitx5]\n")
	writeFile(t, filepath.Join(repo, "fonts", ManifestFile), "omarchy:\n  font: X\n")
	local := &Local{Manifest: Manifest{Use: []string{repo}}}
	packs, err := activePacks(local, "")
	if err != nil {
		t.Fatal(err)
	}

	got, err := exportTarget(packs, local, []string{filepath.Join(repo, "ime")}, false)
	if err != nil || got.Sub != "ime" || got.Manifest != packs[1].Manifest {
		t.Errorf("pack in use: got %+v, %v", got, err)
	}
	if got, err := exportTarget(packs, local, nil, true); err != nil || got.Name != LocalName {
		t.Errorf("--local: got %v, %v", got, err)
	}
	if _, err := exportTarget(packs, local, []string{repo}, false); err == nil || !strings.Contains(err.Error(), "fonts, ime") {
		t.Errorf("a repository of packs: want an error naming them, got %v", err)
	}
	if _, err := exportTarget(packs, local, nil, false); err == nil {
		t.Error("no target: want error")
	}
	other := t.TempDir()
	writeFile(t, filepath.Join(other, ManifestFile), "description: not in use\n")
	if got, err := exportTarget(packs, local, []string{other}, false); err != nil || got.Dir != other {
		t.Errorf("a pack not in use: got %v, %v", got, err)
	}
}
