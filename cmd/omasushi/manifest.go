package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
// Use names other omakases this one builds on (owner/repo[/part], URL, or a
// path — relative ones resolve against this repository). They are layered
// underneath the declaring omakase, so it wins on conflicts; see resolveUses.
// An entry may narrow what it takes with only:, down to single packages,
// plugins, files or skills.
//
// A repository may instead be split into feature-sized parts (herdr, kitty,
// claude …) declared in Parts; `omasushi use owner/repo` takes all of them and
// `omasushi use owner/repo/herdr` just one. A manifest that declares parts is
// only their index: its own sections are not applied.
type Manifest struct {
	Name        string             `yaml:"name,omitempty"`
	Description string             `yaml:"description,omitempty"`
	Use         []Use              `yaml:"use,omitempty"`
	Parts       Parts              `yaml:"parts,omitempty"`
	Packages    Packages           `yaml:"packages,omitempty"`
	Omarchy     Omarchy            `yaml:"omarchy,omitempty"`
	Herdr       Herdr              `yaml:"herdr,omitempty"`
	Claude      Claude             `yaml:"claude,omitempty"`
	Agent       Claude             `yaml:"agent,omitempty"`
	Files       map[string]string  `yaml:"files,omitempty"`
	Hosts       map[string]Overlay `yaml:"hosts,omitempty"`
}

// Use is one entry of a manifest's use: list. The short spelling is the bare
// source, which takes the whole omakase; the long one adds only:, which takes
// just the named items and leaves the rest on the belt:
//
//	use:
//	  - polidog/omakase             # all of it
//	  - source: someone/big
//	    only:
//	      packages.aur: [kitty]
//	      files: [files/kitty/kitty.conf]
type Use struct {
	Source string    `yaml:"source"`
	Only   Selection `yaml:"only,omitempty"`
}

func (u *Use) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		return n.Decode(&u.Source)
	}
	type plain Use // no UnmarshalYAML, so Decode does not recurse
	var p plain
	if err := n.Decode(&p); err != nil {
		return err
	}
	if p.Source == "" {
		return fmt.Errorf("line %d: a use: entry needs a source", n.Line)
	}
	if err := p.Only.check(); err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	*u = Use(p)
	return nil
}

func (u Use) MarshalYAML() (any, error) {
	if len(u.Only) == 0 {
		return u.Source, nil
	}
	type plain Use
	return plain(u), nil
}

// Selection is a use: entry's only: block: dotted paths into the manifest of
// the omakase being used, each with what to take from it. A path with no list
// takes everything under it; under a leaf the list names that leaf's own
// entries (packages.aur: [kitty]), under a section it names the sub-keys to
// descend into (packages: [aur]), and the most specific path that matches
// decides. files: accepts either side of a mapping, the omakase path or the
// destination.
type Selection map[string][]string

// selectable is every path only: can address, so a typo fails loudly instead
// of quietly selecting nothing.
var selectable = []string{
	"packages.pacman", "packages.aur",
	"omarchy.font",
	"omarchy.defaults.agent", "omarchy.defaults.browser",
	"omarchy.defaults.editor", "omarchy.defaults.terminal",
	"omarchy.plugins",
	"herdr.plugins",
	"claude.skills", "claude.commands",
	"agent.skills", "agent.commands",
	"files",
}

func (s *Selection) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: only: must be a map of manifest paths", n.Line)
	}
	out := Selection{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		var key string
		if err := n.Content[i].Decode(&key); err != nil {
			return err
		}
		v := n.Content[i+1]
		switch {
		case v.Tag == "!!null" || v.Tag == "!!bool":
			out[key] = nil // the whole path
		case v.Kind == yaml.SequenceNode:
			var items []string
			if err := v.Decode(&items); err != nil {
				return err
			}
			out[key] = items
		case v.Kind == yaml.ScalarNode:
			out[key] = []string{v.Value}
		default:
			return fmt.Errorf("line %d: only: %s must be a list of entries", v.Line, key)
		}
	}
	*s = out
	return nil
}

