package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// A Omakase is a directory (usually a git checkout, or a part of one) holding
// an omasushi.yaml plus the files, skills and commands it refers to. Several
// omakases can be in use at once; they are layered in config order, later
// ones winning.
type Omakase struct {
	Name     string    // owner/repo, or owner/repo/part (base dir name for local omakases)
	Source   string    // what the user typed for the repository: owner/repo, URL, or local path
	Part     string    // sub-directory inside the repository ("" = the root manifest)
	Repo     string    // the checkout (git root); Update pulls here
	Dir      string    // Repo/Part: where omasushi.yaml and its files live
	Local    bool      // Repo is a user path, not managed by omasushi (never pulled)
	Uses     []Use     // use: declarations this omakase carries (resolved by resolveUses)
	Via      string    // name of the omakase whose use: pulled this one in ("" = configured directly)
	Only     Selection // set when a filtered use: reached it: take just these items (nil = all of it)
	Manifest *Manifest
	Root     *Manifest // set for a part written inline: the manifest that declares it
	Machine  *Machine  // set for the machine omakase: Save writes the whole file, recipe: included
}

// Resolve is this omakase's desired state for host: its manifest with the
// host overlay merged on, narrowed to what a filtered use: takes from it.
func (r Omakase) Resolve(host string) Overlay {
	return r.Manifest.Resolve(host).filter(r.Only)
}

func (r Omakase) ManifestPath() string { return filepath.Join(r.Dir, ManifestFile) }

// Save writes the omakase's manifest back to disk. An inline part is stored in
// its repository's root manifest, so that whole manifest is what gets written.
func (r Omakase) Save() error {
	switch {
	case r.Machine != nil:
		return r.Machine.Save()
	case r.Root != nil:
		return r.Root.Save(r.ManifestPath())
	}
	return r.Manifest.Save(r.ManifestPath())
}

// MachineName is what the machine manifest is called wherever omakases are
// named: diff's "<- machine", list, and export --to.
const MachineName = "machine"

// A Machine is this machine's own omakase, ~/.config/omasushi/omasushi.yaml.
// It is an ordinary manifest — packages, files, hosts, and a use: list of the
// omakases this machine takes from other people — with one key of its own:
// recipe:, the omakase this machine publishes.
//
// That makes three layers of the same format in three places: the omakases
// under use: at the bottom, the recipe over them, and this file over both.
// Only this one never leaves the machine, so whatever is particular to it —
// or simply not for sharing — stays out of the recipe by living here, and
// `publish` only ever has the recipe to offer.
type Machine struct {
	Recipe   string `yaml:"recipe,omitempty"`
	Manifest `yaml:",inline"`
}

func machineDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "omasushi")
	}
	return expandHome("~/.config/omasushi")
}

// machinePath is the machine manifest. It is an omasushi.yaml like any other:
// the same file an omakase repository carries, in the one place that is this
// machine's own.
func machinePath() string { return filepath.Join(machineDir(), ManifestFile) }

func omakasesDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "omasushi", "omakases")
	}
	return expandHome("~/.local/share/omasushi/omakases")
}

func LoadMachine() (*Machine, error) {
	b, err := os.ReadFile(machinePath())
	if os.IsNotExist(err) {
		return migrateConfig()
	}
	if err != nil {
		return nil, err
	}
	var m Machine
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", machinePath(), err)
	}
	return &m, nil
}

func (m *Machine) Save() error {
	if err := os.MkdirAll(machineDir(), 0o755); err != nil {
		return err
	}
	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(m); err != nil {
		return err
	}
	return os.WriteFile(machinePath(), []byte(sb.String()), 0o644)
}

// blank reports whether the machine manifest says nothing at all, which is
// when omasushi falls back to an omasushi.yaml in the working directory.
func (m *Machine) blank() bool {
	return m.Recipe == "" && len(m.Use) == 0 && m.Parts.Len() == 0 &&
		len(m.Hosts) == 0 && m.Resolve("").empty()
}

