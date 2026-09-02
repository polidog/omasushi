package main

import (
	"path/filepath"
	"testing"
)

// A manifest's use: pulls other omakases in underneath it: dependencies come
// first (so the declaring omakase wins the merge), Via names who brought them.
func TestResolveUsesLayersDependenciesFirst(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "c", ManifestFile), "packages:\n  aur: [pc]\n")
	writeFile(t, filepath.Join(root, "b", ManifestFile), "use: [../c]\npackages:\n  aur: [pb]\n")
	writeFile(t, filepath.Join(root, "a", ManifestFile), "use: [../b]\npackages:\n  aur: [pa]\n")

	rs, err := omakaseFromDir(filepath.Join(root, "a", ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	out, err := resolveUses(rs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 || out[0].Name != "c" || out[1].Name != "b" || out[2].Name != "a" {
		t.Fatalf("order: got %+v", names(out))
	}
	if out[0].Via != "b" || out[1].Via != "a" || out[2].Via != "" {
		t.Errorf("via: got %q %q %q", out[0].Via, out[1].Via, out[2].Via)
	}
}

// A dependency already configured directly keeps its own position and is not
// loaded twice; a use: cycle resolves instead of looping.
func TestResolveUsesDedupAndCycles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a", ManifestFile), "use: [../b]\n")
	writeFile(t, filepath.Join(root, "b", ManifestFile), "name: b\n")

	ra, err := omakaseFromDir(filepath.Join(root, "a", ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	rb, err := omakaseFromDir(filepath.Join(root, "b", ManifestFile))
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
		t.Errorf("directly configured omakases must not be marked via: %+v", out)
	}

	cyc := t.TempDir()
	writeFile(t, filepath.Join(cyc, "a", ManifestFile), "use: [../b]\n")
	writeFile(t, filepath.Join(cyc, "b", ManifestFile), "use: [../a]\n")
	ra, err = omakaseFromDir(filepath.Join(cyc, "a", ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	out, err = resolveUses(ra) // cycle a -> b -> a
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].Name != "b" || out[1].Name != "a" || out[0].Via != "a" {
		t.Fatalf("cycle: got %+v", names(out))
	}
}

// The root use: of a split repository belongs to the bundle: its omakases are
// layered before every part.
func TestResolveUsesSplitRepoRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "c", ManifestFile), "packages:\n  aur: [pc]\n")
	writeFile(t, filepath.Join(root, "bundle", ManifestFile), "use: [../c]\nparts: [kitty]\n")
	writeFile(t, filepath.Join(root, "bundle", "kitty", ManifestFile), "files:\n  files/kitty.conf: ~/.config/kitty/kitty.conf\n")

	rs, err := omakaseFromDir(filepath.Join(root, "bundle", ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	out, err := resolveUses(rs)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].Name != "c" || out[1].Name != "bundle/kitty" {
		t.Fatalf("got %v", names(out))
	}
}

// export writes to the user's own omakase when one is marked, --to still wins,
// and with neither the old "pick one" error stands.
func TestExportTargetPrefersMine(t *testing.T) {
	rs := []Omakase{{Name: "someone/base"}, {Name: "me/setup"}}
	if got, err := exportTarget(rs, "", "me/setup"); err != nil || got.Name != "me/setup" {
		t.Errorf("mine: got %v, %v", got, err)
	}
	if got, err := exportTarget(rs, "someone/base", "me/setup"); err != nil || got.Name != "someone/base" {
		t.Errorf("--to over mine: got %v, %v", got, err)
	}
	if _, err := exportTarget(rs, "", ""); err == nil {
		t.Error("no mine, several in use: want error")
	}
	if _, err := exportTarget(rs, "", "gone/away"); err == nil {
		t.Error("mine not among the active omakases: want error")
	}
}

// Plan attributes each pending action to the omakase behind it: list items to
// the first declarer, scalars to the last (the merge winner).
func TestPlanProvenance(t *testing.T) {
	oms := []Omakase{
		{Name: "someone/base", Manifest: &Manifest{
			Packages: Packages{Aur: []string{"foo", "shared"}},
			Omarchy:  Omarchy{Font: "Base Font"},
		}},
		{Name: "me/setup", Manifest: &Manifest{
			Packages: Packages{Aur: []string{"bar", "shared"}},
			Omarchy:  Omarchy{Font: "My Font"},
		}},
	}
	have := &State{
		Aur: map[string]bool{}, Pacman: map[string]bool{}, Provides: map[string]bool{},
		OmarchyPlugins: map[string]InstalledOmarchyPlugin{}, HerdrPlugins: map[string]bool{},
	}
	actions, _ := Plan(oms, "h", have)

	byOm := map[string]string{}
	var font string
	for _, a := range actions {
		switch a.Kind {
		case "aur":
			byOm[a.Omakase] = a.Desc
		case "font":
			font = a.Omakase
		}
	}
	if byOm["me/setup"] != "install bar" {
		t.Errorf("me/setup: got %q", byOm["me/setup"])
	}
	if byOm["someone/base"] != "install foo shared" {
		t.Errorf("someone/base gets shared (first declarer): got %q", byOm["someone/base"])
	}
	if font != "me/setup" {
		t.Errorf("font goes to the merge winner: got %q", font)
	}
}

func names(rs []Omakase) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Name)
	}
	return out
}

