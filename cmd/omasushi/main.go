package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var version = "dev"

func usage() {
	fmt.Fprintln(os.Stderr, `omasushi — share your Omarchy setup as an omakase repository

usage: omasushi [-f omasushi.yaml] [-H host] <command> [args]

omakases:
  use <owner/repo[/part]|url|path>
                              add an omakase (clone it, or point at a local dir);
                              owner/repo takes every part of a split repository,
                              owner/repo/herdr just that one
  list                        show omakases in use (name, source, checkout)
  update                      git pull every remote omakase
  remove <name>               forget an omakase (unlinks its files, deletes
                              its managed checkout)
  init [dir]                  scaffold a new omakase repository
  publish [<name>|<repo>|<path>]
                              put an omakase on omasushi-web (POSTs its repo
                              URL to the site's API; --browser uses the form)

machine:
  status [--json]             where am I: omakases, their git state, this
                              machine's setup, and how far apart they are
  plan [--json]               show what apply would do
  apply                       install missing packages/plugins, link files/skills
  clean [<name>] [--dry-run]  undo apply's links: remove the symlinks and put
                              the .bak originals back (packages stay installed)
  export [--to <omakase>] [--host <name>]
                              record this machine's installed packages/plugins
                              into an omakase (--host writes under hosts.<name>)
  version

-f path      use a single manifest instead of the configured omakases
             (defaults to ./omasushi.yaml when no omakase is configured)
-H host      resolve hosts.<host> overlays as if running on that machine`)
	os.Exit(2)
}

func main() {
	file := flag.String("f", "", "manifest path (single-omakase mode)")
	host := flag.String("H", "", "hostname to resolve (default: this machine)")
	flag.Usage = usage
	flag.Parse()
	if flag.NArg() < 1 {
		usage()
	}
	if *host == "" {
		*host, _ = os.Hostname()
	}
	cmd, args := flag.Arg(0), flag.Args()[1:]

	cfg, err := LoadConfig()
	die(err)

	switch cmd {
	case "version":
		fmt.Println("omasushi", version)
		return
	case "init":
		dir := "."
		if len(args) > 0 {
			dir = args[0]
		}
		die(initOmakase(dir))
		return
	case "use":
		if len(args) != 1 {
			usage()
		}
		rs, err := cfg.Use(args[0])
		die(err)
		for _, r := range rs {
			fmt.Printf("using %s from %s (%s)\n", r.Name, r.Source, tildify(r.Dir))
		}
		return
	case "remove":
		if len(args) != 1 {
			usage()
		}
		omakases, err := LoadOmakases(cfg)
		die(err)
		for _, r := range omakases {
			if r.Name == args[0] {
				_, err := Unlink([]Omakase{r}, *host, false)
				die(err)
			}
		}
		die(cfg.Remove(args[0]))
		fmt.Println("removed", args[0])
		return
	case "publish":
		die(publishCmd(cfg, *file, args))
		return
	}

	omakases, err := activeOmakases(cfg, *file)
	die(err)

	switch cmd {
	case "list":
		if len(omakases) == 0 {
			fmt.Println("no omakases in use (try: omasushi use owner/repo)")
		}
		for _, r := range omakases {
			kind := "git"
			if r.Local {
				kind = "local"
			}
			fmt.Printf("%-28s %-6s %-44s %s\n", r.Name, kind, r.Source, tildify(r.Dir))
		}
	case "update":
		die(Update(omakases))
	case "status":
		fs := flag.NewFlagSet("status", flag.ExitOnError)
		asJSON := fs.Bool("json", false, "machine readable output")
		fs.Parse(args)
		have, err := Probe()
		die(err)
		st := gatherStatus(omakases, *host, have)
		if *asJSON {
			printStatusJSON(st)
		} else {
			printStatus(st)
		}
	case "plan":
		fs := flag.NewFlagSet("plan", flag.ExitOnError)
		asJSON := fs.Bool("json", false, "machine readable output")
		fs.Parse(args)
		have, err := Probe()
		die(err)
		actions, extras := Plan(omakases, *host, have)
		if *asJSON {
			printPlanJSON(omakases, actions, extras)
		} else {
			printPlan(actions, extras)
		}
	case "apply":
		have, err := Probe()
		die(err)
		actions, _ := Plan(omakases, *host, have)
		if len(actions) == 0 {
			fmt.Println("up to date")
			return
		}
		for _, a := range actions {
			fmt.Printf("==> %s: %s\n", a.Kind, a.Desc)
			die(a.Run())
		}
	case "clean":
		fs := flag.NewFlagSet("clean", flag.ExitOnError)
		dryRun := fs.Bool("dry-run", false, "only show what would be unlinked")
		fs.Parse(args)
		targets := omakases
		if fs.NArg() > 0 {
			t, err := pickOmakase(omakases, fs.Arg(0))
			die(err)
			targets = []Omakase{*t}
		}
		undone, err := Unlink(targets, *host, *dryRun)
		die(err)
		if len(undone) == 0 {
			fmt.Println("nothing linked")
		}
	case "export":
		fs := flag.NewFlagSet("export", flag.ExitOnError)
		toHost := fs.String("host", "", "write into hosts.<name> overlay")
		to := fs.String("to", "", "omakase to write into (required when several are in use)")
		fs.Parse(args)
		target, err := pickOmakase(omakases, *to)
		die(err)
		have, err := Probe()
		die(err)
		added := export(omakases, target, have, *host, *toHost)
		if len(added) == 0 {
			fmt.Println("nothing new")
			return
		}
		die(target.Save())
		fmt.Printf("wrote %s\n", target.ManifestPath())
		for _, a := range added {
			fmt.Println("+", a)
		}
	default:
		usage()
	}
}