// Omakase is the machine manifest as the top layer of the stack: an omakase
// rooted at ~/.config/omasushi (so its files: paths live beside it), whose
// use: chain is what this machine takes from other people plus — last, so it
// wins over them — the recipe.
func (m *Machine) Omakase() Omakase {
	uses := append([]Use{}, m.Use...)
	if m.Recipe != "" {
		uses = append(uses, Use{Source: m.Recipe})
	}
	return Omakase{
		Name: MachineName, Source: machineDir(), Repo: machineDir(), Dir: machineDir(),
		Local: true, Uses: uses, Manifest: &m.Manifest, Machine: m,
	}
}

// recipeRepo is the checkout the recipe: source names — a directory of the
// user's own, or a managed clone — without cloning anything. "" when unset.
func (m *Machine) recipeRepo() string {
	if m.Recipe == "" {
		return ""
	}
	src, err := parseSource(m.Recipe)
	if err != nil {
		return ""
	}
	return checkoutDir(src)
}

// checkoutDir is where a source lives on disk: its own directory for a local
// one, the managed clone for a remote one (which may not exist yet).
func checkoutDir(src source) string {
	if src.Local {
		return src.Target
	}
	return filepath.Join(omakasesDir(), src.Name)
}

// migrateConfig converts the pre-machine-manifest ~/.config/omasushi/config.yaml
// — an omakases: list plus mine: — into the machine manifest, once. The old
// file is left where it is, unread from then on.
func migrateConfig() (*Machine, error) {
	legacy := filepath.Join(machineDir(), "config.yaml")
	b, err := os.ReadFile(legacy)
	if os.IsNotExist(err) {
		return &Machine{}, nil
	}
	if err != nil {
		return nil, err
	}
	var old struct {
		Omakases []struct {
			Name   string `yaml:"name"`
			Source string `yaml:"source"`
			Part   string `yaml:"part"`
		} `yaml:"omakases"`
		Mine string `yaml:"mine"`
	}
	if err := yaml.Unmarshal(b, &old); err != nil {
		return nil, fmt.Errorf("%s: %w", legacy, err)
	}
	m := &Machine{}
	for _, ref := range old.Omakases {
		source := omakaseName(ref.Source, ref.Part)
		if ref.Name == old.Mine || (old.Mine != "" && strings.HasPrefix(old.Mine, ref.Name+"/")) {
			m.Recipe = ref.Source // the recipe is a repository; publish takes it whole
			continue
		}
		m.use(source)
	}
	if err := m.Save(); err != nil {
		return nil, err
	}
	fmt.Fprintf(os.Stderr, "note: %s is now %s (omakases: -> use:, mine: -> recipe:); the old file is no longer read\n",
		tildify(legacy), tildify(machinePath()))
	return m, nil
}

// source is a parsed omakase source: the repository plus an optional part.
type source struct {
	Repo   string // what to record: owner/repo, URL, or local path (part stripped)
	Name   string // owner/repo (checkout directory under omakasesDir); base name for local dirs
	Target string // git URL or absolute local dir
	Local  bool
	Part   string // sub-directory, "" for the root
}

