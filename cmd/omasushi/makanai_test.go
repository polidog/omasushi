package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The three layers stack in one order: what this machine takes from other
// people at the bottom, the recipe it publishes over them, and the machine
// manifest itself on top.
func TestMakanaiLayers(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	theirs := t.TempDir()
	writeFile(t, filepath.Join(theirs, ManifestFile), "name: theirs\npackages:\n  aur: [theirs]\nomarchy:\n  font: Theirs\n")
	recipe := t.TempDir()
	writeFile(t, filepath.Join(recipe, ManifestFile), "name: my-omakase\npackages:\n  aur: [mine]\nomarchy:\n  font: Recipe\n")

	makanai := &Makanai{Recipe: recipe, Manifest: Manifest{
		Use:      []Use{{Source: theirs}},
		Packages: Packages{Aur: []string{"work-vpn"}},
		Omarchy:  Omarchy{Font: "This Machine"},
	}}
	rs, err := activeOmakases(makanai, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := names(rs); len(got) != 3 || got[0] != filepath.Base(theirs) || got[1] != filepath.Base(recipe) || got[2] != MakanaiName {
		t.Fatalf("layer order: %v, want theirs, recipe, makanai", got)
	}

	// the makanai wins the merge, and its own picks are in the stack
	var merged Overlay
	for _, r := range rs {
		merged = merged.merge(r.Resolve("h"))
	}
	if merged.Omarchy.Font != "This Machine" {
		t.Errorf("the makanai must win: font %q", merged.Omarchy.Font)
	}
	for _, want := range []string{"theirs", "mine", "work-vpn"} {
		if !contains(merged.Packages.Aur, want) {
			t.Errorf("%q missing from %v", want, merged.Packages.Aur)
		}
	}

	// its files: are rooted beside it, so they live in ~/.config/omasushi
	if rs[2].Dir != makanaiDir() {
		t.Errorf("makanai dir: %s, want %s", rs[2].Dir, makanaiDir())
	}
}

// A blank makanai steps aside for an omasushi.yaml in the working
// directory, which is how a checkout is driven in place.
func TestBlankMakanaiFallsBackToCwd(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if !(&Makanai{}).blank() {
		t.Error("an empty makanai is blank")
	}
	if (&Makanai{Recipe: "x"}).blank() {
		t.Error("a recipe makes it not blank")
	}
	if (&Makanai{Manifest: Manifest{Packages: Packages{Aur: []string{"p"}}}}).blank() {
		t.Error("its own packages make it not blank")
	}
}

// use records the source as typed and never twice; the recipe goes to its own
// slot, not into use:.
func TestMakanaiAddAndRemove(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	theirs := t.TempDir()
	writeFile(t, filepath.Join(theirs, ManifestFile), "name: theirs\n")
	mine := t.TempDir()
	writeFile(t, filepath.Join(mine, ManifestFile), "name: mine\n")

	makanai := &Makanai{}
	if _, err := makanai.Add(theirs, false); err != nil {
		t.Fatal(err)
	}
	if _, err := makanai.Add(theirs, false); err != nil { // again: no duplicate
		t.Fatal(err)
	}
	if _, err := makanai.Add(mine, true); err != nil {
		t.Fatal(err)
	}
	if len(makanai.Use) != 1 || makanai.Use[0].Source != theirs {
		t.Fatalf("use: %+v", makanai.Use)
	}
	if makanai.Recipe != mine {
		t.Fatalf("recipe: %q", makanai.Recipe)
	}

	// it is written where the next command will read it
	back, err := LoadMakanai()
	if err != nil || back.Recipe != mine || len(back.Use) != 1 {
		t.Fatalf("reload: %v %+v", err, back)
	}

	if err := makanai.Remove(filepath.Base(theirs)); err != nil {
		t.Fatal(err)
	}
	if len(makanai.Use) != 0 {
		t.Fatalf("after remove: %+v", makanai.Use)
	}
	if err := makanai.Remove("nobody/nothing"); err == nil {
		t.Error("removing what is not in use: want error")
	}

	// the recipe is not removed by name — it has its own command
	if err := makanai.Remove(filepath.Base(mine)); err == nil || !strings.Contains(err.Error(), "recipe") {
		t.Errorf("removing the recipe: got %v", err)
	}
}

// The pre-makanai config.yaml is converted once: omakases: becomes
// use:, and the omakase that was mine: becomes the recipe.
func TestMigrateConfig(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	writeFile(t, filepath.Join(cfgDir, "omasushi", "config.yaml"), `omakases:
  - name: someone/base
    source: someone/base
  - name: polidog/omakase/herdr
    source: polidog/omakase
    part: herdr
  - name: me/setup
    source: me/setup
mine: me/setup
`)
	m, err := LoadMakanai()
	if err != nil {
		t.Fatal(err)
	}
	if m.Recipe != "me/setup" {
		t.Errorf("recipe: %q", m.Recipe)
	}
	if len(m.Use) != 2 || m.Use[0].Source != "someone/base" || m.Use[1].Source != "polidog/omakase/herdr" {
		t.Fatalf("use: %+v", m.Use)
	}
	if _, err := os.Stat(makanaiPath()); err != nil {
		t.Fatalf("the converted manifest must be written: %v", err)
	}
	// and it is what gets read from then on, config.yaml untouched
	again, err := LoadMakanai()
	if err != nil || again.Recipe != "me/setup" || len(again.Use) != 2 {
		t.Fatalf("reload: %v %+v", err, again)
	}
}

// recipe: survives a save/load round trip alongside the manifest it is inlined
// with — a makanai is an omasushi.yaml with one key more.
func TestMakanaiRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := &Makanai{Recipe: "~/src/omakase", Manifest: Manifest{
		Use:      []Use{{Source: "someone/big", Only: Selection{"packages.aur": {"kitty"}}}},
		Packages: Packages{Aur: []string{"work-vpn"}},
		Files:    map[string]string{"files/work/gitconfig": "~/.config/git/config.work"},
	}}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(makanaiPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "recipe: ~/src/omakase\n") {
		t.Errorf("recipe: must lead the file:\n%s", b)
	}
	back, err := LoadMakanai()
	if err != nil {
		t.Fatal(err)
	}
	if back.Recipe != m.Recipe || len(back.Use) != 1 || back.Use[0].Only["packages.aur"][0] != "kitty" {
		t.Fatalf("round trip: %+v", back)
	}
	if back.Files["files/work/gitconfig"] != "~/.config/git/config.work" {
		t.Fatalf("files lost: %+v", back.Files)
	}
}

// This machine's own omakase is never publishable: it is the layer that stays.
func TestPublishRefusesTheMakanai(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	writeFile(t, makanaiPath(), "packages:\n  aur: [work-vpn]\n")
	if _, _, err := publishDir(makanaiDir()); err == nil || !strings.Contains(err.Error(), "recipe") {
		t.Errorf("publishing the makanai: got %v", err)
	}
	if _, _, err := publishTarget(&Makanai{}, "", ""); err == nil {
		t.Error("publish with no recipe and no manifest here: want error")
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
