package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Unlink is the reverse of the link half of sync: every symlink an omakase
// put in place is removed, and a file that sync moved aside as .bak is put
// back. Only links that point at the omakase are touched, so something the
// user re-pointed by hand is left alone. Packages, plugins, font and defaults
// are never undone (the tool never uninstalls, and the previous font/defaults
// were not recorded). Destinations with no .bak leave a hole rather than a
// restored file, so they are called out at the end. Returns the destinations
// it unlinked.
func Unlink(omakases []Omakase, host string, dryRun bool) (undone []string, err error) {
	var links []link
	agent := resolveAgent(omakases, host)
	for _, r := range omakases {
		links = append(links, omakaseLinks(r, r.Manifest.Resolve(host), agent)...)
	}
	herdrDir := filepath.Join(expandHome("~"), ".config/herdr") + string(filepath.Separator)
	hyprDir := filepath.Join(expandHome("~"), ".config/hypr") + string(filepath.Separator)
	herdrTouched, hyprTouched := false, false
	var noRestore []string
	for _, l := range links {
		cur, rerr := os.Readlink(l.dst)
		if rerr != nil || cur != l.src {
			continue
		}
		bak := l.dst + ".bak"
		_, hasBak := os.Lstat(bak)
		desc := "rm " + l.dst
		if hasBak == nil {
			desc += " (restore .bak)"
		} else {
			noRestore = append(noRestore, l.dst)
		}
		fmt.Printf("==> %s: %s\n", l.kind, desc)
		if !dryRun {
			if err := os.Remove(l.dst); err != nil {
				return undone, err
			}
			if hasBak == nil {
				if err := os.Rename(bak, l.dst); err != nil {
					return undone, err
				}
			}
		}
		undone = append(undone, l.dst)
		if strings.HasPrefix(l.dst, herdrDir) {
			herdrTouched = true
		}
		if strings.HasPrefix(l.dst, hyprDir) {
			hyprTouched = true
		}
	}
	warnNoRestore(noRestore, dryRun)
	if dryRun {
		return undone, nil
	}
	if herdrTouched {
		if err := runVisible("herdr", "server", "reload-config"); err != nil {
			fmt.Println("  (herdr not running; config is picked up on next start)")
		}
	}
	if hyprTouched {
		if err := runVisible("hyprctl", "reload"); err != nil {
			fmt.Println("  (no Hyprland session; config is picked up on next start)")
		}
	}
	return undone, nil
}

// warnNoRestore names the destinations that were only the omakase's file: the
// link is gone and nothing took its place. Usually that is what the omakase
// added and nobody misses, but some of these are files the rest of a config
// still reads — ~/.config/hypr/bindings.lua is required by Omarchy's
// hyprland.lua, and removing it makes the next `hyprctl reload` fail — so
// point at the copy Omarchy ships when there is one.
func warnNoRestore(dsts []string, dryRun bool) {
	if len(dsts) == 0 {
		return
	}
	if dryRun {
		fmt.Println("\nnothing to restore for these — no .bak, so they would be left missing:")
	} else {
		fmt.Println("\nnothing to restore for these — no .bak, so the file is now gone:")
	}
	for _, dst := range dsts {
		if def := omarchyDefault(dst); def != "" {
			fmt.Printf("  %s (Omarchy ships one: cp %s %s)\n", dst, def, dst)
		} else {
			fmt.Printf("  %s\n", dst)
		}
	}
}

// omarchyDefault is the file Omarchy seeds dst with on a fresh install, for a
// dst under ~/.config that Omarchy owns. Empty if there is no such default.
func omarchyDefault(dst string) string {
	root := os.Getenv("OMARCHY_PATH")
	if root == "" {
		root = "/usr/share/omarchy"
	}
	cfg := filepath.Join(expandHome("~"), ".config") + string(filepath.Separator)
	if !strings.HasPrefix(dst, cfg) {
		return ""
	}
	def := filepath.Join(root, "config", strings.TrimPrefix(dst, cfg))
	if _, err := os.Stat(def); err != nil {
		return ""
	}
	return def
}
