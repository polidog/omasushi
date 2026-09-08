package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ManifestFile is the file a pack carries; also the file at the root of a
// repository of packs (naming the repository) and this machine's own file.
const ManifestFile = "omasushi.yaml"

// Manifest is one pack's omasushi.yaml: what one feature needs on top of
// stock Omarchy. The same shape names a repository of packs at its root
// (Name and Description only) and is this machine's own file, ~/.config/omasushi/omasushi.yaml.
//
// Use names the packs this one needs (owner/repo[/pack], a git URL, or a
// path — relative ones resolve against this pack's directory, so a sibling is
// ../fonts). They are layered underneath the declaring pack, so it wins on
// conflicts; see resolveUses. A pack that is only a use: list is a bundle.
type Manifest struct {
	Name        string            `yaml:"name,omitempty"`
	Description string            `yaml:"description,omitempty"`
	Use         []string          `yaml:"use,omitempty"`
	Packages    Packages          `yaml:"packages,omitempty"`
	Omarchy     Omarchy           `yaml:"omarchy,omitempty"`
	Herdr       Herdr             `yaml:"herdr,omitempty"`
	Claude      Claude            `yaml:"claude,omitempty"`
	Agent       Claude            `yaml:"agent,omitempty"`
	Hypr        string            `yaml:"hypr,omitempty"` // a .lua snippet, loaded from hyprland.lua after Omarchy's defaults
	Files       map[string]string `yaml:"files,omitempty"`
}

type Packages struct {
	Pacman []string `yaml:"pacman,omitempty"`
	Aur    []string `yaml:"aur,omitempty"`
}

type Omarchy struct {
	Font     string          `yaml:"font,omitempty"` // value of `omarchy font set`; empty = don't care
	Defaults Defaults        `yaml:"defaults,omitempty"`
	Plugins  []OmarchyPlugin `yaml:"plugins,omitempty"`
}

// Defaults mirrors `omarchy default <kind> [value]`. Empty means "don't care".
type Defaults struct {
	Agent    string `yaml:"agent,omitempty" json:"agent"`
	Browser  string `yaml:"browser,omitempty" json:"browser"`
	Editor   string `yaml:"editor,omitempty" json:"editor"`
	Terminal string `yaml:"terminal,omitempty" json:"terminal"`
}

func (d Defaults) merge(o Defaults) Defaults {
	if o.Agent != "" {
		d.Agent = o.Agent
	}
	if o.Browser != "" {
		d.Browser = o.Browser
	}
	if o.Editor != "" {
		d.Editor = o.Editor
	}
	if o.Terminal != "" {
		d.Terminal = o.Terminal
	}
	return d
}

type OmarchyPlugin struct {
	URL    string `yaml:"url"`
	Enable bool   `yaml:"enable,omitempty"`
}

type Herdr struct {
	Plugins []HerdrPlugin `yaml:"plugins,omitempty"`
}

type HerdrPlugin struct {
	Source string `yaml:"source"` // owner/repo[/subdir]
	Ref    string `yaml:"ref,omitempty"`
}

// Claude shares Claude Code skills and slash commands. Skills is a pack
// relative directory whose children are linked to ~/.claude/skills/<name>;
// Commands is a directory whose *.md files are linked to
// ~/.claude/commands/<name>.md. Linking per entry (not the whole directory)
// lets the machine keep its own skills alongside the shared ones.
//
// The same shape under agent: is linked for whichever agent is the Omarchy
// default (omarchy.defaults.agent, else the machine's own choice), so one
// skills/ directory serves Claude Code, Codex, Gemini CLI … (see agentDirs).
type Claude struct {
	Skills   string `yaml:"skills,omitempty"`
	Commands string `yaml:"commands,omitempty"`
}

func LoadManifest(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Manifest{}, nil
	}
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &m, nil
}

func (m *Manifest) Save(path string) error {
	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(sb.String()), 0o644)
}

// merge layers o on top of m, for stacking several packs: later wins on
// scalars, lists are unioned, plugin entries are keyed by URL/source with the
// later one winning. Name, Description, Use and Hypr are per pack and stay
// out of it.
func (m Manifest) merge(o Manifest) Manifest {
	m.Packages.Pacman = union(m.Packages.Pacman, o.Packages.Pacman)
	m.Packages.Aur = union(m.Packages.Aur, o.Packages.Aur)
	if o.Omarchy.Font != "" {
		m.Omarchy.Font = o.Omarchy.Font
	}
	m.Omarchy.Defaults = m.Omarchy.Defaults.merge(o.Omarchy.Defaults)
	m.Omarchy.Plugins = mergeOmarchyPlugins(m.Omarchy.Plugins, o.Omarchy.Plugins)
	m.Herdr.Plugins = mergeHerdrPlugins(m.Herdr.Plugins, o.Herdr.Plugins)
	if o.Claude.Skills != "" {
		m.Claude.Skills = o.Claude.Skills
	}
	if o.Claude.Commands != "" {
		m.Claude.Commands = o.Claude.Commands
	}
	if o.Agent.Skills != "" {
		m.Agent.Skills = o.Agent.Skills
	}
	if o.Agent.Commands != "" {
		m.Agent.Commands = o.Agent.Commands
	}
	files := map[string]string{}
	for k, v := range m.Files {
		files[k] = v
	}
	for k, v := range o.Files {
		files[k] = v
	}
	m.Files = files
	return m
}

// declares reports whether a manifest carries anything sync would act on —
// as opposed to only naming something (a repository root) or nothing at all.
func (m *Manifest) declares() bool {
	return len(m.Use) > 0 ||
		len(m.Packages.Pacman) > 0 || len(m.Packages.Aur) > 0 ||
		m.Omarchy.Font != "" || m.Omarchy.Defaults != (Defaults{}) ||
		len(m.Omarchy.Plugins) > 0 || len(m.Herdr.Plugins) > 0 ||
		m.Claude != (Claude{}) || m.Agent != (Claude{}) ||
		m.Hypr != "" || len(m.Files) > 0
}

func union(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]string{}, a...), b...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func mergeOmarchyPlugins(a, b []OmarchyPlugin) []OmarchyPlugin {
	idx := map[string]int{}
	var out []OmarchyPlugin
	for _, p := range append(append([]OmarchyPlugin{}, a...), b...) {
		k := normalizeGitURL(p.URL)
		if i, ok := idx[k]; ok {
			out[i] = p
			continue
		}
		idx[k] = len(out)
		out = append(out, p)
	}
	return out
}

func mergeHerdrPlugins(a, b []HerdrPlugin) []HerdrPlugin {
	idx := map[string]int{}
	var out []HerdrPlugin
	for _, p := range append(append([]HerdrPlugin{}, a...), b...) {
		if i, ok := idx[p.Source]; ok {
			out[i] = p
			continue
		}
		idx[p.Source] = len(out)
		out = append(out, p)
	}
	return out
}

// normalizeGitURL makes https://github.com/a/b.git and github.com/a/b compare equal.
func normalizeGitURL(u string) string {
	u = strings.TrimSpace(u)
	u = strings.TrimPrefix(u, "https://")
	u = strings.TrimPrefix(u, "http://")
	u = strings.TrimPrefix(u, "git@")
	u = strings.Replace(u, ":", "/", 1)
	u = strings.TrimSuffix(u, "/")
	u = strings.TrimSuffix(u, ".git")
	return strings.ToLower(u)
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[1:])
	}
	return p
}