// parseSource turns user input into a source.
//
//	owner/repo                  -> https://github.com/owner/repo.git
//	owner/repo/part             -> same repository, Part = part
//	https://github.com/o/r/part -> Part = part (github.com / gitlab.com only)
//	https://... / git@...       -> as is
//	./dir, ../dir, ~/dir, /abs, or an existing directory -> local
func parseSource(s string) (source, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return source{}, fmt.Errorf("empty omakase source")
	}
	isPathy := strings.HasPrefix(s, "/") || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") ||
		strings.HasPrefix(s, "~") || s == "."
	if !isPathy {
		if fi, statErr := os.Stat(s); statErr == nil && fi.IsDir() && !strings.Contains(s, "://") {
			isPathy = true
		}
	}
	if isPathy {
		abs, err := filepath.Abs(expandHome(s))
		if err != nil {
			return source{}, err
		}
		return source{Repo: abs, Name: filepath.Base(abs), Target: abs, Local: true}, nil
	}

	var src source
	switch {
	case strings.Contains(s, "://") || strings.HasPrefix(s, "git@"):
		src.Repo, src.Part = splitURLPart(s)
		src.Target = src.Repo
	default:
		parts := strings.Split(strings.Trim(s, "/"), "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return source{}, fmt.Errorf("omakase source must be owner/repo[/part], a git URL, or a local path: %q", s)
		}
		src.Repo = parts[0] + "/" + parts[1]
		src.Target = "https://github.com/" + src.Repo + ".git"
		src.Part = strings.Join(parts[2:], "/")
	}
	if err := checkPart(src.Part); err != nil {
		return source{}, err
	}
	src.Name = repoPath(src.Target)
	return src, nil
}

// repoPath is the host-less path of a git URL: https://github.com/a/b.git
// and git@github.com:a/b both give "a/b". Checkouts live at
// omakasesDir()/<repoPath>, so the same repository name under two owners
// never collides.
func repoPath(u string) string {
	for _, p := range []string{"ssh://", "git://"} {
		u = strings.TrimPrefix(strings.TrimSpace(u), p)
	}
	n := strings.TrimPrefix(normalizeGitURL(u), "git@")
	if i := strings.Index(n, "/"); i >= 0 {
		n = n[i+1:]
	}
	return strings.Trim(n, "/")
}

// splitURLPart splits https://github.com/owner/repo/part into the repository
// URL and the part. Only github.com / gitlab.com URLs have a fixed owner/repo
// shape; anything else is returned untouched with no part.
func splitURLPart(s string) (repo, part string) {
	t := strings.TrimSuffix(strings.TrimSpace(s), "/")
	for _, p := range []string{"https://", "http://", "ssh://", "git://"} {
		t = strings.TrimPrefix(t, p)
	}
	t = strings.TrimPrefix(t, "git@")
	t = strings.Replace(t, ":", "/", 1)
	t = strings.TrimPrefix(t, "www.")
	segs := strings.Split(t, "/")
	if len(segs) <= 3 {
		return s, ""
	}
	u, err := canonicalRepoURL(strings.Join(segs[:3], "/"))
	if err != nil {
		return s, ""
	}
	return u + ".git", strings.Join(segs[3:], "/")
}

func checkPart(p string) error {
	if p == "" {
		return nil
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.HasPrefix(seg, ".") {
			return fmt.Errorf("bad omakase part %q", p)
		}
	}
	return nil
}

// resolveSource is the repository-level view of parseSource, kept for callers
// that only care about where the checkout comes from.
func resolveSource(s string) (name, target string, local bool, err error) {
	src, err := parseSource(s)
	if err != nil {
		return "", "", false, err
	}
	return src.Name, src.Target, src.Local, nil
}

func omakaseName(repo, part string) string {
	if part == "" {
		return repo
	}
	return repo + "/" + part
}

