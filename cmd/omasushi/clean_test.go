package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// capture what f writes to stdout.
func capture(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()
	f()
	w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

// A link whose .bak was restored is quiet; one with nothing behind it is named,
// with Omarchy's own copy of the file when there is one to copy back.
func TestUnlinkWarnsWhenNothingToRestore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	omarchy := t.TempDir()
	t.Setenv("OMARCHY_PATH", omarchy)

	dir := t.TempDir()
	for _, p := range []string{"files/hypr/bindings.lua", "files/kitty/kitty.conf"} {
		os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(dir, p), []byte("x"), 0o644)
	}
	bindings := filepath.Join(home, ".config/hypr/bindings.lua")
	kitty := filepath.Join(home, ".config/kitty/kitty.conf")
	for _, dst := range []string{bindings, kitty} {
		os.MkdirAll(filepath.Dir(dst), 0o755)
	}
	os.Symlink(filepath.Join(dir, "files/hypr/bindings.lua"), bindings)
	os.Symlink(filepath.Join(dir, "files/kitty/kitty.conf"), kitty)
	os.WriteFile(kitty+".bak", []byte("mine"), 0o644) // kitty has something to go back to
	os.MkdirAll(filepath.Join(omarchy, "config/hypr"), 0o755)
	os.WriteFile(filepath.Join(omarchy, "config/hypr/bindings.lua"), []byte("default"), 0o644)

	r := Omakase{Name: "t", Dir: dir, Manifest: &Manifest{Files: map[string]string{
		"files/hypr/bindings.lua": "~/.config/hypr/bindings.lua",
		"files/kitty/kitty.conf":  "~/.config/kitty/kitty.conf",
	}}}

	out := capture(t, func() {
		if _, err := Unlink([]Omakase{r}, "", false); err != nil {
			t.Fatal(err)
		}
	})

	if !strings.Contains(out, "nothing to restore") || !strings.Contains(out, bindings) {
		t.Errorf("bindings.lua left no file behind but was not warned about:\n%s", out)
	}
	if strings.Contains(out, "nothing to restore for these") && strings.Contains(out[strings.Index(out, "nothing to restore"):], kitty) {
		t.Errorf("kitty.conf was restored from .bak, so it should not be warned about:\n%s", out)
	}
	if want := "cp " + filepath.Join(omarchy, "config/hypr/bindings.lua"); !strings.Contains(out, want) {
		t.Errorf("want the Omarchy default pointed at (%s):\n%s", want, out)
	}
	if _, err := os.Lstat(bindings); !os.IsNotExist(err) {
		t.Errorf("bindings.lua should be gone, not restored")
	}
	if b, _ := os.ReadFile(kitty); string(b) != "mine" {
		t.Errorf("kitty.conf should be back from .bak, got %q", b)
	}
}
