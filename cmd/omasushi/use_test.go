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
