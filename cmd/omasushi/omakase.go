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

// A Omakase is a directory (usually a git checkout, or a part of one) holding
// an omasushi.yaml plus the files, skills and commands it refers to. Several
// omakases can be in use at once; they are layered in config order, later
// ones winning.
type Omakase struct {
	Name     string // repo, or repo/part
	Source   string // what the user typed for the repository: owner/repo, URL, or local path
	Part     string // sub-directory inside the repository ("" = the root manifest)
	Repo     string // the checkout (git root); Update pulls here
	Dir      string // Repo/Part: where omasushi.yaml and its files live
	Local    bool   // Repo is a user path, not managed by omasushi (never pulled)
	Manifest *Manifest
}

func (r Omakase) ManifestPath() string { return filepath.Join(r.Dir, ManifestFile) }

// Config is ~/.config/omasushi/config.yaml: the ordered list of omakases in use.
type Config struct {
	Omakases []OmakaseRef `yaml:"omakases"`
}

type OmakaseRef struct {
	Name   string `yaml:"name"`
	Source string `yaml:"source"`
	Part   string `yaml:"part,omitempty"`
}

func configPath() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "omasushi", "config.yaml")
	}
	return expandHome("~/.config/omasushi/config.yaml")
}

func omakasesDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "omasushi", "omakases")
	}
	return expandHome("~/.local/share/omasushi/omakases")
}

func LoadConfig() (*Config, error) {
	b, err := os.ReadFile(configPath())
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", configPath(), err)
	}
	return &c, nil
}

func (c *Config) Save() error {
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// source is a parsed omakase source: the repository plus an optional part.
type source struct {
	Repo   string // what to record: owner/repo, URL, or local path (part stripped)
	Name   string // repository name (checkout directory name)
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
	src.Name = strings.TrimSuffix(filepath.Base(normalizeGitURL(src.Target)), ".git")
	return src, nil
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

// LoadOmakases materialises the configured omakases. Missing checkouts are an
// error (run `omasushi use` again); a missing manifest is an empty manifest.
// A root entry whose repository has been split into parts (recorded before
// the split) expands to all of its parts.
func LoadOmakases(c *Config) ([]Omakase, error) {
	var out []Omakase
	for _, ref := range c.Omakases {
		src, err := parseSource(ref.Source)
		if err != nil {
			return nil, err
		}
		if ref.Part != "" {
			src.Part = ref.Part
		}
		repo := src.Target
		if !src.Local {
			repo = filepath.Join(omakasesDir(), src.Name)
		}
		if _, err := os.Stat(repo); err != nil {
			return nil, fmt.Errorf("omakase %s: checkout missing at %s (run `omasushi use %s`)", ref.Name, repo, ref.Source)
		}
		rs, err := omakasesIn(src, repo, ref.Name)
		if err != nil {
			return nil, err
		}
		out = append(out, rs...)
	}
	return out, nil
}

// omakasesIn loads src.Part of the checkout at repo; for the root of a
// repository that declares parts it loads every part instead.
func omakasesIn(src source, repo, name string) ([]Omakase, error) {
	load := func(part string) (Omakase, error) {
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
		r.Manifest = m
		return r, nil
	}
	root, err := load(src.Part)
	if err != nil {
		return nil, err
	}
	if src.Part != "" || len(root.Manifest.Parts) == 0 {
		return []Omakase{root}, nil
	}
	var out []Omakase
	for _, p := range root.Manifest.Parts {
		if err := checkPart(p); err != nil {
			return nil, fmt.Errorf("%s: %w", root.ManifestPath(), err)
		}
		r, err := load(p)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
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

// Use adds an omakase: clones remote sources into omakasesDir (one checkout
// per repository, shared by its parts), records local paths as they are.
// `owner/repo` on a repository split into parts adds every part; re-using an
// existing name refreshes the checkout.
func (c *Config) Use(input string) ([]Omakase, error) {
	src, err := parseSource(input)
	if err != nil {
		return nil, err
	}
	repo := src.Target
	if !src.Local {
		repo = filepath.Join(omakasesDir(), src.Name)
		if _, err := os.Stat(repo); err == nil {
			if err := runVisible("git", "-C", repo, "pull", "--ff-only"); err != nil {
				return nil, err
			}
		} else {
			if err := os.MkdirAll(omakasesDir(), 0o755); err != nil {
				return nil, err
			}
			if err := runVisible("git", "clone", "--depth", "1", src.Target, repo); err != nil {
				return nil, err
			}
		}
	}
	if _, err := os.Stat(filepath.Join(repo, src.Part, ManifestFile)); err != nil {
		return nil, fmt.Errorf("%s has no %s", filepath.Join(repo, src.Part), ManifestFile)
	}
	rs, err := omakasesIn(src, repo, "")
	if err != nil {
		return nil, err
	}
	for _, r := range rs {
		c.add(OmakaseRef{Name: r.Name, Source: src.Repo, Part: r.Part})
	}
	return rs, c.Save()
}

func (c *Config) add(ref OmakaseRef) {
	for i, have := range c.Omakases {
		if have.Name == ref.Name {
			c.Omakases[i] = ref
			return
		}
	}
	c.Omakases = append(c.Omakases, ref)
}

// Remove forgets an omakase by name (repo or repo/part). The managed checkout
// is deleted once no other part of the same repository is in use.
func (c *Config) Remove(name string) error {
	var removed *OmakaseRef
	kept := c.Omakases[:0:0]
	for _, ref := range c.Omakases {
		if ref.Name == name && removed == nil {
			r := ref
			removed = &r
			continue
		}
		kept = append(kept, ref)
	}
	if removed == nil {
		return fmt.Errorf("no omakase named %q", name)
	}
	c.Omakases = kept
	src, err := parseSource(removed.Source)
	if err == nil && !src.Local && !c.usesRepo(src.Name) {
		dir := filepath.Join(omakasesDir(), src.Name)
		if strings.HasPrefix(dir, omakasesDir()) {
			os.RemoveAll(dir)
		}
	}
	return c.Save()
}

func (c *Config) usesRepo(repoName string) bool {
	for _, ref := range c.Omakases {
		if src, err := parseSource(ref.Source); err == nil && !src.Local && src.Name == repoName {
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