// A use: entry with only: takes just the items it names, and nothing else
// from that omakase.
func TestUseOnlyTakesNamedItems(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "big", ManifestFile), `
packages:
  aur: [kitty, ghostty]
  pacman: [tmux]
omarchy:
  font: Their Font
files:
  files/kitty.conf: ~/.config/kitty/kitty.conf
  files/nvim.lua: ~/.config/nvim/init.lua
`)
	writeFile(t, filepath.Join(root, "mine", ManifestFile), `
use:
  - source: ../big
    only:
      packages.aur: [kitty]
      files: [files/kitty.conf]
packages:
  aur: [my-tool]
`)
	out := resolve(t, filepath.Join(root, "mine", ManifestFile))
	if len(out) != 2 || out[0].Name != "big" {
		t.Fatalf("got %v", names(out))
	}
	o := out[0].Resolve("h")
	if got := o.Packages.Aur; len(got) != 1 || got[0] != "kitty" {
		t.Errorf("packages.aur: got %v, want [kitty]", got)
	}
	if len(o.Packages.Pacman) != 0 {
		t.Errorf("packages.pacman was not selected: got %v", o.Packages.Pacman)
	}
	if o.Omarchy.Font != "" {
		t.Errorf("omarchy.font was not selected: got %q", o.Omarchy.Font)
	}
	if len(o.Files) != 1 || o.Files["files/kitty.conf"] == "" {
		t.Errorf("files: got %v", o.Files)
	}
	if out[1].Resolve("h").Packages.Aur[0] != "my-tool" {
		t.Errorf("the declaring omakase is untouched: got %v", out[1].Resolve("h"))
	}
}

// only: addresses the manifest by dotted path: a leaf's list names its own
// entries, a section's list names the sub-keys to descend into, and the most
// specific path that matches decides.
func TestSelectionPaths(t *testing.T) {
	sel := Selection{"packages": {"aur"}, "omarchy.defaults": {"agent"}, "herdr.plugins": nil}
	for _, c := range []struct {
		path, item string
		want       bool
	}{
		{"packages.aur", "kitty", true},
		{"packages.pacman", "tmux", false},
		{"omarchy.defaults.agent", "", true},
		{"omarchy.defaults.editor", "", false},
		{"omarchy.font", "", false},
		{"herdr.plugins", "anything", true},
		{"files", "files/x", false},
	} {
		if got := sel.keeps(c.path, c.item); got != c.want {
			t.Errorf("keeps(%q, %q) = %v, want %v", c.path, c.item, got, c.want)
		}
	}
	if err := (Selection{"packages.typo": nil}).check(); err == nil {
		t.Error("an unknown only: path must be rejected")
	}
	if err := (Selection{"omarchy": nil}).check(); err != nil {
		t.Errorf("a section path is a valid only: key: %v", err)
	}
}

