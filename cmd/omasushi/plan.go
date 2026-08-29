package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Action struct {
	Kind   string       `json:"kind"` // aur, pacman, font, default-*, omarchy-add, omarchy-enable, herdr-add, herdr-reload, file-link, skill-link, command-link
	Desc   string       `json:"desc"`
	Recipe string       `json:"recipe,omitempty"`
	Run    func() error `json:"-"`
}

// link is a resolved symlink request: absolute source inside a recipe,
// destination with ~ expanded.
type link struct {
	kind, recipe, label, src, dst string
}

// Plan diffs the layered recipes against the probed state and returns the
// actions needed. It never removes anything; extras are reported separately.
func Plan(recipes []Recipe, host string, have *State) (actions []Action, extras []string) {
	var want Overlay
	var links []link
	for _, r := range recipes {
		o := r.Manifest.Resolve(host)
		want = want.merge(o)
		links = append(links, recipeLinks(r, o)...)
	}

	var aur, pacman []string
	for _, p := range want.Packages.Aur {
		if !have.Aur[p] && !have.Pacman[p] && !have.Provides[p] {
			aur = append(aur, p)
		}
	}
	for _, p := range want.Packages.Pacman {
		if !have.Pacman[p] && !have.Aur[p] && !have.Provides[p] {
			pacman = append(pacman, p)
		}
	}
	if len(pacman) > 0 {
		pk := pacman
		actions = append(actions, Action{Kind: "pacman", Desc: fmt.Sprintf("install %s", strings.Join(pk, " ")), Run: func() error {
			return runVisible("omarchy-pkg-add", pk...)
		}})
	}
	if len(aur) > 0 {
		pk := aur
		actions = append(actions, Action{Kind: "aur", Desc: fmt.Sprintf("install %s", strings.Join(pk, " ")), Run: func() error {
			return runVisible("omarchy-pkg-aur-add", pk...)
		}})
	}

	if f := want.Omarchy.Font; f != "" && f != have.Font {
		actions = append(actions, Action{Kind: "font", Desc: fmt.Sprintf("%s -> %s", have.Font, f), Run: func() error {
			return runVisible("omarchy-font-set", f)
		}})
	}
	actions = append(actions, planDefaults(want.Omarchy.Defaults, have.Defaults)...)

	for _, p := range want.Omarchy.Plugins {
		p := p
		inst, ok := have.OmarchyPlugins[normalizeGitURL(p.URL)]
		switch {
		case !ok:
			actions = append(actions, Action{Kind: "omarchy-add", Desc: fmt.Sprintf("add %s (enable=%v)", p.URL, p.Enable), Run: func() error {
				args := []string{p.URL, "--yes"}
				if p.Enable {
					args = append(args, "--enable")
				}
				return runVisible("omarchy-plugin-add", args...)
			}})
		case p.Enable && !inst.Enabled:
			id := inst.ID
			actions = append(actions, Action{Kind: "omarchy-enable", Desc: "enable " + id, Run: func() error {
				return runVisible("omarchy-plugin-enable", id)
			}})
		}
	}

	for _, p := range want.Herdr.Plugins {
		p := p
		if have.HerdrPlugins[p.Source] {
			continue
		}
		actions = append(actions, Action{Kind: "herdr-add", Desc: "install " + p.Source, Run: func() error {
			args := []string{"plugin", "install", p.Source, "--yes"}
			if p.Ref != "" {
				args = append(args, "--ref", p.Ref)
			}
			return runVisible("herdr", args...)
		}})
	}

	herdrTouched := false
	herdrDir := filepath.Join(expandHome("~"), ".config/herdr") + string(filepath.Separator)
	for _, l := range links {
		l := l
		if cur, err := os.Readlink(l.dst); err == nil && cur == l.src {
			continue
		}
		actions = append(actions, Action{Kind: l.kind, Recipe: l.recipe, Desc: l.label, Run: func() error {
			return linkFile(l.src, l.dst)
		}})
		if strings.HasPrefix(l.dst, herdrDir) {
			herdrTouched = true
		}
	}
	if herdrTouched {
		// Best effort: the server may not be running on a fresh machine.
		actions = append(actions, Action{Kind: "herdr-reload", Desc: "herdr server reload-config", Run: func() error {
			if err := runVisible("herdr", "server", "reload-config"); err != nil {
				fmt.Println("  (herdr not running; config is picked up on next start)")
			}
			return nil
		}})
	}

	wantAur := map[string]bool{}
	for _, p := range want.Packages.Aur {
		wantAur[p] = true
	}
	for p := range have.Aur {
		if !wantAur[p] {
			extras = append(extras, "packages.aur: "+p)
		}
	}
	wantOP := map[string]bool{}
	for _, p := range want.Omarchy.Plugins {
		wantOP[normalizeGitURL(p.URL)] = true
	}
	for k, p := range have.OmarchyPlugins {
		if !wantOP[k] {
			extras = append(extras, "omarchy.plugins: "+p.URL)
		}
	}
	wantHP := map[string]bool{}
	for _, p := range want.Herdr.Plugins {
		wantHP[p.Source] = true
	}
	for s := range have.HerdrPlugins {
		if !wantHP[s] {
			extras = append(extras, "herdr.plugins: "+s)
		}
	}
	sort.Strings(extras)
	return actions, extras
}

