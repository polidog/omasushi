package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ManifestFile is the file name an omakase repository carries at its root.
const ManifestFile = "omasushi.yaml"

// Manifest is the desired state of a machine, as declared by one omakase.
// Hosts holds per-host overlays that are merged onto the base when
// resolved for a given hostname.
//
// A repository may instead be split into feature-sized parts (herdr, kitty,
// claude …) declared in Parts; `omasushi use owner/repo` takes all of them and
// `omasushi use owner/repo/herdr` just one. A manifest that declares parts is
// only their index: its own sections are not applied.
type Manifest struct {
	Name        string             `yaml:"name,omitempty"`
	Description string             `yaml:"description,omitempty"`
	Parts       Parts              `yaml:"parts,omitempty"`
	Packages    Packages           `yaml:"packages,omitempty"`
	Omarchy     Omarchy            `yaml:"omarchy,omitempty"`
	Herdr       Herdr              `yaml:"herdr,omitempty"`
	Claude      Claude             `yaml:"claude,omitempty"`
	Files       map[string]string  `yaml:"files,omitempty"`
	Hosts       map[string]Overlay `yaml:"hosts,omitempty"`
}

// Parts is a root manifest's parts declaration. It accepts two spellings:
//
//	parts: [herdr, kitty]        # each part is a directory with its own omasushi.yaml,
//	                             # and its paths are relative to that directory
//
//	parts:                       # each part is written out here, and its paths stay
//	  herdr:                     # relative to the repository root, so one files/
//	    herdr:                   # tree can serve every part
//	      plugins: [{source: a/b}]
//	  kitty:
//	    files: {files/kitty/kitty.conf: ~/.config/kitty/kitty.conf}
//
// The two mix: in the map form a part with an empty value is a directory part.
// Names is the declared order; Inline holds only the parts written out in place.
// A part may carry anything a manifest can except nested parts.
type Parts struct {
	Names  []string
	Inline map[string]*Manifest
}

func (p Parts) Len() int { return len(p.Names) }

func (p *Parts) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.SequenceNode:
		return n.Decode(&p.Names)
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			var name string
			if err := n.Content[i].Decode(&name); err != nil {
				return err
			}
			p.Names = append(p.Names, name)
			if v := n.Content[i+1]; v.Tag != "!!null" {
				var m Manifest
				if err := v.Decode(&m); err != nil {
					return err
				}
				if m.Parts.Len() > 0 {
					return fmt.Errorf("line %d: part %q cannot declare parts of its own", v.Line, name)
				}
				if p.Inline == nil {
					p.Inline = map[string]*Manifest{}
				}
				p.Inline[name] = &m
			}
		}
		return nil
	}
	return fmt.Errorf("line %d: parts must be a list of directories or a map of parts", n.Line)
}

func (p Parts) MarshalYAML() (any, error) {
	if len(p.Inline) == 0 {
		return p.Names, nil
	}
	n := &yaml.Node{Kind: yaml.MappingNode}
	for _, name := range p.Names {
		val := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "~"}
		if m, ok := p.Inline[name]; ok {
			val = &yaml.Node{}
			if err := val.Encode(m); err != nil {
				return nil, err
			}
		}
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, val)
	}
	return n, nil
}

// Overlay is a Manifest without metadata or Hosts; used as the value of
// hosts.<name> and as the result of Resolve.
type Overlay struct {
	Packages Packages          `yaml:"packages,omitempty"`
	Omarchy  Omarchy           `yaml:"omarchy,omitempty"`
	Herdr    Herdr             `yaml:"herdr,omitempty"`
	Claude   Claude            `yaml:"claude,omitempty"`
	Files    map[string]string `yaml:"files,omitempty"`
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

// Claude shares Claude Code skills and slash commands. Skills is an omakase
// relative directory whose children are linked to ~/.claude/skills/<name>;
// Commands is a directory whose *.md files are linked to
// ~/.claude/commands/<name>.md. Linking per entry (not the whole directory)
// lets the machine keep its own skills alongside the shared ones.
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

// Resolve merges the overlay for host onto the base. Lists are unioned,
// plugin entries are keyed by URL/source with the overlay winning.
func (m *Manifest) Resolve(host string) Overlay {
	r := Overlay{
		Packages: m.Packages,
		Omarchy:  m.Omarchy,
		Herdr:    m.Herdr,
		Claude:   m.Claude,
		Files:    map[string]string{},
	}
	for k, v := range m.Files {
		r.Files[k] = v
	}
	o, ok := m.Hosts[host]
	if !ok {
		return r
	}
	return r.merge(o)
}

// merge layers o on top of r. Used both for host overlays and for stacking
// several omakases: later wins on scalars, lists are unioned.
func (r Overlay) merge(o Overlay) Overlay {
	r.Packages.Pacman = union(r.Packages.Pacman, o.Packages.Pacman)
	r.Packages.Aur = union(r.Packages.Aur, o.Packages.Aur)
	if o.Omarchy.Font != "" {
		r.Omarchy.Font = o.Omarchy.Font
	}
	r.Omarchy.Defaults = r.Omarchy.Defaults.merge(o.Omarchy.Defaults)
	r.Omarchy.Plugins = mergeOmarchyPlugins(r.Omarchy.Plugins, o.Omarchy.Plugins)
	r.Herdr.Plugins = mergeHerdrPlugins(r.Herdr.Plugins, o.Herdr.Plugins)
	if o.Claude.Skills != "" {
		r.Claude.Skills = o.Claude.Skills
	}
	if o.Claude.Commands != "" {
		r.Claude.Commands = o.Claude.Commands
	}
	if r.Files == nil {
		r.Files = map[string]string{}
	}
	for k, v := range o.Files {
		r.Files[k] = v
	}
	return r
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