// An only: governs the whole chain it pulls in, so cherry-picking one package
// never drags the used omakase's own dependencies in behind it.
func TestUseOnlyNarrowsTheWholeChain(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "deep", ManifestFile), "packages:\n  aur: [kitty, junk]\n")
	writeFile(t, filepath.Join(root, "big", ManifestFile), "use: [../deep]\npackages:\n  aur: [other]\n")
	writeFile(t, filepath.Join(root, "mine", ManifestFile), `
use:
  - source: ../big
    only:
      packages.aur: [kitty]
`)
	out := resolve(t, filepath.Join(root, "mine", ManifestFile))
	var got []string
	for _, r := range out {
		got = append(got, r.Resolve("h").Packages.Aur...)
	}
	if len(got) != 1 || got[0] != "kitty" {
		t.Errorf("only kitty crosses over: got %v from %v", got, names(out))
	}
}

// A part a filtered use: takes nothing from drops out instead of sitting in
// list doing nothing.
func TestUseOnlyDropsEmptyParts(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "split", ManifestFile), "parts: [kitty, nvim]\n")
	writeFile(t, filepath.Join(root, "split", "kitty", ManifestFile), "packages:\n  aur: [kitty]\n")
	writeFile(t, filepath.Join(root, "split", "nvim", ManifestFile), "packages:\n  aur: [neovim]\n")
	writeFile(t, filepath.Join(root, "mine", ManifestFile), `
use:
  - source: ../split
    only:
      packages.aur: [kitty]
`)
	out := resolve(t, filepath.Join(root, "mine", ManifestFile))
	if len(out) != 2 || out[0].Name != "split/kitty" || out[1].Name != "mine" {
		t.Fatalf("got %v", names(out))
	}
}

// Reaching the same omakase twice widens what is taken from it rather than
// dropping the second use.
func TestUseOnlyWidensOnSecondUse(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "big", ManifestFile), "packages:\n  aur: [a, b, c]\n")
	writeFile(t, filepath.Join(root, "one", ManifestFile), "use:\n  - source: ../big\n    only: {packages.aur: [a]}\n")
	writeFile(t, filepath.Join(root, "two", ManifestFile), "use:\n  - source: ../big\n    only: {packages.aur: [b]}\n")

	one, err := omakaseFromDir(filepath.Join(root, "one", ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	two, err := omakaseFromDir(filepath.Join(root, "two", ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	out, err := resolveUses(append(one, two...))
	if err != nil {
		t.Fatal(err)
	}
	if got := out[0].Resolve("h").Packages.Aur; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("both selections apply: got %v", got)
	}
}

// A bare use: entry round-trips as a bare string, so export never rewrites a
// hand-written manifest into the long form.
func TestUseRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ManifestFile)
	writeFile(t, path, "use:\n  - polidog/omakase\n  - source: someone/big\n    only: {files: [files/x]}\n")
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Use) != 2 || m.Use[0].Source != "polidog/omakase" || m.Use[0].Only != nil {
		t.Fatalf("parse: got %+v", m.Use)
	}
	if got := m.Use[1].Only["files"]; len(got) != 1 || got[0] != "files/x" {
		t.Fatalf("only: got %+v", m.Use[1])
	}
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}
	again, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Use) != 2 || again.Use[0].Source != "polidog/omakase" || again.Use[0].Only != nil {
		t.Errorf("round trip: got %+v", again.Use)
	}
	if got := again.Use[1].Only["files"]; len(got) != 1 || got[0] != "files/x" {
		t.Errorf("round trip only: got %+v", again.Use[1])
	}
}

func resolve(t *testing.T, manifestPath string) []Omakase {
	t.Helper()
	rs, err := omakaseFromDir(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	out, err := resolveUses(rs)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