func (s Selection) check() error {
	for k := range s {
		known := false
		for _, p := range selectable {
			if p == k || strings.HasPrefix(p, k+".") {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("only: %q is not a path into a manifest", k)
		}
	}
	return nil
}

// takes reports whether path is selected at all, and which of its entries:
// items nil with ok means all of them. A path not named itself is answered by
// its nearest named ancestor, whose list then names path's next segment.
func (s Selection) takes(path string) (items []string, ok bool) {
	if s == nil {
		return nil, true // no only: — the whole omakase
	}
	if items, ok = s[path]; ok {
		return items, true
	}
	segs := strings.Split(path, ".")
	for i := len(segs) - 1; i > 0; i-- {
		sub, ok := s[strings.Join(segs[:i], ".")]
		if !ok {
			continue
		}
		return nil, sub == nil || slices.Contains(sub, segs[i])
	}
	return nil, false
}

// keeps reports whether one entry of the collection at path is selected.
func (s Selection) keeps(path, item string) bool {
	items, ok := s.takes(path)
	return ok && (items == nil || slices.Contains(items, item))
}

// keepsURL is keeps for git URLs, which compare normalised.
func (s Selection) keepsURL(path, u string) bool {
	items, ok := s.takes(path)
	if !ok || items == nil {
		return ok
	}
	for _, it := range items {
		if normalizeGitURL(it) == normalizeGitURL(u) {
			return true
		}
	}
	return false
}

// paths is the manifest paths a selection names, for reporting what a filtered
// use: takes. Nil for an omakase taken whole.
func (s Selection) paths() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// widen combines two selections of the same omakase, used when it is reached
// twice: nil ("all of it") wins, and otherwise the entries add up.
func widen(a, b Selection) Selection {
	if a == nil || b == nil {
		return nil
	}
	out := Selection{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		items, seen := out[k]
		switch {
		case !seen:
			out[k] = v
		case items == nil || v == nil:
			out[k] = nil
		default:
			out[k] = union(items, v)
		}
	}
	return out
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
	Agent    Claude            `yaml:"agent,omitempty"`
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

// Resolve merges the overlay for host onto the base. Lists are unioned,
// plugin entries are keyed by URL/source with the overlay winning.
func (m *Manifest) Resolve(host string) Overlay {
	r := Overlay{
		Packages: m.Packages,
		Omarchy:  m.Omarchy,
		Herdr:    m.Herdr,
		Claude:   m.Claude,
		Agent:    m.Agent,
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
	if o.Agent.Skills != "" {
		r.Agent.Skills = o.Agent.Skills
	}
	if o.Agent.Commands != "" {
		r.Agent.Commands = o.Agent.Commands
	}
	if r.Files == nil {
		r.Files = map[string]string{}
	}
	for k, v := range o.Files {
		r.Files[k] = v
	}
	return r
}

// filter narrows an overlay to what sel selects; a nil Selection takes it
// whole. The skills/commands directories survive as long as anything under
// them is selected — which entries of a directory are linked is decided per
// entry, in agentLinks.
func (r Overlay) filter(sel Selection) Overlay {
	if sel == nil {
		return r
	}
	var out Overlay
	for _, p := range r.Packages.Pacman {
		if sel.keeps("packages.pacman", p) {
			out.Packages.Pacman = append(out.Packages.Pacman, p)
		}
	}
	for _, p := range r.Packages.Aur {
		if sel.keeps("packages.aur", p) {
			out.Packages.Aur = append(out.Packages.Aur, p)
		}
	}
	if _, ok := sel.takes("omarchy.font"); ok {
		out.Omarchy.Font = r.Omarchy.Font
	}
	for _, d := range []struct {
		path string
		from string
		to   *string
	}{
		{"omarchy.defaults.agent", r.Omarchy.Defaults.Agent, &out.Omarchy.Defaults.Agent},
		{"omarchy.defaults.browser", r.Omarchy.Defaults.Browser, &out.Omarchy.Defaults.Browser},
		{"omarchy.defaults.editor", r.Omarchy.Defaults.Editor, &out.Omarchy.Defaults.Editor},
		{"omarchy.defaults.terminal", r.Omarchy.Defaults.Terminal, &out.Omarchy.Defaults.Terminal},
	} {
		if _, ok := sel.takes(d.path); ok {
			*d.to = d.from
		}
	}
	for _, p := range r.Omarchy.Plugins {
		if sel.keepsURL("omarchy.plugins", p.URL) {
			out.Omarchy.Plugins = append(out.Omarchy.Plugins, p)
		}
	}
	for _, p := range r.Herdr.Plugins {
		if sel.keeps("herdr.plugins", p.Source) {
			out.Herdr.Plugins = append(out.Herdr.Plugins, p)
		}
	}
	for _, c := range []struct {
		path string
		from Claude
		to   *Claude
	}{{"claude", r.Claude, &out.Claude}, {"agent", r.Agent, &out.Agent}} {
		if _, ok := sel.takes(c.path + ".skills"); ok {
			c.to.Skills = c.from.Skills
		}
		if _, ok := sel.takes(c.path + ".commands"); ok {
			c.to.Commands = c.from.Commands
		}
	}
	for k, v := range r.Files {
		if !sel.keeps("files", k) && !sel.keeps("files", v) {
			continue
		}
		if out.Files == nil {
			out.Files = map[string]string{}
		}
		out.Files[k] = v
	}
	return out
}

// empty reports whether an overlay declares nothing at all, which is what a
// filtered use: leaves behind for the parts it takes nothing from.
func (r Overlay) empty() bool {
	return len(r.Packages.Pacman) == 0 && len(r.Packages.Aur) == 0 &&
		r.Omarchy.Font == "" && r.Omarchy.Defaults == (Defaults{}) &&
		len(r.Omarchy.Plugins) == 0 && len(r.Herdr.Plugins) == 0 &&
		r.Claude == (Claude{}) && r.Agent == (Claude{}) && len(r.Files) == 0
}

// selects reports whether sel takes anything from this manifest, on any host.
func (m *Manifest) selects(sel Selection) bool {
	if !m.Resolve("").filter(sel).empty() {
		return true
	}
	for h := range m.Hosts {
		if !m.Resolve(h).filter(sel).empty() {
			return true
		}
	}
	return false
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
