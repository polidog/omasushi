package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
)

// version is set by the release build (-X main.version), which only works on a
// constant initialiser — hence the init below rather than a call here.
var version = "dev"

// A `go install` binary carries no ldflags, so fall back to the module version
// the toolchain stamped into it (v0.2.0, or a pseudo-version for a checkout).
func init() {
	if version != "dev" {
		return
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if v := bi.Main.Version; v != "" && v != "(devel)" {
		version = v
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `omasushi — share your Omarchy setup as an omakase repository

usage: omasushi [-f omasushi.yaml] [-H host] <command> [args]

omakases:
  use [--recipe] <owner/repo[/part]|url|path>
                              add an omakase to this machine's use: list (clone
                              it, or point at a local dir); owner/repo takes
                              every part of a split repository, owner/repo/herdr
                              just that one. --recipe puts it in recipe: instead
  recipe [<path>|none]        show or set the omakase this machine publishes
  list                        show omakases in use (name, source, checkout;
                              "via X" = pulled in by X's use: declaration)
  update                      git pull every remote omakase
  remove <name>               forget an omakase (unlinks its files, deletes
                              its managed checkout)
  init [dir]                  scaffold a new omakase repository
  publish [<name>|<repo>|<path>]
                              put an omakase on omasushi-web: opens the
                              prefilled submission issue on GitHub, where a
                              workflow validates it onto the belt

machine:
  status [--json]             where am I: omakases, their git state, this
                              machine's setup, and how far apart they are
  diff [--json]               show what sync would do
  sync                        install missing packages/plugins, link files/skills
  unlink [<name>] [--dry-run] undo sync's links: remove the symlinks and put
                              the .bak originals back (packages stay installed)
                              (plan/apply/clean still work as aliases)
  export [--to <omakase>] [--host <name>]
                              record this machine's installed packages/plugins
                              into an omakase: the recipe when set, else this
                              machine. --to machine|recipe|<name> picks another
                              (--host writes under hosts.<name>)
  skill install|update|remove|list [--agent <name>]
                              put the bundled omasushi skill into the default
                              agent's global skills (~/.claude/skills,
                              ~/.codex/skills, ...) — no omakase needed;
                              update rewrites it after a newer go install
  version

this machine's own omakase is ~/.config/omasushi/omasushi.yaml: an ordinary
manifest (packages, files, hosts) whose use: names what it takes from other
people and whose recipe: names the omakase it publishes. It never leaves the
machine; publish only ever offers the recipe.

-f path      use a single manifest instead of this machine's own
             (defaults to ./omasushi.yaml when that one is empty)
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

	machine, err := LoadMachine()
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
		fs := flag.NewFlagSet("use", flag.ExitOnError)
		recipe := fs.Bool("recipe", false, "the omakase this machine publishes, not one it takes from")
		fs.Parse(args)
		if fs.NArg() != 1 {
			usage()
		}
		rs, err := machine.Add(fs.Arg(0), *recipe)
		die(err)
		for _, r := range rs {
			fmt.Printf("using %s from %s (%s)\n", r.Name, r.Source, tildify(r.Dir))
		}
		all, err := resolveUses(rs)
		die(err)
		for _, r := range all {
			if r.Via != "" {
				fmt.Printf("using %s (via %s) (%s)\n", r.Name, r.Via, tildify(r.Dir))
			}
		}
		if *recipe {
			fmt.Printf("recipe: %s — export writes here, and publish offers this one\n", machine.Recipe)
			if len(rs) > 0 && !rs[0].Local {
				fmt.Fprintf(os.Stderr, "note: it is a managed checkout under %s — for a recipe you edit and push, clone it yourself and `omasushi recipe <dir>`\n",
					tildify(omakasesDir()))
			}
		}
		fmt.Printf("wrote %s\n", tildify(machinePath()))
		return
	case "remove":
		if len(args) != 1 {
			usage()
		}
		omakases, err := activeOmakases(machine, "")
		die(err)
		for _, r := range omakases {
			if r.Name == args[0] {
				_, err := Unlink([]Omakase{r}, *host, false)
				die(err)
			}
		}
		die(machine.Remove(args[0]))
		fmt.Println("removed", args[0])
		return
	case "recipe", "mine": // mine is the pre-recipe: name
		if cmd == "mine" {
			fmt.Fprintln(os.Stderr, "note: `mine` is now `recipe` — the omakase this machine publishes")
		}
		if len(args) == 0 {
			if machine.Recipe == "" {
				fmt.Println("not set — omasushi recipe <path>, or omasushi use --recipe <repo>")
			} else {
				fmt.Println(machine.Recipe)
			}
			return
		}
		if args[0] == "none" {
			machine.Recipe = ""
			die(machine.Save())
			fmt.Println("recipe: unset")
			return
		}
		src, err := parseSource(args[0])
		die(err)
		if _, err := os.Stat(filepath.Join(checkoutDir(src), ManifestFile)); err != nil {
			die(fmt.Errorf("%s has no %s (run `omasushi use --recipe %s` to clone it first)", args[0], ManifestFile, args[0]))
		}
		machine.Recipe = omakaseName(src.Repo, src.Part)
		die(machine.Save())
		fmt.Printf("recipe: %s — export writes here, and publish offers this one\n", machine.Recipe)
		return
	case "publish":
		die(publishCmd(machine, *file, args))
		return
	case "skill":
		die(skillCmd(args))
		return
	}

	omakases, err := activeOmakases(machine, *file)
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
			var note string
			switch {
			case r.Name == MachineName:
				note = "  (this machine)"
			case r.Repo == machine.recipeRepo():
				note = "  (recipe)"
			}
			if r.Via != "" {
				note += "  (via " + r.Via + ")"
			}
			if paths := r.Only.paths(); len(paths) > 0 {
				note += "  (only " + strings.Join(paths, ", ") + ")"
			}
			fmt.Printf("%-28s %-6s %-44s %s%s\n", r.Name, kind, r.Source, tildify(r.Dir), note)
		}
	case "update":
		die(Update(omakases))
		die(updateInstalledSkills())
	case "status":
		fs := flag.NewFlagSet("status", flag.ExitOnError)
		asJSON := fs.Bool("json", false, "machine readable output")
		fs.Parse(args)
		have, err := Probe()
		die(err)
		st := gatherStatus(omakases, *host, have, machine.recipeRepo())
		if *asJSON {
			printStatusJSON(st)
		} else {
			printStatus(st)
		}
	case "diff", "plan": // plan is the pre-rename alias
		fs := flag.NewFlagSet("diff", flag.ExitOnError)
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
	case "sync", "apply": // apply is the pre-rename alias
		have, err := Probe()
		die(err)
		actions, _ := Plan(omakases, *host, have)
		if len(actions) == 0 {
			fmt.Println("up to date")
			return
		}
		if failed := runActions(actions); failed > 0 {
			os.Exit(1)
		}
	case "unlink", "clean": // clean is the pre-rename alias
		fs := flag.NewFlagSet("unlink", flag.ExitOnError)
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
		to := fs.String("to", "", "where to write: machine, recipe, or an omakase name (default: recipe, else machine)")
		fs.Parse(args)
		target, err := exportTarget(omakases, *to, machine)
		die(err)
		if !target.Local {
			fmt.Fprintf(os.Stderr, "note: %s is a managed checkout under %s — commit & push there yourself, or point recipe: at a clone of your own\n",
				target.Name, tildify(omakasesDir()))
		}
		have, err := Probe()
		die(err)
		added := export(omakases, target, have, *host, *toHost)
		if len(added) == 0 {
			fmt.Println("nothing new")
			return
		}
		die(target.Save())
		fmt.Printf("wrote %s\n", tildify(target.ManifestPath()))
		for _, a := range added {
			fmt.Println("+", a)
		}
	default:
		usage()
	}
}

// activeOmakases picks the omakase set: -f wins; otherwise this machine's own
// omakase, whose use: chain (the recipe last, so it wins) is expanded into the
// layers underneath it. A machine that declares nothing at all falls back to an
// omasushi.yaml in the working directory, so a checkout can be driven in place.
func activeOmakases(machine *Machine, file string) ([]Omakase, error) {
	var rs []Omakase
	var err error
	switch {
	case file != "":
		rs, err = omakaseFromDir(file)
	case machine.blank():
		if _, statErr := os.Stat(ManifestFile); statErr != nil {
			return nil, nil
		}
		rs, err = omakaseFromDir(ManifestFile)
	default:
		rs = []Omakase{machine.Omakase()}
	}
	if err != nil || rs == nil {
		return rs, err
	}
	return resolveUses(rs)
}

// exportTarget picks where export writes: --to when given (machine, recipe, or
// an omakase name), else the recipe, else this machine — the one place that is
// always there to record into.
func exportTarget(omakases []Omakase, to string, machine *Machine) (*Omakase, error) {
	switch to {
	case "recipe", "":
		rs := omakasesInRepo(omakases, machine.recipeRepo())
		switch {
		case len(rs) == 1:
			return rs[0], nil
		case len(rs) > 1:
			var names []string
			for _, r := range rs {
				names = append(names, r.Name)
			}
			return nil, fmt.Errorf("the recipe is split into %d parts; pick one with --to (%s), or --to machine", len(rs), strings.Join(names, ", "))
		case to == "recipe":
			return nil, fmt.Errorf("no recipe set; `omasushi recipe <path>` names the omakase this machine publishes")
		}
		to = MachineName
	}
	return pickOmakase(omakases, to)
}

// omakasesInRepo picks the omakases that live in one checkout — several when
// it is split into parts.
func omakasesInRepo(omakases []Omakase, repo string) []*Omakase {
	var out []*Omakase
	if repo == "" {
		return nil
	}
	for i := range omakases {
		if omakases[i].Repo == repo {
			out = append(out, &omakases[i])
		}
	}
	return out
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
		return nil, fmt.Errorf("several omakases in use (%s); pick one with --to, or name your own once with `omasushi recipe <path>`", strings.Join(names, ", "))
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
		if a.Omakase != "" {
			fmt.Printf("+ %-15s %-44s <- %s\n", a.Kind, a.Desc, a.Omakase)
		} else {
			fmt.Printf("+ %-15s %s\n", a.Kind, a.Desc)
		}
	}
	if len(extras) > 0 {
		fmt.Println("\ninstalled but not in any omakase (run `omasushi export` to record them as yours):")
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
		resolved = resolved.merge(r.Resolve(h))
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

# use:                # build on other people's omakases; this file wins on conflicts
#   - polidog/omakase/kitty
#   - someone/nvim-setup

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
	fmt.Printf("next: omasushi recipe %s   # then `omasushi export` records this machine into it\n", dir)
	return nil
}

// runActions works through the plan, carrying on past an action that fails so
// that one package the mirror does not have, or one plugin already installed
// under another URL, does not hold back every link queued behind it. Failures
// are repeated at the end, because by then the output that follows them has
// usually scrolled them out of sight. Returns how many failed.
func runActions(actions []Action) int {
	type failure struct {
		action Action
		err    error
	}
	var failed []failure
	for _, a := range actions {
		if a.Omakase != "" {
			fmt.Printf("==> %s: %s (%s)\n", a.Kind, a.Desc, a.Omakase)
		} else {
			fmt.Printf("==> %s: %s\n", a.Kind, a.Desc)
		}
		if err := a.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "omasushi:", err)
			failed = append(failed, failure{a, err})
		}
	}
	if len(failed) == 0 {
		return 0
	}
	fmt.Fprintf(os.Stderr, "\n%d of %d actions failed; the rest were applied:\n", len(failed), len(actions))
	for _, f := range failed {
		fmt.Fprintf(os.Stderr, "  %-15s %s: %v\n", f.action.Kind, f.action.Desc, f.err)
	}
	fmt.Fprintln(os.Stderr, "run `omasushi sync` again once they are sorted out.")
	return len(failed)
}

func die(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "omasushi:", err)
		os.Exit(1)
	}
}
