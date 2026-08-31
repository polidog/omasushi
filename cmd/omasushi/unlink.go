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
// were not recorded). Returns the destinations it unlinked.
func Unlink(omakases []Omakase, host string, dryRun bool) (undone []string, err error) {
	var links []link
	agent := resolveAgent(omakases, host)
	for _, r := range omakases {
		links = append(links, omakaseLinks(r, r.Manifest.Resolve(host), agent)...)
	}
	herdrDir := filepath.Join(expandHome("~"), ".config/herdr") + string(filepath.Separator)
	hyprDir := filepath.Join(expandHome("~"), ".config/hypr") + string(filepath.Separator)
	herdrTouched, hyprTouched := false, false
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
