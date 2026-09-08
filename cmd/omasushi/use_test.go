package main

import (
	"path/filepath"
	"testing"
)

// A pack's use: pulls other packs in underneath it: dependencies come first
// (so the declaring pack wins the merge), Via names who brought them.
func TestResolveUsesLayersDependenciesFirst(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "c", ManifestFile), "packages:\n  aur: [pc]\n")
	writeFile(t, filepath.Join(root, "b", ManifestFile), "use: [../c]\npackages:\n  aur: [pb]\n")
	writeFile(t, filepath.Join(root, "a", ManifestFile), "use: [../b]\npackages:\n  aur: [pa]\n")

	out := resolve(t, filepath.Join(root, "a"))
	if len(out) != 3 || out[0].Name != "c" || out[1].Name != "b" || out[2].Name != "a" {
		t.Fatalf("order: got %+v", names(out))
	}
	if out[0].Via != "b" || out[1].Via != "a" || out[2].Via != "" {
		t.Errorf("via: got %q %q %q", out[0].Via, out[1].Via, out[2].Via)
	}
}

// A dependency already taken directly keeps its own position and is not
// loaded twice; a use: cycle resolves instead of looping.
func TestResolveUsesDedupAndCycles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", ManifestFile), "use: [../b]\n")
	writeFile(t, filepath.Join(root, "b", ManifestFile), "name: b\n")

	ra, err := packsFromDir(filepath.Join(root, "a"))
	if err != nil {
		t.Fatal(err)
	}
	rb, err := packsFromDir(filepath.Join(root, "b"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := resolveUses(append(rb, ra...))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].Name != "b" || out[1].Name != "a" {
		t.Fatalf("dedup: got %v", names(out))
	}
	if out[0].Via != "" || out[1].Via != "" {
		t.Errorf("packs taken directly must not be marked via: %+v", out)
	}

	cyc := t.TempDir()
	writeFile(t, filepath.Join(cyc, "a", ManifestFile), "use: [../b]\n")
	writeFile(t, filepath.Join(cyc, "b", ManifestFile), "use: [../a]\n")
	out = resolve(t, filepath.Join(cyc, "a")) // cycle a -> b -> a
	if len(out) != 2 || out[0].Name != "b" || out[1].Name != "a" || out[0].Via != "a" {
		t.Fatalf("cycle: got %+v", names(out))
	}
}

// A sibling pack named by relative path keeps its repository's name, so
// polidog/omakase/kitty using ../fonts brings polidog/omakase/fonts — the
// same pack someone else may already have taken by that name.
func TestUseSiblingKeepsRepositoryName(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, "fonts", ManifestFile), "omarchy:\n  font: UDEV Gothic NF\n")
	writeFile(t, filepath.Join(repo, "kitty", ManifestFile), "use: [../fonts]\nomarchy:\n  defaults: {terminal: kitty}\n")

	src, err := parseSource(repo)
	if err != nil {
		t.Fatal(err)
	}
	src.Sub = "kitty"
	kitty, err := packsIn(src, repo, "")
	if err != nil {
		t.Fatal(err)
	}
	out, err := resolveUses(kitty)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(repo)
	if len(out) != 2 || out[0].Name != base+"/fonts" || out[1].Name != base+"/kitty" {
		t.Fatalf("got %v", names(out))
	}
	if out[0].Repo != repo || out[0].Dir != filepath.Join(repo, "fonts") || out[0].Via != base+"/kitty" {
		t.Errorf("fonts: %+v", out[0])
	}

	// taking the whole repository reaches fonts once, in its own place
	all, err := packsFromDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	out, err = resolveUses(all)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].Name != base+"/fonts" || out[0].Via != "" {
		t.Errorf("whole repository: got %v (via %q)", names(out), out[0].Via)
	}
}

// A pack that is only a use: list is a bundle: everything it names, nothing
// of its own.
func TestBundle(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "ime", ManifestFile), "packages:\n  pacman: [fcitx5]\n")
	writeFile(t, filepath.Join(root, "nvim", ManifestFile), "packages:\n  aur: [neovim]\n")
	writeFile(t, filepath.Join(root, "setup", ManifestFile), "use:\n  - ../ime\n  - ../nvim\n")
	out := resolve(t, filepath.Join(root, "setup"))
	if got := names(out); len(got) != 3 || got[0] != "ime" || got[1] != "nvim" || got[2] != "setup" {
		t.Fatalf("got %v", got)
	}
	var merged Manifest
	for _, p := range out {
		merged = merged.merge(*p.Manifest)
	}
	if len(merged.Packages.Pacman) != 1 || len(merged.Packages.Aur) != 1 {
		t.Errorf("merged: %+v", merged.Packages)
	}
}

// Plan attributes each pending action to the pack behind it: list items to
// the first declarer, scalars to the last (the merge winner).
func TestPlanProvenance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	packs := []Pack{
		{Name: "someone/base", Manifest: &Manifest{
			Packages: Packages{Aur: []string{"foo", "shared"}},
			Omarchy:  Omarchy{Font: "Base Font"},
		}},
		{Name: "me/setup", Manifest: &Manifest{
			Packages: Packages{Aur: []string{"bar", "shared"}},
			Omarchy:  Omarchy{Font: "My Font"},
		}},
	}
	actions, _ := Plan(packs, emptyState())

	byPack := map[string]string{}
	var font string
	for _, a := range actions {
		switch a.Kind {
		case "aur":
			byPack[a.Pack] = a.Desc
		case "font":
			font = a.Pack
		}
	}
	if byPack["me/setup"] != "install bar" {
		t.Errorf("me/setup: got %q", byPack["me/setup"])
	}
	if byPack["someone/base"] != "install foo shared" {
		t.Errorf("someone/base gets shared (first declarer): got %q", byPack["someone/base"])
	}
	if font != "me/setup" {
		t.Errorf("font goes to the merge winner: got %q", font)
	}
}

func emptyState() *State {
	return &State{
		Aur: map[string]bool{}, Pacman: map[string]bool{}, Provides: map[string]bool{},
		OmarchyPlugins: map[string]InstalledOmarchyPlugin{}, HerdrPlugins: map[string]bool{},
	}
}

func names(ps []Pack) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Name)
	}
	return out
}

func resolve(t *testing.T, dir string) []Pack {
	t.Helper()
	ps, err := packsFromDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out, err := resolveUses(ps)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
