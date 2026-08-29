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
		if src.Name != "omakase" && c.in != "https://codeberg.org/a/b/c" {
			t.Errorf("%q: name %q", c.in, src.Name)
		}
	}
	for _, bad := range []string{"", "polidog", "polidog/omakase/../x", "polidog/omakase/.git"} {
		if _, err := parseSource(bad); err == nil {
			t.Errorf("%q: want error", bad)
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

	// use with a local path records one entry per part; use path/part just one
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cfg := &Config{}
	if _, err := cfg.Use(repo); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Omakases) != 2 || cfg.Omakases[1].Part != "kitty" || cfg.Omakases[1].Source != repo {
		t.Fatalf("config after use: %+v", cfg.Omakases)
	}
	loaded, err := LoadOmakases(cfg)
	if err != nil || len(loaded) != 2 || loaded[1].Dir != filepath.Join(repo, "kitty") {
		t.Fatalf("LoadOmakases: %v %+v", err, loaded)
	}
	if err := cfg.Remove(base + "/herdr"); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Omakases) != 1 || cfg.Omakases[0].Part != "kitty" {
		t.Fatalf("config after remove: %+v", cfg.Omakases)
	}

	// a pre-split entry (no part) for a repo that now has parts expands on load
	legacy := &Config{Omakases: []OmakaseRef{{Name: base, Source: repo}}}
	loaded, err = LoadOmakases(legacy)
	if err != nil || len(loaded) != 2 {
		t.Fatalf("legacy expand: %v %+v", err, loaded)
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