// activeOmakases picks the omakase set: -f wins; otherwise the config; and
// when nothing is configured, an omasushi.yaml in the working directory.
func activeOmakases(cfg *Config, file string) ([]Omakase, error) {
	if file != "" {
		return omakaseFromDir(file)
	}
	if len(cfg.Omakases) > 0 {
		return LoadOmakases(cfg)
	}
	if _, err := os.Stat(ManifestFile); err == nil {
		return omakaseFromDir(ManifestFile)
	}
	return nil, nil
}

func pickOmakase(omakases []Omakase, name string) (*Omakase, error) {
	switch {
	case len(omakases) == 0:
		return nil, fmt.Errorf("no omakase in use; run `omasushi init` or `omasushi use <repo>` first")
	case name == "" && len(omakases) == 1:
		return &omakases[0], nil
	case name == "":
		var names []string
		for _, r := range omakases {
			names = append(names, r.Name)
		}
		return nil, fmt.Errorf("several omakases in use (%s); pick one with --to", strings.Join(names, ", "))
	}
	for i := range omakases {
		if omakases[i].Name == name {
			return &omakases[i], nil
		}
	}
	return nil, fmt.Errorf("no omakase named %q", name)
}

func printPlan(actions []Action, extras []string) {
	if len(actions) == 0 {
		fmt.Println("up to date")
	}
	for _, a := range actions {
		fmt.Printf("+ %-15s %s\n", a.Kind, a.Desc)
	}
	if len(extras) > 0 {
		fmt.Println("\ninstalled but not in any omakase (run `omasushi export` to record):")
		for _, e := range extras {
			fmt.Println("  ?", e)
		}
	}
}