// omakasesIn loads src.Part of the checkout at repo; for the root of a
// repository that declares parts it loads every part instead. A part is either
// a directory of its own or written inline in the root manifest, in which case
// it is the repository root narrowed to that part's sections.
func omakasesIn(src source, repo, name string) ([]Omakase, error) {
	dirPart := func(part string) (Omakase, error) {
		r := Omakase{Name: omakaseName(src.Name, part), Source: src.Repo, Part: part, Repo: repo, Dir: filepath.Join(repo, part), Local: src.Local}
		if part == "" && name != "" {
			r.Name = name
		}
		if _, err := os.Stat(r.Dir); err != nil {
			return r, fmt.Errorf("omakase %s: no %s directory in %s", r.Name, part, repo)
		}
		m, err := LoadManifest(r.ManifestPath())
		if err != nil {
			return r, err
		}
		if part != "" {
			if _, err := os.Stat(r.ManifestPath()); err != nil {
				return r, fmt.Errorf("omakase %s: %s has no %s", r.Name, r.Dir, ManifestFile)
			}
		}
		r.Manifest = m
		r.Uses = m.Use
		return r, nil
	}
	root, err := dirPart("")
	if err != nil {
		return nil, err
	}
	// An inline part shares the root's directory, so its paths are relative to
	// the repository root. Manifest points into root.Parts.Inline, which is what
	// lets Save fold an export back into the root manifest.
	inlinePart := func(part string) Omakase {
		m := root.Manifest.Parts.Inline[part]
		return Omakase{
			Name: omakaseName(src.Name, part), Source: src.Repo, Part: part,
			Repo: repo, Dir: repo, Local: src.Local,
			Manifest: m, Root: root.Manifest, Uses: m.Use,
		}
	}
	if src.Part != "" {
		if _, ok := root.Manifest.Parts.Inline[src.Part]; ok {
			return []Omakase{inlinePart(src.Part)}, nil
		}
		r, err := dirPart(src.Part)
		if err != nil {
			return nil, err
		}
		return []Omakase{r}, nil
	}
	if root.Manifest.Parts.Len() == 0 {
		return []Omakase{root}, nil
	}
	var out []Omakase
	for _, p := range root.Manifest.Parts.Names {
		if err := checkPart(p); err != nil {
			return nil, fmt.Errorf("%s: %w", root.ManifestPath(), err)
		}
		if _, ok := root.Manifest.Parts.Inline[p]; ok {
			out = append(out, inlinePart(p))
			continue
		}
		r, err := dirPart(p)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	// The root's use: belongs to the bundle as a whole; carrying it on the
	// first part keeps its omakases layered before every part.
	if len(out) > 0 && len(root.Manifest.Use) > 0 {
		out[0].Uses = append(append([]Use{}, root.Manifest.Use...), out[0].Uses...)
	}
	return out, nil
}

// omakaseFromDir treats an arbitrary directory as the active omakase set (for
// `-f path/omasushi.yaml`, and for running inside an omakase checkout). A root
// manifest with parts yields one omakase per part.
func omakaseFromDir(manifestPath string) ([]Omakase, error) {
	abs, err := filepath.Abs(manifestPath)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, err
	}
	dir := filepath.Dir(abs)
	src := source{Repo: dir, Name: filepath.Base(dir), Target: dir, Local: true}
	return omakasesIn(src, dir, "")
}

// resolveUses expands each omakase's use: declarations into the omakases they
// name, layered before the declaring omakase so that it wins on conflicts.
// Their checkouts are managed like `omasushi use` ones (and pulled by update),
// but they are not recorded in the config: the declaring manifest is their
// source of truth.
//
// An entry with only: narrows what is taken, and the narrowing governs
// everything that entry pulls in — the used omakase's own use: chain included
// — so cherry-picking one package never drags a stranger's whole tree in
// behind it. A part left with nothing to contribute drops out entirely rather
// than sit in `list` doing nothing.
//
// A repository already loaded — directly, or through another use: — keeps its
// first position, which also makes cycles harmless; reaching it a second time
// only widens what is taken from it.
func resolveUses(rs []Omakase) ([]Omakase, error) {
	at := map[string]*Omakase{}
	var out []*Omakase
	var add func(rs []Omakase, via string, only Selection) error
	add = func(rs []Omakase, via string, only Selection) error {
		for _, r := range rs {
			if have, ok := at[r.Name]; ok {
				have.Only = widen(have.Only, only)
				continue
			}
			r.Via, r.Only = via, only
			at[r.Name] = &r
			// The machine manifest is the root of the stack, not a middleman:
			// what it uses is what the user asked for directly.
			mine := r.Name
			if mine == MachineName {
				mine = ""
			}
			for _, u := range r.Uses {
				deps, err := loadUse(u, r.Repo)
				if err != nil {
					return fmt.Errorf("%s: use %s: %w", r.Name, u.Source, err)
				}
				// An only: already in force keeps its say over the whole
				// chain; a wider one below it cannot widen it back.
				sub := only
				if sub == nil {
					sub = u.Only
				}
				if err := add(deps, mine, sub); err != nil {
					return err
				}
			}
			out = append(out, at[r.Name])
		}
		return nil
	}
	if err := add(rs, "", nil); err != nil {
		return nil, err
	}
	final := make([]Omakase, 0, len(out))
	for _, r := range out {
		if r.Only != nil && !r.Manifest.selects(r.Only) {
			continue
		}
		final = append(final, *r)
	}
	return final, nil
}

