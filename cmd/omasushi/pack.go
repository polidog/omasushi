package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// A Pack is a directory holding an omasushi.yaml plus the files, skills and
// snippets it refers to: everything one feature needs on top of stock
// Omarchy. A repository holds one pack per subdirectory (or is itself one pack
// when it has no such subdirectory). Several packs are in use at once; they
// are layered in order, later ones winning.
type Pack struct {
	Name     string   // owner/repo/pack, or owner/repo for a single-pack repository (base dir names for local ones)
	Source   string   // what the user typed for the repository: owner/repo, URL, or local path
	Sub      string   // the pack's directory inside the repository ("" = the repository is the pack)
	Repo     string   // the checkout (git root); Update pulls here
	Dir      string   // Repo/Sub: where omasushi.yaml and its files live
	Local    bool     // Repo is a user path, not managed by omasushi (never pulled)
	Ref      string   // the tag, branch or commit the checkout is pinned to ("" = default branch, pulled by update)
	Uses     []string // use: this pack carries (resolved by resolveUses)
	Via      string   // name of the pack whose use: pulled this one in ("" = taken directly)
	Manifest *Manifest
	Machine  *Local // set for this machine's own file
}

func (p Pack) ManifestPath() string { return filepath.Join(p.Dir, ManifestFile) }

// Save writes the pack's manifest back to disk.
func (p Pack) Save() error {
	if p.Machine != nil {
		return p.Machine.Save()
	}
	return p.Manifest.Save(p.ManifestPath())
}

// LocalName is what this machine's own file is called wherever packs are
// named: diff's "<- local", list, status.
const LocalName = "local"

// A Local is this machine's own file, ~/.config/omasushi/omasushi.yaml: the
// packs it takes under use:, and what belongs to the machine alone — a work
// VPN, a monitor layout — in the same sections a pack has. It is layered on
// top of everything it uses, so the machine has the last word, and it is the
// one file publish will not touch.
type Local struct {
	Manifest `yaml:",inline"`
}

func localDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "omasushi")
	}
	return expandHome("~/.config/omasushi")
}

func localPath() string { return filepath.Join(localDir(), ManifestFile) }

// packsDir holds the managed checkouts, one per repository.
func packsDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "omasushi", "packs")
	}
	return expandHome("~/.local/share/omasushi/packs")
}

// LoadLocal reads this machine's file. A recipe: key from before packs (the
// repository this machine published) is folded into use: once, since export
// now names the pack it writes to and publish the checkout it stands in.
func LoadLocal() (*Local, error) {
	migrateCheckouts()
	b, err := os.ReadFile(localPath())
	if os.IsNotExist(err) {
		return &Local{}, nil
	}
	if err != nil {
		return nil, err
	}
	var old struct {
		Recipe   string `yaml:"recipe"`
		Manifest `yaml:",inline"`
	}
	if err := yaml.Unmarshal(b, &old); err != nil {
		return nil, fmt.Errorf("%s: %w", localPath(), err)
	}
	l := &Local{Manifest: old.Manifest}
	if old.Recipe != "" {
		l.use(old.Recipe)
		if err := l.Save(); err != nil {
			return nil, err
		}
		fmt.Fprintf(os.Stderr, "note: recipe: is gone from %s — %s now sits under use:; export names the pack it writes to, publish the checkout you stand in\n",
			tildify(localPath()), old.Recipe)
	}
	return l, nil
}

// migrateCheckouts moves the managed clones from the pre-packs directory,
// once, so nothing is cloned twice.
func migrateCheckouts() {
	old := filepath.Join(filepath.Dir(packsDir()), "omakases")
	if _, err := os.Stat(packsDir()); err == nil {
		return
	}
	if _, err := os.Stat(old); err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(packsDir()), 0o755)
	os.Rename(old, packsDir())
}

func (l *Local) Save() error {
	if err := os.MkdirAll(localDir(), 0o755); err != nil {
		return err
	}
	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(l); err != nil {
		return err
	}
	return os.WriteFile(localPath(), []byte(sb.String()), 0o644)
}

// blank reports whether this machine's file says nothing at all, which is
// when omasushi falls back to an omasushi.yaml in the working directory.
func (l *Local) blank() bool {
	return !l.Manifest.declares()
}