func printPlanJSON(omakases []Omakase, actions []Action, extras []string) {
	type omakaseOut struct {
		Name  string `json:"name"`
		Dir   string `json:"dir"`
		Local bool   `json:"local"`
	}
	out := struct {
		Omakases []omakaseOut `json:"omakases"`
		Actions  []Action     `json:"actions"`
		Extras   []string     `json:"extras"`
	}{Actions: []Action{}, Extras: []string{}}
	for _, r := range omakases {
		out.Omakases = append(out.Omakases, omakaseOut{r.Name, r.Dir, r.Local})
	}
	if out.Omakases == nil {
		out.Omakases = []omakaseOut{}
	}
	if actions != nil {
		out.Actions = actions
	}
	if extras != nil {
		out.Extras = extras
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(out)
}

// export adds installed-but-unlisted items to target. It only adds; it never
// removes entries. Items already declared by any omakase (base or the
// resolved host overlay) are skipped.
func export(omakases []Omakase, target *Omakase, have *State, host, toHost string) (added []string) {
	var resolved Overlay
	for _, r := range omakases {
		h := host
		if toHost != "" {
			h = toHost
		}
		resolved = resolved.merge(r.Manifest.Resolve(h))
	}
	m := target.Manifest
	var t *Overlay
	if toHost == "" {
		t = &Overlay{Packages: m.Packages, Omarchy: m.Omarchy, Herdr: m.Herdr, Claude: m.Claude, Agent: m.Agent, Files: m.Files}
	} else {
		if m.Hosts == nil {
			m.Hosts = map[string]Overlay{}
		}
		o := m.Hosts[toHost]
		t = &o
	}

	if resolved.Omarchy.Font == "" && have.Font != "" {
		t.Omarchy.Font = have.Font
		added = append(added, "omarchy.font: "+have.Font)
	}
	if resolved.Omarchy.Defaults == (Defaults{}) && have.Defaults != (Defaults{}) {
		t.Omarchy.Defaults = have.Defaults
		added = append(added, fmt.Sprintf("omarchy.defaults: agent=%s browser=%s editor=%s terminal=%s",
			have.Defaults.Agent, have.Defaults.Browser, have.Defaults.Editor, have.Defaults.Terminal))
	}

	inAur := map[string]bool{}
	for _, p := range resolved.Packages.Aur {
		inAur[p] = true
	}
	var aur []string
	for p := range have.Aur {
		if !inAur[p] {
			aur = append(aur, p)
		}
	}
	sort.Strings(aur)
	for _, p := range aur {
		added = append(added, "packages.aur: "+p)
	}
	t.Packages.Aur = union(t.Packages.Aur, aur)

	inOP := map[string]bool{}
	for _, p := range resolved.Omarchy.Plugins {
		inOP[normalizeGitURL(p.URL)] = true
	}
	var ops []InstalledOmarchyPlugin
	for k, p := range have.OmarchyPlugins {
		if !inOP[k] {
			ops = append(ops, p)
		}
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i].URL < ops[j].URL })
	for _, p := range ops {
		t.Omarchy.Plugins = append(t.Omarchy.Plugins, OmarchyPlugin{URL: p.URL, Enable: p.Enabled})
		added = append(added, "omarchy.plugins: "+p.URL)
	}

	inHP := map[string]bool{}
	for _, p := range resolved.Herdr.Plugins {
		inHP[p.Source] = true
	}
	var hps []string
	for s := range have.HerdrPlugins {
		if !inHP[s] {
			hps = append(hps, s)
		}
	}
	sort.Strings(hps)
	for _, s := range hps {
		t.Herdr.Plugins = append(t.Herdr.Plugins, HerdrPlugin{Source: s})
		added = append(added, "herdr.plugins: "+s)
	}

	if toHost == "" {
		m.Packages, m.Omarchy, m.Herdr = t.Packages, t.Omarchy, t.Herdr
	} else {
		m.Hosts[toHost] = *t
	}
	return added
}

// initOmakase writes a starter omasushi.yaml and the conventional directories.
func initOmakase(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	mp := filepath.Join(dir, ManifestFile)
	if _, err := os.Stat(mp); err == nil {
		return fmt.Errorf("%s already exists", mp)
	}
	for _, d := range []string{"files", "skills", "commands"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			return err
		}
		os.WriteFile(filepath.Join(dir, d, ".gitkeep"), nil, 0o644)
	}
	name := filepath.Base(dir)
	if abs, err := filepath.Abs(dir); err == nil {
		name = filepath.Base(abs)
	}
	body := fmt.Sprintf(`# omasushi omakase — see https://github.com/polidog/omasushi
name: %s
description: ""

packages:
  pacman: []          # official repos (write by hand)
  aur: []             # filled in by "omasushi export"

omarchy:
  # font: "UDEV Gothic NF"
  # defaults:
  #   agent: claude    # pi|omp|opencode|claude|codex|grok|gemini|copilot|crush
  #   browser: chrome  # chromium|chrome|brave|brave-origin|edge|firefox|zen
  #   editor: nvim     # code|cursor|zed|sublime_text|helix|vim|emacs|nvim
  #   terminal: kitty  # foot|ghostty|kitty
  plugins: []         # - { url: https://github.com/owner/repo.git, enable: true }

herdr:
  plugins: []         # - { source: owner/repo }

agent:                # for the Omarchy default agent (omarchy.defaults.agent, else this machine's)
  skills: skills      # each subdirectory -> ~/.claude/skills/<name>, ~/.codex/skills/<name>, ...
  commands: commands  # each *.md          -> ~/.claude/commands/<name>.md, ~/.codex/prompts/<name>.md
# claude: { skills: skills, commands: commands }   # Claude Code only, whatever the default agent

files: {}             # files/kitty.conf: ~/.config/kitty/kitty.conf

hosts: {}             # <hostname>: { packages: ..., files: ... } overlays
`, name)
	if err := os.WriteFile(mp, []byte(body), 0o644); err != nil {
		return err
	}
	fmt.Printf("created %s\n", mp)
	fmt.Println("next: omasushi -f", mp, "export   # record what this machine has")
	return nil
}

func die(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "omasushi:", err)
		os.Exit(1)
	}
}