// recipeLinks expands files:, claude.skills and claude.commands of one
// recipe into concrete symlinks. Later recipes override earlier ones for the
// same destination (handled by order in Plan: the last link wins on apply).
func recipeLinks(r Recipe, o Overlay) []link {
	var out []link
	srcs := make([]string, 0, len(o.Files))
	for s := range o.Files {
		srcs = append(srcs, s)
	}
	sort.Strings(srcs)
	for _, s := range srcs {
		src, _ := filepath.Abs(filepath.Join(r.Dir, s))
		out = append(out, link{"file-link", r.Name, fmt.Sprintf("%s -> %s", s, o.Files[s]), src, expandHome(o.Files[s])})
	}
	if o.Claude.Skills != "" {
		dir := filepath.Join(r.Dir, o.Claude.Skills)
		for _, e := range readDirSorted(dir) {
			if !e.IsDir() {
				continue
			}
			src := filepath.Join(dir, e.Name())
			dst := expandHome("~/.claude/skills/" + e.Name())
			out = append(out, link{"skill-link", r.Name, fmt.Sprintf("%s -> ~/.claude/skills/%s", filepath.Join(o.Claude.Skills, e.Name()), e.Name()), src, dst})
		}
	}
	if o.Claude.Commands != "" {
		dir := filepath.Join(r.Dir, o.Claude.Commands)
		for _, e := range readDirSorted(dir) {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			src := filepath.Join(dir, e.Name())
			dst := expandHome("~/.claude/commands/" + e.Name())
			out = append(out, link{"command-link", r.Name, fmt.Sprintf("%s -> ~/.claude/commands/%s", filepath.Join(o.Claude.Commands, e.Name()), e.Name()), src, dst})
		}
	}
	return out
}

func readDirSorted(dir string) []os.DirEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries
}

// linkFile symlinks src to dst, moving an existing non-link file aside as .bak.
func linkFile(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("source missing: %s", src)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if fi, err := os.Lstat(dst); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(dst); err != nil {
				return err
			}
		} else {
			bak := dst + ".bak"
			fmt.Printf("  backing up %s -> %s\n", dst, bak)
			if err := os.Rename(dst, bak); err != nil {
				return err
			}
		}
	}
	fmt.Printf("  ln -s %s %s\n", src, dst)
	return os.Symlink(src, dst)
}

// planDefaults emits one action per default that differs from the manifest.
func planDefaults(want, have Defaults) []Action {
	var out []Action
	add := func(kind, w, h string, run func() error) {
		if w == "" || w == h {
			return
		}
		out = append(out, Action{Kind: "default-" + kind, Desc: fmt.Sprintf("%s -> %s", h, w), Run: run})
	}
	add("agent", want.Agent, have.Agent, func() error { return setDefaultAgent(want.Agent) })
	add("browser", want.Browser, have.Browser, func() error { return runVisible("omarchy-default-browser", want.Browser) })
	add("editor", want.Editor, have.Editor, func() error { return runVisible("omarchy-default-editor", want.Editor) })
	add("terminal", want.Terminal, have.Terminal, func() error { return runVisible("omarchy-default-terminal", want.Terminal) })
	return out
}

// setDefaultAgent mirrors omarchy-default-agent without exec'ing the agent
// afterwards: install via mise, then record the choice.
func setDefaultAgent(agent string) error {
	pkg := map[string]string{
		"omp":  "github:can1357/oh-my-pi",
		"grok": "npm:@xai-official/grok",
	}[agent]
	if pkg == "" {
		pkg = agent
	}
	if err := runVisible("mise", "use", "-g", pkg); err != nil {
		return err
	}
	f := filepath.Join(expandHome("~"), ".config/omarchy/defaults/agent")
	if err := os.MkdirAll(filepath.Dir(f), 0o755); err != nil {
		return err
	}
	return os.WriteFile(f, []byte(agent+"\n"), 0o644)
}