// Pack is this machine's file as the top layer of the stack: a pack rooted
// at ~/.config/omasushi (so its files: paths live beside it), whose use: is
// what this machine takes.
func (l *Local) Pack() Pack {
	return Pack{
		Name: LocalName, Source: localDir(), Repo: localDir(), Dir: localDir(),
		Local: true, Uses: l.Use, Manifest: &l.Manifest, Machine: l,
	}
}

// checkoutDir is where a source lives on disk: its own directory for a local
// one, the managed clone for a remote one (which may not exist yet).
func checkoutDir(src source) string {
	if src.Local {
		return src.Target
	}
	return filepath.Join(packsDir(), src.Name)
}

// source is a parsed pack source: the repository plus an optional pack in it.
type source struct {
	Repo   string // what to record: owner/repo, URL, or local path (pack stripped)
	Name   string // owner/repo (checkout directory under packsDir); base name for local dirs
	Target string // git URL or absolute local dir
	Local  bool
	Sub    string // the pack's directory in the repository, "" for the root
	Ref    string // owner/repo/pack@ref: the tag, branch or commit to pin the checkout to
}

// String is the use: entry for src: what parseSource read, pack and pin included.
func (src source) String() string {
	s := packName(src.Repo, src.Sub)
	if src.Ref != "" {
		s += "@" + src.Ref
	}
	return s
}

// parseSource turns user input into a source.
//
//	owner/repo                  -> https://github.com/owner/repo.git
//	owner/repo/pack             -> same repository, Sub = pack
//	https://github.com/o/r/pack -> Sub = pack (github.com / gitlab.com only)
//	https://... / git@...       -> as is
//	any of those @v1.2.0        -> Ref = v1.2.0 (a tag, branch or commit); local dirs take none
//	./dir, ../dir, ~/dir, /abs, or an existing directory -> local
func parseSource(s string) (source, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return source{}, fmt.Errorf("empty pack source")
	}
	var ref string
	// git@host:a/b has an @ too; a ref never holds a / or :.
	if i := strings.LastIndex(s, "@"); i > 0 && !strings.ContainsAny(s[i+1:], "/:") {
		s, ref = s[:i], s[i+1:]
		if ref == "" {
			return source{}, fmt.Errorf("empty ref after @ in %q", s)
		}
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
		src.Repo, src.Sub = splitURLSub(s)
		src.Target = src.Repo
	default:
		parts := strings.Split(strings.Trim(s, "/"), "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return source{}, fmt.Errorf("pack source must be owner/repo[/pack], a git URL, or a local path: %q", s)
		}
		src.Repo = parts[0] + "/" + parts[1]
		src.Target = "https://github.com/" + src.Repo + ".git"
		src.Sub = strings.Join(parts[2:], "/")
	}
	if err := checkSub(src.Sub); err != nil {
		return source{}, err
	}
	src.Name = repoPath(src.Target)
	src.Ref = ref
	return src, nil
}

// repoPath is the host-less path of a git URL: https://github.com/a/b.git
// and git@github.com:a/b both give "a/b". Checkouts live at
// packsDir()/<repoPath>, so the same repository name under two owners never
// collides.
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

// splitURLSub splits https://github.com/owner/repo/pack into the repository
// URL and the pack. Only github.com / gitlab.com URLs have a fixed owner/repo
// shape; anything else is returned untouched with no pack.
func splitURLSub(s string) (repo, sub string) {
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

func checkSub(p string) error {
	if p == "" {
		return nil
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." || strings.HasPrefix(seg, ".") {
			return fmt.Errorf("bad pack path %q", p)
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

func packName(repo, sub string) string {
	if sub == "" {
		return repo
	}
	return repo + "/" + sub
}

// packDirs lists the subdirectories of repo that carry an omasushi.yaml —
// the packs of a repository — in name order. Hidden directories are skipped.
func packDirs(repo string) []string {
	var out []string
	for _, e := range readDirSorted(repo) {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(repo, e.Name(), ManifestFile)); err == nil {
			out = append(out, e.Name())
		}
	}
	return out
}

// isPackRepo reports whether dir holds an omasushi.yaml or packs that do.
func isPackRepo(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, ManifestFile)); err == nil {
		return true
	}
	return len(packDirs(dir)) > 0
}

