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
	fmt.Fprintln(os.Stderr, `omasushi — add a feature to an Omarchy machine as one pack

usage: omasushi [-f <pack-dir>] <command> [args]

packs:
  use <owner/repo[/pack][@ref]|url|path>
                              take a pack (clone its repository, or point at a
                              local dir); owner/repo takes every pack of a
                              repository, owner/repo/herdr just that one;
                              @v1.2.0 pins the checkout to a tag/branch/commit
  list                        show packs in use (name, source, checkout;
                              "via X" = pulled in by X's use:)
  update                      git pull every remote repository (a pinned one
                              is re-fetched at its ref)
  remove <name>               forget a pack (unlinks its files, deletes its
                              managed checkout once nothing else needs it)
  init <dir>                  scaffold a repository of packs, or a pack inside one
  publish [<name>|<repo>|<path>] [--dry-run]
                              put a repository of packs on omasushi.dev: opens
                              the prefilled submission issue on GitHub, where a
                              workflow validates it onto the belt

machine:
  status [--json]             where am I: packs, their git state, this
                              machine's setup, and how far apart they are
  diff [--json]               show what sync would do
  sync                        install missing packages/plugins, link files/skills
  unlink [<name>] [--dry-run] undo sync's links: remove the symlinks and put
                              the .bak originals back (packages stay installed)
                              (plan/apply/clean still work as aliases)
  export <pack-dir> | --local record this machine's installed packages/plugins
                              that no pack in use declares yet, into that pack
                              (add-only) — or into this machine's own file
  skill install|update|remove|list [--agent <name>]
                              put the bundled omasushi skill into the default
                              agent's global skills (~/.claude/skills,
                              ~/.codex/skills, ...) — no pack needed;
                              update rewrites it after a newer go install
  version

this machine's own file is ~/.config/omasushi/omasushi.yaml: the packs it
takes under use:, plus what belongs to the machine alone in the same sections
a pack has. It is never published.

-f dir       use one pack (or repository of packs) instead of this machine's
             file (defaults to . when that file is empty)`)
	os.Exit(2)
}

