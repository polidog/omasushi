package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSource(t *testing.T) {
	cases := []struct {
		in         string
		repo, part string
		target     string
	}{
		{"polidog/omakase", "polidog/omakase", "", "https://github.com/polidog/omakase.git"},
		{"polidog/omakase/herdr", "polidog/omakase", "herdr", "https://github.com/polidog/omakase.git"},
		{"polidog/omakase/apps/kitty", "polidog/omakase", "apps/kitty", "https://github.com/polidog/omakase.git"},
		{"https://github.com/polidog/omakase.git", "https://github.com/polidog/omakase.git", "", "https://github.com/polidog/omakase.git"},
		{"https://github.com/polidog/omakase/herdr", "https://github.com/polidog/omakase.git", "herdr", "https://github.com/polidog/omakase.git"},
		{"git@gitlab.com:polidog/omakase/herdr", "https://gitlab.com/polidog/omakase.git", "herdr", "https://gitlab.com/polidog/omakase.git"},
		{"https://codeberg.org/a/b/c", "https://codeberg.org/a/b/c", "", "https://codeberg.org/a/b/c"},
	}
	for _, c := range cases {
		src, err := parseSource(c.in)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if src.Repo != c.repo || src.Part != c.part || src.Target != c.target || src.Local {
			t.Errorf("%q: got %+v", c.in, src)
		}
		wantName := "polidog/omakase"
		if c.in == "https://codeberg.org/a/b/c" {
			wantName = "a/b/c"
		}
		if src.Name != wantName {
			t.Errorf("%q: name %q, want %q", c.in, src.Name, wantName)
		}
	}
	for _, bad := range []string{"", "polidog", "polidog/omakase/../x", "polidog/omakase/.git"} {
		if _, err := parseSource(bad); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}

func TestRepoPath(t *testing.T) {
	cases := map[string]string{
		"https://github.com/polidog/omasushi.git": "polidog/omasushi",
		"git@github.com:Polidog/omasushi":         "polidog/omasushi",
		"ssh://git@gitlab.com/a/b.git":            "a/b",
		"https://codeberg.org/a/b/c":              "a/b/c",
	}
	for in, want := range cases {
		if got := repoPath(in); got != want {
			t.Errorf("repoPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPartsExpandAndConfig(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, ManifestFile), "name: bundle\nparts: [herdr, kitty]\n")
	writeFile(t, filepath.Join(repo, "herdr", ManifestFile), "herdr:\n  plugins:\n    - source: a/b\n")
	writeFile(t, filepath.Join(repo, "kitty", ManifestFile), "files:\n  files/kitty.conf: ~/.config/kitty/kitty.conf\n")

	// -f / cwd mode: the root expands to its parts
	rs, err := omakaseFromDir(filepath.Join(repo, ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].Part != "herdr" || rs[1].Part != "kitty" {
		t.Fatalf("got %+v", rs)
	}
	base := filepath.Base(repo)
	if rs[0].Name != base+"/herdr" || rs[0].Dir != filepath.Join(repo, "herdr") || rs[0].Repo != repo {
		t.Errorf("herdr: %+v", rs[0])
	}
	if len(rs[0].Manifest.Herdr.Plugins) != 1 || len(rs[1].Manifest.Files) != 1 {
		t.Errorf("manifests not loaded per part: %+v %+v", rs[0].Manifest, rs[1].Manifest)
	}

	// use records the repository as the user typed it, and loading the machine
	// manifest expands it into its parts
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	machine := &Machine{}
	if _, err := machine.Add(repo, false); err != nil {
		t.Fatal(err)
	}
	if len(machine.Use) != 1 || machine.Use[0].Source != repo {
		t.Fatalf("machine manifest after use: %+v", machine.Use)
	}
	loaded, err := activeOmakases(machine, "")
	if err != nil || len(loaded) != 3 || loaded[1].Dir != filepath.Join(repo, "kitty") {
		t.Fatalf("activeOmakases: %v %+v", err, names(loaded))
	}
	if loaded[2].Name != MachineName {
		t.Errorf("the machine is the top layer, got %v", names(loaded))
	}

	// a plain repository without parts still loads as one omakase
	plain := t.TempDir()
	writeFile(t, filepath.Join(plain, ManifestFile), "name: plain\n")
	rs, err = omakaseFromDir(filepath.Join(plain, ManifestFile))
	if err != nil || len(rs) != 1 || rs[0].Part != "" || rs[0].Dir != plain {
		t.Fatalf("plain: %v %+v", err, rs)
	}

	// a part that is declared but missing is an error
	writeFile(t, filepath.Join(repo, ManifestFile), "parts: [herdr, nope]\n")
	if _, err := omakaseFromDir(filepath.Join(repo, ManifestFile)); err == nil {
		t.Error("missing part: want error")
	}
}

func TestInlineParts(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, ManifestFile), `name: bundle
parts:
  fonts:
    packages: { aur: [ttf-udev-gothic] }
    omarchy: { font: UDEV Gothic NF }
  kitty:
    files:
      files/kitty/kitty.conf: ~/.config/kitty/kitty.conf
  herdr:
`)
	writeFile(t, filepath.Join(repo, "herdr", ManifestFile), "herdr:\n  plugins:\n    - source: a/b\n")

	rs, err := omakaseFromDir(filepath.Join(repo, ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 3 {
		t.Fatalf("got %d omakases: %+v", len(rs), rs)
	}
	base := filepath.Base(repo)
	// inline parts keep the repository root as their directory, so files: paths
	// are relative to it; a part with an empty value is still a directory part
	for i, want := range []struct{ name, dir string }{
		{base + "/fonts", repo},
		{base + "/kitty", repo},
		{base + "/herdr", filepath.Join(repo, "herdr")},
	} {
		if rs[i].Name != want.name || rs[i].Dir != want.dir {
			t.Errorf("part %d: got name=%q dir=%q, want name=%q dir=%q", i, rs[i].Name, rs[i].Dir, want.name, want.dir)
		}
	}
	if rs[0].Manifest.Omarchy.Font != "UDEV Gothic NF" || len(rs[1].Manifest.Files) != 1 || len(rs[2].Manifest.Herdr.Plugins) != 1 {
		t.Errorf("sections not split per part: %+v", rs)
	}
	if rs[0].Root == nil || rs[2].Root != nil {
		t.Errorf("Root set wrong: inline=%v dir=%v", rs[0].Root, rs[2].Root)
	}

	// one inline part on its own
	src, err := parseSource(repo)
	if err != nil {
		t.Fatal(err)
	}
	src.Part = "kitty"
	one, err := omakasesIn(src, repo, "")
	if err != nil || len(one) != 1 || one[0].Part != "kitty" || len(one[0].Manifest.Files) != 1 {
		t.Fatalf("single inline part: %v %+v", err, one)
	}

	// saving an inline part rewrites the whole root manifest, keeping its siblings
	rs[0].Manifest.Packages.Aur = append(rs[0].Manifest.Packages.Aur, "ttf-hackgen")
	if err := rs[0].Save(); err != nil {
		t.Fatal(err)
	}
	back, err := LoadManifest(filepath.Join(repo, ManifestFile))
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Parts.Names; len(got) != 3 || got[0] != "fonts" || got[2] != "herdr" {
		t.Fatalf("part order lost: %+v", got)
	}
	if len(back.Parts.Inline["fonts"].Packages.Aur) != 2 {
		t.Errorf("export not folded back: %+v", back.Parts.Inline["fonts"].Packages)
	}
	if _, ok := back.Parts.Inline["herdr"]; ok {
		t.Error("directory part should not gain an inline body")
	}
	if len(back.Parts.Inline["kitty"].Files) != 1 {
		t.Error("sibling part lost on save")
	}

	// a part cannot declare parts of its own
	writeFile(t, filepath.Join(repo, ManifestFile), "parts:\n  a:\n    parts: [b]\n")
	if _, err := omakaseFromDir(filepath.Join(repo, ManifestFile)); err == nil {
		t.Error("nested parts: want error")
	}
}