// loadUse materialises one use: entry. A relative path resolves against the
// declaring omakase's repository, so a repo can point at a sibling directory.
func loadUse(u Use, fromDir string) ([]Omakase, error) {
	entry := u.Source
	if strings.HasPrefix(entry, "./") || strings.HasPrefix(entry, "../") {
		entry = filepath.Join(fromDir, entry)
	}
	src, err := parseSource(entry)
	if err != nil {
		return nil, err
	}
	repo, err := ensureCheckout(src)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(repo, ManifestFile)); err != nil {
		return nil, fmt.Errorf("%s has no %s", repo, ManifestFile)
	}
	return omakasesIn(src, repo, "")
}

// ensureCheckout returns the checkout directory for src, cloning a remote
// source that is not on disk yet. It never pulls; `omasushi update` does.
func ensureCheckout(src source) (string, error) {
	if src.Local {
		return src.Target, nil
	}
	repo := filepath.Join(omakasesDir(), src.Name)
	if _, err := os.Stat(repo); err == nil {
		return repo, nil
	}
	if err := os.MkdirAll(omakasesDir(), 0o755); err != nil {
		return "", err
	}
	if err := runVisible("git", "clone", "--depth", "1", src.Target, repo); err != nil {
		return "", err
	}
	return repo, nil
}

// Add records an omakase under the machine manifest's use:, cloning a remote
// source (or refreshing an existing checkout) first. What the user typed is
// what gets written — `owner/repo` on a split repository records the
// repository, so parts added to it later come along on their own — and the
// omakases it resolves to are returned for the caller to report.
//
// Adding it as the recipe puts it in recipe: instead: that slot is the one
// omakase this machine publishes and exports to, not one it merely uses.
func (m *Machine) Add(input string, recipe bool) ([]Omakase, error) {
	src, err := parseSource(input)
	if err != nil {
		return nil, err
	}
	if !src.Local {
		repo := filepath.Join(omakasesDir(), src.Name)
		if _, err := os.Stat(repo); err == nil {
			if err := runVisible("git", "-C", repo, "pull", "--ff-only"); err != nil {
				return nil, err
			}
		}
	}
	repo, err := ensureCheckout(src)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(repo, ManifestFile)); err != nil {
		return nil, fmt.Errorf("%s has no %s", repo, ManifestFile)
	}
	rs, err := omakasesIn(src, repo, "")
	if err != nil {
		return nil, err
	}
	source := omakaseName(src.Repo, src.Part)
	if recipe {
		m.Recipe = source
	} else {
		m.use(source)
	}
	return rs, m.Save()
}

// use appends a source to use: unless it is already there, keeping whatever
// only: that entry carries.
func (m *Machine) use(source string) {
	for _, u := range m.Use {
		if sameSource(u.Source, source) {
			return
		}
	}
	m.Use = append(m.Use, Use{Source: source})
}

// sameSource reports whether two use: entries name the same omakase, so that
// polidog/omakase and https://github.com/polidog/omakase.git count as one.
func sameSource(a, b string) bool {
	sa, ea := parseSource(a)
	sb, eb := parseSource(b)
	if ea != nil || eb != nil {
		return a == b
	}
	return sa.Target == sb.Target && sa.Part == sb.Part
}

