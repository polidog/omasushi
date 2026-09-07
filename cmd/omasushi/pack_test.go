package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSource(t *testing.T) {
	cases := []struct {
		in        string
		repo, sub string
		target    string
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
		if src.Repo != c.repo || src.Sub != c.sub || src.Target != c.target || src.Local {
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

// A repository's packs are its subdirectories with an omasushi.yaml, in name
// order; the root only names the repository.
func TestPacksInRepository(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, ManifestFile), "name: bundle\ndescription: two packs\n")
	writeFile(t, filepath.Join(repo, "kitty", ManifestFile), "files:\n  files/kitty.conf: ~/.config/kitty/kitty.conf\n")
	writeFile(t, filepath.Join(repo, "herdr", ManifestFile), "herdr:\n  plugins:\n    - source: a/b\n")
	writeFile(t, filepath.Join(repo, "files", "x"), "not a pack: no manifest\n")
	writeFile(t, filepath.Join(repo, ".hidden", ManifestFile), "skipped\n")

	ps, err := packsFromDir(repo)
	if err != nil {
		t.Fatal(err)
	}
	if got := names(ps); len(got) != 2 || ps[0].Sub != "herdr" || ps[1].Sub != "kitty" {
		t.Fatalf("got %v", got)
	}
	base := filepath.Base(repo)
	if ps[0].Name != base+"/herdr" || ps[0].Dir != filepath.Join(repo, "herdr") || ps[0].Repo != repo {
		t.Errorf("herdr: %+v", ps[0])
	}
	if len(ps[0].Manifest.Herdr.Plugins) != 1 || len(ps[1].Manifest.Files) != 1 {
		t.Errorf("manifests not loaded per pack: %+v %+v", ps[0].Manifest, ps[1].Manifest)
	}

	// use records the repository as typed, and this machine's file expands it
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	local := &Local{}
	if _, err := local.Add(repo); err != nil {
		t.Fatal(err)
	}
	if len(local.Use) != 1 || local.Use[0] != repo {
		t.Fatalf("use after add: %+v", local.Use)
	}
	loaded, err := activePacks(local, "")
	if err != nil || len(loaded) != 3 || loaded[1].Dir != filepath.Join(repo, "kitty") {
		t.Fatalf("activePacks: %v %+v", err, names(loaded))
	}
	if loaded[2].Name != LocalName {
		t.Errorf("this machine's file is the top layer, got %v", names(loaded))
	}

	// one pack of it
	src, err := parseSource(repo)
	if err != nil {
		t.Fatal(err)
	}
	src.Sub = "kitty"
	one, err := packsIn(src, repo, "")
	if err != nil || len(one) != 1 || one[0].Sub != "kitty" || len(one[0].Manifest.Files) != 1 {
		t.Fatalf("single pack: %v %+v", err, one)
	}
	src.Sub = "nope"
	if _, err := packsIn(src, repo, ""); err == nil {
		t.Error("a pack that is not there: want error")
	}

	// remove one pack of a repository added whole: the siblings stay
	if err := local.Remove(base + "/kitty"); err == nil {
		t.Error("a pack of a local repository cannot be removed on its own")
	}
}

// A repository with no pack subdirectory is itself one pack.
func TestSinglePackRepository(t *testing.T) {
	plain := t.TempDir()
	writeFile(t, filepath.Join(plain, ManifestFile), "packages:\n  aur: [neovim]\n")
	ps, err := packsFromDir(plain)
	if err != nil || len(ps) != 1 || ps[0].Sub != "" || ps[0].Dir != plain {
		t.Fatalf("plain: %v %+v", err, ps)
	}
	if ps[0].Name != filepath.Base(plain) {
		t.Errorf("name: %q", ps[0].Name)
	}
	// -f also takes the manifest path itself
	ps, err = packsFromDir(filepath.Join(plain, ManifestFile))
	if err != nil || len(ps) != 1 || ps[0].Dir != plain {
		t.Fatalf("by file: %v %+v", err, ps)
	}
}

// A root that has packs and declares sections of its own is refused: it
// would be applied by nobody and mislead everybody.
func TestRootWithPacksOnlyNamesItself(t *testing.T) {
	repo := t.TempDir()
	writeFile(t, filepath.Join(repo, ManifestFile), "name: x\npackages:\n  aur: [stray]\n")
	writeFile(t, filepath.Join(repo, "ime", ManifestFile), "packages:\n  pacman: [fcitx5]\n")
	if _, err := packsFromDir(repo); err == nil || !strings.Contains(err.Error(), "ime") {
		t.Errorf("want an error naming the packs, got %v", err)
	}
	// a root with no manifest at all is fine: the packs are the repository
	os.Remove(filepath.Join(repo, ManifestFile))
	ps, err := packsFromDir(repo)
	if err != nil || len(ps) != 1 || ps[0].Sub != "ime" {
		t.Fatalf("no root manifest: %v %+v", err, ps)
	}
}
