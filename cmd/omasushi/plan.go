package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Action struct {
	Kind string       `json:"kind"` // aur, pacman, font, default-*, omarchy-add, omarchy-enable, herdr-add, herdr-reload, hypr-snippet, hypr-loader, hypr-reload, file-link, skill-link, command-link
	Desc string       `json:"desc"`
	Pack string       `json:"pack,omitempty"`
	Run  func() error `json:"-"`
}

// link is a resolved symlink request: absolute source inside a pack,
// destination with ~ expanded.
type link struct {
	kind, pack, label, src, dst string
}

// Plan diffs the layered packs against the probed state and returns the
// actions needed. It never removes anything; extras are reported separately.
// Each action carries the pack behind it, so the plan can say where a
// pending install comes from when several packs are stacked.
func Plan(packs []Pack, have *State) (actions []Action, extras []string) {
	var want Manifest
	var links []link
	agent := resolveAgent(packs)
	// prov[key] attributes each declared item: the first pack to list it (the
	// item is installed on its account), the last to set a scalar (it wins
	// the merge).
	prov := map[string]string{}
	first := func(key, name string) {
		if _, ok := prov[key]; !ok {
			prov[key] = name
		}
	}
	for _, p := range packs {
		m := *p.Manifest
		for _, pk := range m.Packages.Pacman {
			first("pacman:"+pk, p.Name)
		}
		for _, pk := range m.Packages.Aur {
			first("aur:"+pk, p.Name)
		}
		if m.Omarchy.Font != "" {
			prov["font"] = p.Name
		}
		for kind, v := range map[string]string{
			"default-agent": m.Omarchy.Defaults.Agent, "default-browser": m.Omarchy.Defaults.Browser,
			"default-editor": m.Omarchy.Defaults.Editor, "default-terminal": m.Omarchy.Defaults.Terminal,
		} {
			if v != "" {
				prov[kind] = p.Name
			}
		}
		for _, pl := range m.Omarchy.Plugins {
			first("omarchy:"+normalizeGitURL(pl.URL), p.Name)
		}
		for _, pl := range m.Herdr.Plugins {
			first("herdr:"+pl.Source, p.Name)
		}
		want = want.merge(m)
		links = append(links, packLinks(p, agent)...)
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
	// One install action per declaring pack, so a stacked plan reads
	// "install foo bar <- someone/packs/x" rather than one anonymous batch.
	installs := func(kind, cmd string, missing []string) {
		byPack := map[string][]string{}
		var names []string
		for _, p := range missing {
			n := prov[kind+":"+p]
			if _, ok := byPack[n]; !ok {
				names = append(names, n)
			}
			byPack[n] = append(byPack[n], p)
		}
		sort.Strings(names)
		for _, n := range names {
			pk := byPack[n]
			actions = append(actions, Action{Kind: kind, Pack: n, Desc: fmt.Sprintf("install %s", strings.Join(pk, " ")), Run: func() error {
				return runVisible(cmd, pk...)
			}})
		}
	}
	installs("pacman", "omarchy-pkg-add", pacman)
	installs("aur", "omarchy-pkg-aur-add", aur)

	if f := want.Omarchy.Font; f != "" && f != have.Font {
		actions = append(actions, Action{Kind: "font", Pack: prov["font"], Desc: fmt.Sprintf("%s -> %s", have.Font, f), Run: func() error {
			return runVisible("omarchy-font-set", f)
		}})
	}
	defaults := planDefaults(want.Omarchy.Defaults, have.Defaults)
	for i := range defaults {
		defaults[i].Pack = prov[defaults[i].Kind]
	}
	actions = append(actions, defaults...)

	for _, p := range want.Omarchy.Plugins {
		p := p
		from := prov["omarchy:"+normalizeGitURL(p.URL)]
		inst, ok := have.OmarchyPlugins[normalizeGitURL(p.URL)]
		switch {
		case !ok:
			actions = append(actions, Action{Kind: "omarchy-add", Pack: from, Desc: fmt.Sprintf("add %s (enable=%v)", p.URL, p.Enable), Run: func() error {
				args := []string{p.URL, "--yes"}
				if p.Enable {
					args = append(args, "--enable")
				}
				return runVisible("omarchy-plugin-add", args...)
			}})
		case p.Enable && !inst.Enabled:
			id := inst.ID
			actions = append(actions, Action{Kind: "omarchy-enable", Pack: from, Desc: "enable " + id, Run: func() error {
				return runVisible("omarchy-plugin-enable", id)
			}})
		}
	}

	for _, p := range want.Herdr.Plugins {
		p := p
		if have.HerdrPlugins[p.Source] {
			continue
		}
		actions = append(actions, Action{Kind: "herdr-add", Pack: prov["herdr:"+p.Source], Desc: "install " + p.Source, Run: func() error {
			args := []string{"plugin", "install", p.Source, "--yes"}
			if p.Ref != "" {
				args = append(args, "--ref", p.Ref)
			}
			return runVisible("herdr", args...)
		}})
	}

	herdrTouched, hyprTouched := false, false
	herdrDir := filepath.Join(expandHome("~"), ".config/herdr") + string(filepath.Separator)
	hyprPrefix := hyprDir() + string(filepath.Separator)
	var snippets []string
	for _, l := range links {
		l := l
		if l.kind == "hypr-snippet" {
			snippets = append(snippets, l.dst)
		}
		if cur, err := os.Readlink(l.dst); err == nil && cur == l.src {
			continue
		}
		actions = append(actions, Action{Kind: l.kind, Pack: l.pack, Desc: l.label, Run: func() error {
			return linkFile(l.src, l.dst)
		}})
		if strings.HasPrefix(l.dst, herdrDir) {
			herdrTouched = true
		}
		if strings.HasPrefix(l.dst, hyprPrefix) {
			hyprTouched = true
		}
	}
	// The loader lists what is in omasushi.d once the links above are made:
	// the packs' snippets plus whatever was already there.
	if hyprLoaderPending(union(hyprSnippetsOnDisk(), snippets)) {
		actions = append(actions, Action{Kind: "hypr-loader", Desc: tildify(hyprLoaderPath()) + " (loaded from hyprland.lua)", Run: writeHyprLoader})
		hyprTouched = true
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
	if hyprTouched {
		// Linking a config Hyprland has already read changes nothing until it
		// rereads it, so keybindings would look like they simply did not apply.
		// Best effort: there is no compositor to talk to outside a session.
		actions = append(actions, Action{Kind: "hypr-reload", Desc: "hyprctl reload", Run: func() error {
			if err := runVisible("hyprctl", "reload"); err != nil {
				fmt.Println("  (no Hyprland session; config is picked up on next start)")
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

// agentDirs is where each Omarchy default agent looks for skills (a directory
// per skill holding SKILL.md, the format shared by all of them) and for
// prompt-style commands (*.md); "" means the agent has no such place.
var agentDirs = map[string]struct{ skills, commands string }{
	"claude":   {"~/.claude/skills", "~/.claude/commands"},
	"codex":    {"~/.codex/skills", "~/.codex/prompts"},
	"gemini":   {"~/.gemini/skills", ""},
	"copilot":  {"~/.copilot/skills", ""},
	"opencode": {"~/.config/opencode/skill", "~/.config/opencode/command"},
}

// resolveAgent decides which agent the agent: section is for: the stacked
// packs' omarchy.defaults.agent (what sync will make the default), else the
// machine's current default, else claude.
func resolveAgent(packs []Pack) string {
	var want Manifest
	for _, p := range packs {
		want = want.merge(*p.Manifest)
	}
	if want.Omarchy.Defaults.Agent != "" {
		return want.Omarchy.Defaults.Agent
	}
	if b, err := os.ReadFile(expandHome("~/.config/omarchy/defaults/agent")); err == nil {
		if a := strings.TrimSpace(string(b)); a != "" {
			return a
		}
	}
	return "claude"
}

// packLinks expands files:, hypr:, claude.{skills,commands} and
// agent.{skills,commands} of one pack into concrete symlinks. Later packs
// override earlier ones for the same destination (handled by order in Plan:
// the last link wins on sync). agent is the resolved default agent, which
// picks the destination of the agent: section.
func packLinks(p Pack, agent string) []link {
	m := p.Manifest
	var out []link
	srcs := make([]string, 0, len(m.Files))
	for s := range m.Files {
		srcs = append(srcs, s)
	}
	sort.Strings(srcs)
	for _, s := range srcs {
		src, _ := filepath.Abs(filepath.Join(p.Dir, s))
		out = append(out, link{"file-link", p.Name, fmt.Sprintf("%s -> %s", s, m.Files[s]), src, expandHome(m.Files[s])})
	}
	if m.Hypr != "" {
		src, _ := filepath.Abs(filepath.Join(p.Dir, m.Hypr))
		dst := filepath.Join(hyprSnippetsDir(), hyprSnippetName(p))
		out = append(out, link{"hypr-snippet", p.Name, fmt.Sprintf("%s -> %s", m.Hypr, tildify(dst)), src, dst})
	}
	out = append(out, agentLinks(p, m.Claude, agentDirs["claude"].skills, agentDirs["claude"].commands)...)
	if m.Agent.Skills != "" || m.Agent.Commands != "" {
		d, ok := agentDirs[agent]
		if !ok {
			fmt.Fprintf(os.Stderr, "%s: agent.skills/commands: no known skills directory for agent %q; skipped\n", p.Name, agent)
		} else {
			if m.Agent.Commands != "" && d.commands == "" {
				fmt.Fprintf(os.Stderr, "%s: agent.commands: %s has no prompt-commands directory; skipped\n", p.Name, agent)
			}
			out = append(out, agentLinks(p, m.Agent, d.skills, d.commands)...)
		}
	}
	return out
}

// agentLinks links each skill directory of c.Skills under skillsDir and each
// *.md of c.Commands under commandsDir (either "" = skip).
func agentLinks(p Pack, c Claude, skillsDir, commandsDir string) []link {
	var out []link
	if c.Skills != "" && skillsDir != "" {
		dir := filepath.Join(p.Dir, c.Skills)
		for _, e := range readDirSorted(dir) {
			if !e.IsDir() {
				continue
			}
			src := filepath.Join(dir, e.Name())
			dst := expandHome(skillsDir + "/" + e.Name())
			out = append(out, link{"skill-link", p.Name, fmt.Sprintf("%s -> %s/%s", filepath.Join(c.Skills, e.Name()), skillsDir, e.Name()), src, dst})
		}
	}
	if c.Commands != "" && commandsDir != "" {
		dir := filepath.Join(p.Dir, c.Commands)
		for _, e := range readDirSorted(dir) {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
				continue
			}
			src := filepath.Join(dir, e.Name())
			dst := expandHome(commandsDir + "/" + e.Name())
			out = append(out, link{"command-link", p.Name, fmt.Sprintf("%s -> %s/%s", filepath.Join(c.Commands, e.Name()), commandsDir, e.Name()), src, dst})
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