// Remove drops an omakase from use:. A single part of a repository recorded
// whole is dropped by replacing that entry with its siblings, so `remove
// owner/repo/herdr` keeps working on a repository added as `owner/repo`. The
// managed checkout goes once nothing points at that repository any more.
func (m *Machine) Remove(name string) error {
	for i, u := range m.Use {
		src, err := parseSource(u.Source)
		if err != nil || omakaseName(src.Name, src.Part) != name {
			continue
		}
		m.Use = append(m.Use[:i:i], m.Use[i+1:]...)
		m.dropCheckout(src)
		return m.Save()
	}
	for i, u := range m.Use {
		src, err := parseSource(u.Source)
		if err != nil || src.Part != "" || !strings.HasPrefix(name, src.Name+"/") {
			continue
		}
		part := strings.TrimPrefix(name, src.Name+"/")
		root, err := LoadManifest(filepath.Join(checkoutDir(src), ManifestFile))
		if err != nil {
			return err
		}
		if !slices.Contains(root.Parts.Names, part) {
			continue
		}
		if src.Local {
			return fmt.Errorf("%s is one part of the local omakase %s; remove the whole path, or point use: at the parts you want", name, u.Source)
		}
		var kept []Use
		for _, p := range root.Parts.Names {
			if p != part {
				kept = append(kept, Use{Source: omakaseName(u.Source, p), Only: u.Only})
			}
		}
		m.Use = append(m.Use[:i:i], append(kept, m.Use[i+1:]...)...)
		return m.Save()
	}
	if m.recipeRepo() != "" && (name == m.Recipe || strings.HasPrefix(name+"/", recipeNamePrefix(m))) {
		return fmt.Errorf("%s is this machine's recipe; drop it with `omasushi recipe none`", name)
	}
	return fmt.Errorf("no omakase named %q", name)
}

// recipeNamePrefix is the recipe's omakase name with a trailing slash, for
// spotting one of its parts.
func recipeNamePrefix(m *Machine) string {
	src, err := parseSource(m.Recipe)
	if err != nil {
		return "\x00"
	}
	return omakaseName(src.Name, src.Part) + "/"
}

// dropCheckout deletes a managed clone once no use: entry and no recipe still
// points at that repository.
func (m *Machine) dropCheckout(src source) {
	if src.Local || m.usesRepo(src.Name) {
		return
	}
	dir := filepath.Join(omakasesDir(), src.Name)
	if strings.HasPrefix(dir, omakasesDir()) {
		os.RemoveAll(dir)
		os.Remove(filepath.Dir(dir)) // owner dir, only if now empty
	}
}

func (m *Machine) usesRepo(repoName string) bool {
	sources := make([]string, 0, len(m.Use)+1)
	for _, u := range m.Use {
		sources = append(sources, u.Source)
	}
	if m.Recipe != "" {
		sources = append(sources, m.Recipe)
	}
	for _, s := range sources {
		if src, err := parseSource(s); err == nil && !src.Local && src.Name == repoName {
			return true
		}
	}
	return false
}

// Update pulls every remote omakase repository once. Local omakases are left alone.
func Update(omakases []Omakase) error {
	done := map[string]bool{}
	for _, r := range omakases {
		if done[r.Repo] {
			continue
		}
		done[r.Repo] = true
		if r.Local {
			fmt.Printf("==> %s: local, skipped\n", r.Name)
			continue
		}
		fmt.Printf("==> %s\n", repoLabel(omakases, r.Repo))
		if err := runVisible("git", "-C", r.Repo, "pull", "--ff-only"); err != nil {
			return err
		}
	}
	return nil
}

// repoLabel names a checkout by the omakases using it: "omakase (herdr, kitty)".
func repoLabel(omakases []Omakase, repo string) string {
	var parts []string
	base := filepath.Base(repo)
	for _, r := range omakases {
		if r.Repo == repo && r.Part != "" {
			parts = append(parts, r.Part)
		}
	}
	if len(parts) == 0 {
		return base
	}
	sort.Strings(parts)
	return fmt.Sprintf("%s (%s)", base, strings.Join(parts, ", "))
}

func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}
