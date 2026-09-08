package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A pack's hypr: snippet is linked into ~/.config/hypr/omasushi.d/<pack>.lua,
// and ~/.config/hypr/omasushi.lua — written by omasushi — loads every file in
// that directory. hyprland.lua gets one `require("hypr.omasushi")` at the
// end, once, the way hyprmoncfg adds its own line, so it runs after Omarchy's
// defaults and the user's own overrides. Packs thus add bindings without any
// of them owning bindings.lua, and `omarchy font set` rewriting a file in
// place never turns one of these links back into a copy.

const hyprRequire = `require("hypr.omasushi")`

func hyprDir() string         { return filepath.Join(expandHome("~"), ".config/hypr") }
func hyprSnippetsDir() string { return filepath.Join(hyprDir(), "omasushi.d") }
func hyprLoaderPath() string  { return filepath.Join(hyprDir(), "omasushi.lua") }
func hyprlandLuaPath() string { return filepath.Join(hyprDir(), "hyprland.lua") }
func hyprSnippetName(p Pack) string {
	return strings.ReplaceAll(p.Name, "/", "-") + ".lua"
}

// hyprLoaderContent is the loader for the given snippet files, in name
// order. A snippet that has gone missing is skipped rather than breaking the
// whole config, so a stale loader is harmless until the next sync rewrites it.
func hyprLoaderContent(files []string) string {
	var sb strings.Builder
	sb.WriteString("-- Written by omasushi: loads the hypr: snippet of every pack in use, in name order.\n")
	sb.WriteString("-- Do not edit; `omasushi sync` and `omasushi unlink` rewrite it.\n")
	sorted := append([]string{}, files...)
	sort.Strings(sorted)
	for _, f := range sorted {
		fmt.Fprintf(&sb, "do local p = %q; local f = io.open(p, \"r\"); if f then f:close(); dofile(p) end end\n", f)
	}
	return sb.String()
}

// hyprSnippetsOnDisk lists the snippet files currently in omasushi.d.
func hyprSnippetsOnDisk() []string {
	var out []string
	for _, e := range readDirSorted(hyprSnippetsDir()) {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".lua") {
			out = append(out, filepath.Join(hyprSnippetsDir(), e.Name()))
		}
	}
	return out
}

// hyprLoaderPending reports whether the loader needs writing for the
// snippets expected after the links are in place: its content differs, or
// hyprland.lua does not load it yet. With nothing expected and no loader on
// disk there is nothing to do — machines without a hypr: pack are left alone.
func hyprLoaderPending(expected []string) bool {
	cur, err := os.ReadFile(hyprLoaderPath())
	if len(expected) == 0 && os.IsNotExist(err) {
		return false
	}
	if err != nil || string(cur) != hyprLoaderContent(expected) {
		return true
	}
	return !hyprlandLoads()
}

// hyprlandLoads reports whether hyprland.lua requires the loader (or does
// not exist, in which case there is nothing to add it to).
func hyprlandLoads() bool {
	b, err := os.ReadFile(hyprlandLuaPath())
	if err != nil {
		return true
	}
	return strings.Contains(string(b), "hypr.omasushi")
}

// writeHyprLoader rewrites the loader from the snippets on disk and makes
// sure hyprland.lua loads it. Called after the snippet links are made (sync)
// or removed (unlink), so the directory is the truth at that point.
func writeHyprLoader() error {
	if err := os.MkdirAll(hyprDir(), 0o755); err != nil {
		return err
	}
	content := hyprLoaderContent(hyprSnippetsOnDisk())
	if err := os.WriteFile(hyprLoaderPath(), []byte(content), 0o644); err != nil {
		return err
	}
	fmt.Printf("  wrote %s\n", tildify(hyprLoaderPath()))
	if hyprlandLoads() {
		return nil
	}
	b, err := os.ReadFile(hyprlandLuaPath())
	if err != nil {
		return err
	}
	s := string(b)
	if s != "" && !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	s += "\n-- Added by omasushi: the hypr: snippets of the packs in use, loaded last.\n" + hyprRequire + "\n"
	fmt.Printf("  added %s to %s\n", hyprRequire, tildify(hyprlandLuaPath()))
	return os.WriteFile(hyprlandLuaPath(), []byte(s), 0o644)
}