func main() {
	file := flag.String("f", "", "pack directory (single-pack mode)")
	flag.Usage = usage
	flag.Parse()
	if flag.NArg() < 1 {
		usage()
	}
	cmd, args := flag.Arg(0), flag.Args()[1:]

	local, err := LoadLocal()
	die(err)

	switch cmd {
	case "version":
		fmt.Println("omasushi", version)
		return
	case "init":
		if len(args) != 1 {
			usage()
		}
		die(initDir(args[0]))
		return
	case "use":
		if len(args) != 1 {
			usage()
		}
		ps, err := local.Add(args[0])
		die(err)
		for _, p := range ps {
			fmt.Printf("using %s from %s (%s)\n", p.Name, p.Source, tildify(p.Dir))
		}
		all, err := resolveUses(ps)
		die(err)
		for _, p := range all {
			if p.Via != "" {
				fmt.Printf("using %s (via %s) (%s)\n", p.Name, p.Via, tildify(p.Dir))
			}
		}
		fmt.Printf("wrote %s\n", tildify(localPath()))
		return
	case "remove":
		if len(args) != 1 {
			usage()
		}
		packs, err := activePacks(local, "")
		die(err)
		for _, p := range packs {
			if p.Name == args[0] {
				_, err := Unlink([]Pack{p}, false)
				die(err)
			}
		}
		die(local.Remove(args[0]))
		fmt.Println("removed", args[0])
		return
	case "recipe", "mine":
		die(fmt.Errorf("`%s` is gone: export names the pack it writes to (omasushi export <pack-dir>), publish the checkout you stand in", cmd))
	case "publish":
		die(publishCmd(local, *file, args))
		return
	case "skill":
		die(skillCmd(args))
		return
	}

	packs, err := activePacks(local, *file)
	die(err)

	switch cmd {
	case "list":
		if len(packs) == 0 {
			fmt.Println("no packs in use (try: omasushi use owner/repo)")
		}
		for _, p := range packs {
			kind := "git"
			if p.Local {
				kind = "local"
			}
			var note string
			if p.Name == LocalName {
				note = "  (this machine)"
			}
			if p.Via != "" {
				note += "  (via " + p.Via + ")"
			}
			if p.Ref != "" {
				note += "  (pinned @" + p.Ref + ")"
			}
			fmt.Printf("%-28s %-6s %-44s %s%s\n", p.Name, kind, p.Source, tildify(p.Dir), note)
		}
	case "update":
		die(Update(packs))
		die(updateInstalledSkills())
	case "status":
		fs := flag.NewFlagSet("status", flag.ExitOnError)
		asJSON := fs.Bool("json", false, "machine readable output")
		fs.Parse(args)
		have, err := Probe()
		die(err)
		st := gatherStatus(packs, have)
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
		actions, extras := Plan(packs, have)
		if *asJSON {
			printPlanJSON(packs, actions, extras)
		} else {
			printPlan(actions, extras)
		}
	case "sync", "apply": // apply is the pre-rename alias
		have, err := Probe()
		die(err)
		actions, _ := Plan(packs, have)
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
		targets := packs
		if fs.NArg() > 0 {
			t, err := pickPack(packs, fs.Arg(0))
			die(err)
			targets = []Pack{*t}
		}
		undone, err := Unlink(targets, *dryRun)
		die(err)
		if len(undone) == 0 {
			fmt.Println("nothing linked")
		}
	case "export":
		fs := flag.NewFlagSet("export", flag.ExitOnError)
		toLocal := fs.Bool("local", false, "write into this machine's own file instead of a pack")
		fs.Parse(args)
		target, err := exportTarget(packs, local, fs.Args(), *toLocal)
		die(err)
		if !target.Local {
			fmt.Fprintf(os.Stderr, "note: %s is a managed checkout under %s — commit & push there yourself, or export into a clone of your own\n",
				target.Name, tildify(packsDir()))
		}
		have, err := Probe()
		die(err)
		added := export(packs, target, have)
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

// activePacks picks the pack set: -f wins; otherwise this machine's file,
// whose use: is expanded into the layers underneath it. A file that says
// nothing at all falls back to the working directory, so a checkout can be
// driven in place.
func activePacks(local *Local, file string) ([]Pack, error) {
	var ps []Pack
	var err error
	switch {
	case file != "":
		ps, err = packsFromDir(file)
	case local.blank():
		if !isPackRepo(".") {
			return nil, nil
		}
		ps, err = packsFromDir(".")
	default:
		ps = []Pack{local.Pack()}
	}
	if err != nil || ps == nil {
		return ps, err
	}
	return resolveUses(ps)
}

// exportTarget picks where export writes: this machine's file with --local,
// else the pack directory named — one pack, so a repository of several is
// refused with their names. A pack already in use is written through its
// loaded manifest; any other pack directory is loaded fresh.
func exportTarget(packs []Pack, local *Local, args []string, toLocal bool) (*Pack, error) {
	switch {
	case toLocal && len(args) > 0:
		return nil, fmt.Errorf("export takes a pack directory or --local, not both")
	case toLocal:
		for i := range packs {
			if packs[i].Name == LocalName {
				return &packs[i], nil
			}
		}
		p := local.Pack()
		return &p, nil
	case len(args) != 1:
		var names []string
		for _, p := range packs {
			if p.Local && p.Name != LocalName {
				names = append(names, tildify(p.Dir))
			}
		}
		hint := ""
		if len(names) > 0 {
			hint = " — in use here: " + strings.Join(names, ", ")
		}
		return nil, fmt.Errorf("export needs a pack directory to write into, or --local for this machine's own file%s", hint)
	}
	abs, err := filepath.Abs(args[0])
	if err != nil {
		return nil, err
	}
	for i := range packs {
		if packs[i].Dir == abs {
			return &packs[i], nil
		}
	}
	ps, err := packsFromDir(abs)
	if err != nil {
		return nil, err
	}
	if len(ps) != 1 || ps[0].Dir != abs {
		var names []string
		for _, p := range ps {
			names = append(names, p.Sub)
		}
		return nil, fmt.Errorf("%s is a repository of packs (%s); name one of them", args[0], strings.Join(names, ", "))
	}
	return &ps[0], nil
}

func pickPack(packs []Pack, name string) (*Pack, error) {
	if len(packs) == 0 {
		return nil, fmt.Errorf("no pack in use; run `omasushi use <repo>` first")
	}
	for i := range packs {
		if packs[i].Name == name {
			return &packs[i], nil
		}
	}
	return nil, fmt.Errorf("no pack named %q", name)
}

func printPlan(actions []Action, extras []string) {
	if len(actions) == 0 {
		fmt.Println("up to date")
	}
	for _, a := range actions {
		if a.Pack != "" {
			fmt.Printf("+ %-15s %-44s <- %s\n", a.Kind, a.Desc, a.Pack)
		} else {
			fmt.Printf("+ %-15s %s\n", a.Kind, a.Desc)
		}
	}
	if len(extras) > 0 {
		fmt.Println("\ninstalled but not in any pack (omasushi export <pack-dir> records them; --local keeps them on this machine):")
		for _, e := range extras {
			fmt.Println("  ?", e)
		}
	}
}

func printPlanJSON(packs []Pack, actions []Action, extras []string) {
	type packOut struct {
		Name  string `json:"name"`
		Dir   string `json:"dir"`
		Local bool   `json:"local"`
	}
	out := struct {
		Packs   []packOut `json:"packs"`
		Actions []Action  `json:"actions"`
		Extras  []string  `json:"extras"`
	}{Packs: []packOut{}, Actions: []Action{}, Extras: []string{}}
	for _, p := range packs {
		out.Packs = append(out.Packs, packOut{p.Name, p.Dir, p.Local})
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
// removes entries. Items already declared by any pack in use are skipped.
func export(packs []Pack, target *Pack, have *State) (added []string) {
	var resolved Manifest
	for _, p := range packs {
		resolved = resolved.merge(*p.Manifest)
	}
	m := target.Manifest

	if resolved.Omarchy.Font == "" && have.Font != "" {
		m.Omarchy.Font = have.Font
		added = append(added, "omarchy.font: "+have.Font)
	}
	if resolved.Omarchy.Defaults == (Defaults{}) && have.Defaults != (Defaults{}) {
		m.Omarchy.Defaults = have.Defaults
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
	m.Packages.Aur = union(m.Packages.Aur, aur)

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
		m.Omarchy.Plugins = append(m.Omarchy.Plugins, OmarchyPlugin{URL: p.URL, Enable: p.Enabled})
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
		m.Herdr.Plugins = append(m.Herdr.Plugins, HerdrPlugin{Source: s})
		added = append(added, "herdr.plugins: "+s)
	}
	return added
}

// initDir scaffolds a repository of packs, or — when dir sits inside one
// (its parent carries an omasushi.yaml) — a pack in it.
func initDir(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(abs, ManifestFile)); err == nil {
		return fmt.Errorf("%s already exists", filepath.Join(dir, ManifestFile))
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(abs), ManifestFile)); err == nil {
		return initPack(dir, abs)
	}
	return initRepo(dir, abs)
}

func initRepo(dir, abs string) error {
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return err
	}
	body := fmt.Sprintf(`# A repository of packs — see https://github.com/polidog/omasushi
# Every subdirectory with an omasushi.yaml is a pack; this file only names
# the repository. `+"`omasushi init %s/<pack>`"+` adds one.
name: %s
description: ""
`, dir, filepath.Base(abs))
	if err := os.WriteFile(filepath.Join(abs, ManifestFile), []byte(body), 0o644); err != nil {
		return err
	}
	fmt.Printf("created %s\n", filepath.Join(dir, ManifestFile))
	fmt.Printf("next: omasushi init %s/<pack>   # a pack: one feature's packages, plugins, config, skills\n", dir)
	return nil
}

func initPack(dir, abs string) error {
	for _, d := range []string{"files", "skills", "commands"} {
		if err := os.MkdirAll(filepath.Join(abs, d), 0o755); err != nil {
			return err
		}
		os.WriteFile(filepath.Join(abs, d, ".gitkeep"), nil, 0o644)
	}
	body := `# A pack: everything one feature needs on top of stock Omarchy.
# See https://github.com/polidog/omasushi
description: ""

# use:                # packs this one needs; loaded underneath it, so this file wins
#   - ../fonts        # a sibling in this repository
#   - polidog/omakase/ime

packages:
  pacman: []          # official repos (write by hand)
  aur: []             # filled in by "omasushi export <this dir>"

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

# hypr: hypr/bindings.lua   # a hyprland snippet, loaded after Omarchy's defaults alongside other packs'

files: {}             # files/kitty.conf: ~/.config/kitty/kitty.conf
`
	if err := os.WriteFile(filepath.Join(abs, ManifestFile), []byte(body), 0o644); err != nil {
		return err
	}
	fmt.Printf("created %s\n", filepath.Join(dir, ManifestFile))
	fmt.Printf("next: omasushi export %s   # records what this machine has that no pack declares yet\n", dir)
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
		if a.Pack != "" {
			fmt.Printf("==> %s: %s (%s)\n", a.Kind, a.Desc, a.Pack)
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