// packsIn loads src.Sub of the checkout at repo. For the root of a repository
// it loads every pack — each subdirectory with an omasushi.yaml — or, when
// there is none, the repository itself as one pack. A root that has packs is
// only their index: it may name the repository, and nothing else.
func packsIn(src source, repo, name string) ([]Pack, error) {
	load := func(sub string) (Pack, error) {
		p := Pack{Name: packName(src.Name, sub), Source: src.Repo, Sub: sub, Repo: repo, Dir: filepath.Join(repo, sub), Local: src.Local, Ref: src.Ref}
		if sub == "" && name != "" {
			p.Name = name
		}
		if _, err := os.Stat(p.ManifestPath()); err != nil {
			if sub == "" {
				return p, fmt.Errorf("%s has no %s", repo, ManifestFile)
			}
			return p, fmt.Errorf("pack %s: no %s in %s", p.Name, ManifestFile, p.Dir)
		}
		m, err := LoadManifest(p.ManifestPath())
		if err != nil {
			return p, err
		}
		p.Manifest = m
		p.Uses = m.Use
		return p, nil
	}
	if src.Sub != "" {
		p, err := load(src.Sub)
		if err != nil {
			return nil, err
		}
		return []Pack{p}, nil
	}
	subs := packDirs(repo)
	if len(subs) == 0 {
		p, err := load("")
		if err != nil {
			return nil, err
		}
		return []Pack{p}, nil
	}
	root, err := LoadManifest(filepath.Join(repo, ManifestFile))
	if err != nil {
		return nil, err
	}
	if root.declares() {
		return nil, fmt.Errorf("%s: a repository with packs (%s) only names itself at the root; move its sections into a pack",
			filepath.Join(repo, ManifestFile), strings.Join(subs, ", "))
	}
	var out []Pack
	for _, sub := range subs {
		p, err := load(sub)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// packsFromDir treats a directory as the active pack set (for `-f`, and for
// running inside a checkout): a repository of packs yields one per pack, a
// single pack itself.
func packsFromDir(dir string) ([]Pack, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if fi, err := os.Stat(abs); err != nil {
		return nil, err
	} else if !fi.IsDir() {
		abs = filepath.Dir(abs)
	}
	src := source{Repo: abs, Name: filepath.Base(abs), Target: abs, Local: true}
	return packsIn(src, abs, "")
}

// resolveUses expands each pack's use: into the packs they name, layered
// before the declaring pack so that it wins on conflicts. Their checkouts are
// managed like `omasushi use` ones (and pulled by update), but they are not
// recorded in this machine's file: the declaring manifest is their source of
// truth.
//
// A pack already loaded — directly, or through another use: — keeps its
// first position, which also makes cycles harmless. One checkout serves
// every pack of a repository, so two of them pinned to different refs is
// an error rather than a silent last-one-wins.
func resolveUses(ps []Pack) ([]Pack, error) {
	at := map[string]bool{}
	pinned := map[string]Pack{} // by Repo
	var out []Pack
	var add func(ps []Pack, via string) error
	add = func(ps []Pack, via string) error {
		for _, p := range ps {
			if at[p.Name] {
				continue
			}
			p.Via = via
			at[p.Name] = true
			if q, ok := pinned[p.Repo]; ok && q.Ref != p.Ref {
				return fmt.Errorf("%s is pinned to %q by %s and %q by %s; one checkout, one ref", filepath.Base(p.Repo), q.Ref, q.Name, p.Ref, p.Name)
			}
			pinned[p.Repo] = p
			// This machine's file is the root of the stack, not a middleman:
			// what it uses is what the user asked for directly.
			mine := p.Name
			if mine == LocalName {
				mine = ""
			}
			for _, u := range p.Uses {
				deps, err := loadUse(u, p)
				if err != nil {
					return fmt.Errorf("%s: use %s: %w", p.Name, u, err)
				}
				if err := add(deps, mine); err != nil {
					return err
				}
			}
			out = append(out, p)
		}
		return nil
	}
	if err := add(ps, ""); err != nil {
		return nil, err
	}
	return out, nil
}

// loadUse materialises one use: entry of from. A relative path resolves
// against the pack's directory, so a sibling pack is ../fonts; one that
// stays inside the same repository keeps that repository's name, so the
// sibling is polidog/omakase/fonts and not a stranger called fonts.
func loadUse(entry string, from Pack) ([]Pack, error) {
	if strings.HasPrefix(entry, "./") || strings.HasPrefix(entry, "../") {
		abs := filepath.Join(from.Dir, entry)
		if rel, err := filepath.Rel(from.Repo, abs); err == nil && !strings.HasPrefix(rel, "..") && from.Machine == nil {
			src, err := parseSource(from.Source)
			if err != nil {
				return nil, err
			}
			src.Sub = filepath.ToSlash(rel)
			if src.Sub == "." {
				src.Sub = ""
			}
			src.Ref = from.Ref // a sibling shares the checkout, so the pin
			return packsIn(src, from.Repo, "")
		}
		entry = abs
	}
	src, err := parseSource(entry)
	if err != nil {
		return nil, err
	}
	repo, err := ensureCheckout(src)
	if err != nil {
		return nil, err
	}
	if !src.Local {
		if err := pin(repo, src.Ref, false); err != nil {
			return nil, err
		}
	}
	if !isPackRepo(repo) {
		return nil, fmt.Errorf("%s has no %s", repo, ManifestFile)
	}
	return packsIn(src, repo, "")
}

// pin puts a managed checkout where its use: entry says: detached at ref (a
// tag, branch or commit), or on the default branch when there is no ref and
// it was pinned before. refresh goes to the network first (update, use);
// otherwise it is only touched when ref is not on disk yet.
func pin(repo, ref string, refresh bool) error {
	git := func(args ...string) error { return runVisible("git", append([]string{"-C", repo}, args...)...) }
	out := func(args ...string) string { return run("git", append([]string{"-C", repo}, args...)...) }
	if ref == "" {
		if out("symbolic-ref", "-q", "HEAD") == "" {
			branch := strings.TrimPrefix(out("rev-parse", "--abbrev-ref", "origin/HEAD"), "origin/")
			if err := git("checkout", branch); err != nil {
				return err
			}
		}
		if !refresh {
			return nil
		}
		return git("pull", "--ff-only")
	}
	want := out("rev-parse", "-q", "--verify", ref+"^{commit}")
	if refresh || want == "" {
		if err := git("fetch", "--depth", "1", "origin", ref); err != nil {
			return err
		}
		want = out("rev-parse", "FETCH_HEAD")
	}
	if want == out("rev-parse", "HEAD") {
		return nil
	}
	return git("checkout", "--detach", want)
}

// ensureCheckout returns the checkout directory for src, cloning a remote
// source that is not on disk yet. It never pulls; `omasushi update` does.
func ensureCheckout(src source) (string, error) {
	if src.Local {
		return src.Target, nil
	}
	repo := filepath.Join(packsDir(), src.Name)
	if _, err := os.Stat(repo); err == nil {
		return repo, nil
	}
	if err := os.MkdirAll(packsDir(), 0o755); err != nil {
		return "", err
	}
	if err := runVisible("git", "clone", "--depth", "1", src.Target, repo); err != nil {
		return "", err
	}
	return repo, nil
}

// Add records a source under use:, cloning a remote one (or refreshing an
// existing checkout) first. What the user typed is what gets written —
// `owner/repo` records the repository, so packs added to it later come along
// on their own — and the packs it resolves to are returned for the caller to
// report.
func (l *Local) Add(input string) ([]Pack, error) {
	src, err := parseSource(input)
	if err != nil {
		return nil, err
	}
	fresh := false
	if !src.Local {
		_, err := os.Stat(filepath.Join(packsDir(), src.Name))
		fresh = err != nil
	}
	repo, err := ensureCheckout(src)
	if err != nil {
		return nil, err
	}
	if !src.Local {
		if err := pin(repo, src.Ref, !fresh); err != nil {
			return nil, err
		}
	}
	if !isPackRepo(repo) {
		return nil, fmt.Errorf("%s has no %s", repo, ManifestFile)
	}
	ps, err := packsIn(src, repo, "")
	if err != nil {
		return nil, err
	}
	l.use(src.String())
	return ps, l.Save()
}

// use appends a source to use:, or rewrites the entry already naming that
// pack — so `use x@v2` re-pins x@v1, and `use x` lets it float again.
func (l *Local) use(source string) {
	for i, u := range l.Use {
		if sameSource(u, source) {
			l.Use[i] = source
			return
		}
	}
	l.Use = append(l.Use, source)
}

// sameSource reports whether two use: entries name the same pack, so that
// polidog/omakase and https://github.com/polidog/omakase.git count as one.
func sameSource(a, b string) bool {
	sa, ea := parseSource(a)
	sb, eb := parseSource(b)
	if ea != nil || eb != nil {
		return a == b
	}
	return sa.Target == sb.Target && sa.Sub == sb.Sub
}

// Remove drops a pack from use:. A single pack of a repository recorded whole
// is dropped by replacing that entry with its siblings, so `remove
// owner/repo/herdr` keeps working on a repository added as `owner/repo`. The
// managed checkout goes once nothing points at that repository any more.
func (l *Local) Remove(name string) error {
	for i, u := range l.Use {
		src, err := parseSource(u)
		if err != nil || packName(src.Name, src.Sub) != name {
			continue
		}
		l.Use = append(l.Use[:i:i], l.Use[i+1:]...)
		l.dropCheckout(src)
		return l.Save()
	}
	for i, u := range l.Use {
		src, err := parseSource(u)
		if err != nil || src.Sub != "" || !strings.HasPrefix(name, src.Name+"/") {
			continue
		}
		sub := strings.TrimPrefix(name, src.Name+"/")
		subs := packDirs(checkoutDir(src))
		if !contains(subs, sub) {
			continue
		}
		if src.Local {
			return fmt.Errorf("%s is one pack of the local repository %s; remove the whole path, or point use: at the packs you want", name, u)
		}
		var kept []string
		for _, s := range subs {
			if s != sub {
				src.Sub = s
				kept = append(kept, src.String())
			}
		}
		l.Use = append(l.Use[:i:i], append(kept, l.Use[i+1:]...)...)
		return l.Save()
	}
	return fmt.Errorf("no pack named %q", name)
}

// dropCheckout deletes a managed clone once no use: entry still points at
// that repository.
func (l *Local) dropCheckout(src source) {
	if src.Local || l.usesRepo(src.Name) {
		return
	}
	dir := filepath.Join(packsDir(), src.Name)
	if strings.HasPrefix(dir, packsDir()) {
		os.RemoveAll(dir)
		os.Remove(filepath.Dir(dir)) // owner dir, only if now empty
	}
}

func (l *Local) usesRepo(repoName string) bool {
	for _, s := range l.Use {
		if src, err := parseSource(s); err == nil && !src.Local && src.Name == repoName {
			return true
		}
	}
	return false
}

// Update pulls every remote repository once; a pinned one is re-fetched at
// its ref instead (a branch pin moves, a tag pin stays). Local ones are left
// alone.
func Update(packs []Pack) error {
	done := map[string]bool{}
	for _, p := range packs {
		if done[p.Repo] {
			continue
		}
		done[p.Repo] = true
		if p.Local {
			fmt.Printf("==> %s: local, skipped\n", p.Name)
			continue
		}
		fmt.Printf("==> %s\n", repoLabel(packs, p.Repo))
		if err := pin(p.Repo, p.Ref, true); err != nil {
			return err
		}
	}
	return nil
}

// repoLabel names a checkout by the packs using it: "omakase (herdr, kitty)".
func repoLabel(packs []Pack, repo string) string {
	var subs []string
	base := filepath.Base(repo)
	for _, p := range packs {
		if p.Repo == repo && p.Sub != "" {
			subs = append(subs, p.Sub)
		}
	}
	if len(subs) == 0 {
		return base
	}
	sort.Strings(subs)
	return fmt.Sprintf("%s (%s)", base, strings.Join(subs, ", "))
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}
