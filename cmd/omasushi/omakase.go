package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// A Omakase is a directory (usually a git checkout) holding an omasushi.yaml
// plus the files, skills and commands it refers to. Several omakases can be in
// use at once; they are layered in config order, later ones winning.
type Omakase struct {
	Name     string
	Source   string // what the user typed: owner/repo, URL, or local path
	Dir      string // where the checkout lives
	Local    bool   // Dir is a user path, not managed by omasushi (never pulled)
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

// resolveSource turns user input into (name, git URL or local dir, isLocal).
//
//	owner/repo            -> https://github.com/owner/repo.git
//	https://... / git@... -> as is
//	./dir, ../dir, ~/dir, /abs, or an existing directory -> local
func resolveSource(s string) (name, target string, local bool, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", false, fmt.Errorf("empty omakase source")
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
			return "", "", false, err
		}
		return filepath.Base(abs), abs, true, nil
	}
	url := s
	if !strings.Contains(s, "://") && !strings.HasPrefix(s, "git@") {
		parts := strings.Split(strings.Trim(s, "/"), "/")
		if len(parts) != 2 {
			return "", "", false, fmt.Errorf("omakase source must be owner/repo, a git URL, or a local path: %q", s)
		}
		url = "https://github.com/" + parts[0] + "/" + parts[1] + ".git"
	}
	name = strings.TrimSuffix(filepath.Base(normalizeGitURL(url)), ".git")
	return name, url, false, nil
}

// LoadOmakases materialises the configured omakases. Missing checkouts are an
// error (run `omasushi use` again); a missing manifest is an empty manifest.
func LoadOmakases(c *Config) ([]Omakase, error) {
	var out []Omakase
	for _, ref := range c.Omakases {
		_, target, local, err := resolveSource(ref.Source)
		if err != nil {
			return nil, err
		}
		r := Omakase{Name: ref.Name, Source: ref.Source, Local: local}
		if local {
			r.Dir = target
		} else {
			r.Dir = filepath.Join(omakasesDir(), ref.Name)
		}
		if _, err := os.Stat(r.Dir); err != nil {
			return nil, fmt.Errorf("omakase %s: checkout missing at %s (run `omasushi use %s`)", ref.Name, r.Dir, ref.Source)
		}
		m, err := LoadManifest(r.ManifestPath())
		if err != nil {
			return nil, err
		}
		r.Manifest = m
		out = append(out, r)
	}
	return out, nil
}

// omakaseFromDir treats an arbitrary directory as the single active omakase
// (for `-f path/omasushi.yaml`, and for running inside an omakase checkout).
func omakaseFromDir(manifestPath string) (Omakase, error) {
	abs, err := filepath.Abs(manifestPath)
	if err != nil {
		return Omakase{}, err
	}
	m, err := LoadManifest(abs)
	if err != nil {
		return Omakase{}, err
	}
	dir := filepath.Dir(abs)
	return Omakase{Name: filepath.Base(dir), Source: dir, Dir: dir, Local: true, Manifest: m}, nil
}

// Use adds an omakase: clones remote sources into omakasesDir, records local
// paths as they are. Re-using an existing name refreshes the checkout.
func (c *Config) Use(source string) (Omakase, error) {
	name, target, local, err := resolveSource(source)
	if err != nil {
		return Omakase{}, err
	}
	r := Omakase{Name: name, Source: source, Local: local}
	if local {
		r.Dir = target
		if _, err := os.Stat(filepath.Join(r.Dir, ManifestFile)); err != nil {
			return Omakase{}, fmt.Errorf("%s has no %s", r.Dir, ManifestFile)
		}
	} else {
		r.Dir = filepath.Join(omakasesDir(), name)
		if _, err := os.Stat(r.Dir); err == nil {
			if err := runVisible("git", "-C", r.Dir, "pull", "--ff-only"); err != nil {
				return Omakase{}, err
			}
		} else {
			if err := os.MkdirAll(omakasesDir(), 0o755); err != nil {
				return Omakase{}, err
			}
			if err := runVisible("git", "clone", "--depth", "1", target, r.Dir); err != nil {
				return Omakase{}, err
			}
		}
	}
	m, err := LoadManifest(r.ManifestPath())
	if err != nil {
		return Omakase{}, err
	}
	r.Manifest = m
	for i, ref := range c.Omakases {
		if ref.Name == name {
			c.Omakases[i].Source = source
			return r, c.Save()
		}
	}
	c.Omakases = append(c.Omakases, OmakaseRef{Name: name, Source: source})
	return r, c.Save()
}

func (c *Config) Remove(name string) error {
	for i, ref := range c.Omakases {
		if ref.Name != name {
			continue
		}
		c.Omakases = append(c.Omakases[:i], c.Omakases[i+1:]...)
		if _, _, local, _ := resolveSource(ref.Source); !local {
			dir := filepath.Join(omakasesDir(), name)
			if strings.HasPrefix(dir, omakasesDir()) {
				os.RemoveAll(dir)
			}
		}
		return c.Save()
	}
	return fmt.Errorf("no omakase named %q", name)
}

// Update pulls every remote omakase. Local omakases are left alone.
func Update(omakases []Omakase) error {
	for _, r := range omakases {
		if r.Local {
			fmt.Printf("==> %s: local, skipped\n", r.Name)
			continue
		}
		fmt.Printf("==> %s\n", r.Name)
		if err := runVisible("git", "-C", r.Dir, "pull", "--ff-only"); err != nil {
			return err
		}
	}
	return nil
}

func gitAvailable() bool {
	_, err := exec.LookPath("git")
	return err == nil
}
