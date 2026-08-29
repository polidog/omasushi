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
	fmt.Fprintln(os.Stderr, `omasushi — share your Omarchy setup as a recipe repository

usage: omasushi [-f omasushi.yaml] [-H host] <command> [args]

recipes:
  use <owner/repo|url|path>   add a recipe (clone it, or point at a local dir)
  list                        show recipes in use
  update                      git pull every remote recipe
  remove <name>               forget a recipe (deletes its managed checkout)
  init [dir]                  scaffold a new recipe repository

machine:
  plan [--json]               show what apply would do
  apply                       install missing packages/plugins, link files/skills
  export [--to <recipe>] [--host <name>]
                              record this machine's installed packages/plugins
                              into a recipe (--host writes under hosts.<name>)
  version

-f path      use a single manifest instead of the configured recipes
             (defaults to ./omasushi.yaml when no recipe is configured)
-H host      resolve hosts.<host> overlays as if running on that machine`)
	os.Exit(2)
}

func main() {
	file := flag.String("f", "", "manifest path (single-recipe mode)")
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
		die(initRecipe(dir))
		return
	case "use":
		if len(args) != 1 {
			usage()
		}
		r, err := cfg.Use(args[0])
		die(err)
		fmt.Printf("using %s (%s)\n", r.Name, r.Dir)
		return
	case "remove":
		if len(args) != 1 {
			usage()
		}
		die(cfg.Remove(args[0]))
		fmt.Println("removed", args[0])
		return
	}

	recipes, err := activeRecipes(cfg, *file)
	die(err)

	switch cmd {
	case "list":
		if len(recipes) == 0 {
			fmt.Println("no recipes in use (try: omasushi use owner/repo)")
		}
		for _, r := range recipes {
			kind := "git"
			if r.Local {
				kind = "local"
			}
			fmt.Printf("%-24s %-6s %s\n", r.Name, kind, r.Dir)
		}
	case "update":
		die(Update(recipes))
	case "plan":
		fs := flag.NewFlagSet("plan", flag.ExitOnError)
		asJSON := fs.Bool("json", false, "machine readable output")
		fs.Parse(args)
		have, err := Probe()
		die(err)
		actions, extras := Plan(recipes, *host, have)
		if *asJSON {
			printPlanJSON(recipes, actions, extras)
		} else {
			printPlan(actions, extras)
		}
	case "apply":
		have, err := Probe()
		die(err)
		actions, _ := Plan(recipes, *host, have)
		if len(actions) == 0 {
			fmt.Println("up to date")
			return
		}
		for _, a := range actions {
			fmt.Printf("==> %s: %s\n", a.Kind, a.Desc)
			die(a.Run())
		}
	case "export":
		fs := flag.NewFlagSet("export", flag.ExitOnError)
		toHost := fs.String("host", "", "write into hosts.<name> overlay")
		to := fs.String("to", "", "recipe to write into (required when several are in use)")
		fs.Parse(args)
		target, err := pickRecipe(recipes, *to)
		die(err)
		have, err := Probe()
		die(err)
		added := export(recipes, target, have, *host, *toHost)
		if len(added) == 0 {
			fmt.Println("nothing new")
			return
		}
		die(target.Manifest.Save(target.ManifestPath()))
		fmt.Printf("wrote %s\n", target.ManifestPath())
		for _, a := range added {
			fmt.Println("+", a)
		}
	default:
		usage()
	}
}

// activeRecipes picks the recipe set: -f wins; otherwise the config; and
// when nothing is configured, an omasushi.yaml in the working directory.
func activeRecipes(cfg *Config, file string) ([]Recipe, error) {
	if file != "" {
		r, err := recipeFromDir(file)
		if err != nil {
			return nil, err
		}
		return []Recipe{r}, nil
	}
	if len(cfg.Recipes) > 0 {
		return LoadRecipes(cfg)
	}
	if _, err := os.Stat(ManifestFile); err == nil {
		r, err := recipeFromDir(ManifestFile)
		if err != nil {
			return nil, err
		}
		return []Recipe{r}, nil
	}
	return nil, nil
}

func pickRecipe(recipes []Recipe, name string) (*Recipe, error) {
	switch {
	case len(recipes) == 0:
		return nil, fmt.Errorf("no recipe in use; run `omasushi init` or `omasushi use <repo>` first")
	case name == "" && len(recipes) == 1:
		return &recipes[0], nil
	case name == "":
		var names []string
		for _, r := range recipes {
			names = append(names, r.Name)
		}
		return nil, fmt.Errorf("several recipes in use (%s); pick one with --to", strings.Join(names, ", "))
	}
	for i := range recipes {
		if recipes[i].Name == name {
			return &recipes[i], nil
		}
	}
	return nil, fmt.Errorf("no recipe named %q", name)
}

func printPlan(actions []Action, extras []string) {
	if len(actions) == 0 {
		fmt.Println("up to date")
	}
	for _, a := range actions {
		fmt.Printf("+ %-15s %s\n", a.Kind, a.Desc)
	}
	if len(extras) > 0 {
		fmt.Println("\ninstalled but not in any recipe (run `omasushi export` to record):")
		for _, e := range extras {
			fmt.Println("  ?", e)
		}
	}
}

func printPlanJSON(recipes []Recipe, actions []Action, extras []string) {
	type recipeOut struct {
		Name  string `json:"name"`
		Dir   string `json:"dir"`
		Local bool   `json:"local"`
	}
	out := struct {
		Recipes []recipeOut `json:"recipes"`
		Actions []Action    `json:"actions"`
		Extras  []string    `json:"extras"`
	}{Actions: []Action{}, Extras: []string{}}
	for _, r := range recipes {
		out.Recipes = append(out.Recipes, recipeOut{r.Name, r.Dir, r.Local})
	}
	if out.Recipes == nil {
		out.Recipes = []recipeOut{}
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
// removes entries. Items already declared by any recipe (base or the
// resolved host overlay) are skipped.
func export(recipes []Recipe, target *Recipe, have *State, host, toHost string) (added []string) {
	var resolved Overlay
	for _, r := range recipes {
		h := host
		if toHost != "" {
			h = toHost
		}
		resolved = resolved.merge(r.Manifest.Resolve(h))
	}
	m := target.Manifest
	var t *Overlay
	if toHost == "" {
		t = &Overlay{Packages: m.Packages, Omarchy: m.Omarchy, Herdr: m.Herdr, Claude: m.Claude, Files: m.Files}
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

// initRecipe writes a starter omasushi.yaml and the conventional directories.
func initRecipe(dir string) error {
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
	body := fmt.Sprintf(`# omasushi recipe — see https://github.com/polidog/omasushi
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

claude:
  skills: skills      # each subdirectory -> ~/.claude/skills/<name>
  commands: commands  # each *.md          -> ~/.claude/commands/<name>.md

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
