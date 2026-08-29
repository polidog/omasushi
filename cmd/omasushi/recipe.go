package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// A Recipe is a directory (usually a git checkout) holding an omasushi.yaml
// plus the files, skills and commands it refers to. Several recipes can be in
// use at once; they are layered in config order, later ones winning.
type Recipe struct {
	Name     string
	Source   string // what the user typed: owner/repo, URL, or local path
	Dir      string // where the checkout lives
	Local    bool   // Dir is a user path, not managed by omasushi (never pulled)
	Manifest *Manifest
}

func (r Recipe) ManifestPath() string { return filepath.Join(r.Dir, ManifestFile) }

// Config is ~/.config/omasushi/config.yaml: the ordered list of recipes in use.
type Config struct {
	Recipes []RecipeRef `yaml:"recipes"`
}

type RecipeRef struct {
	Name   string `yaml:"name"`
	Source string `yaml:"source"`
}

func configPath() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "omasushi", "config.yaml")
	}
	return expandHome("~/.config/omasushi/config.yaml")
}

func recipesDir() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "omasushi", "recipes")
	}
	return expandHome("~/.local/share/omasushi/recipes")
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
		return "", "", false, fmt.Errorf("empty recipe source")
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
			return "", "", false, fmt.Errorf("recipe source must be owner/repo, a git URL, or a local path: %q", s)
		}
		url = "https://github.com/" + parts[0] + "/" + parts[1] + ".git"
	}
	name = strings.TrimSuffix(filepath.Base(normalizeGitURL(url)), ".git")
	return name, url, false, nil
}

// LoadRecipes materialises the configured recipes. Missing checkouts are an
// error (run `omasushi use` again); a missing manifest is an empty manifest.
func LoadRecipes(c *Config) ([]Recipe, error) {
	var out []Recipe
	for _, ref := range c.Recipes {
		_, target, local, err := resolveSource(ref.Source)
		if err != nil {
			return nil, err
		}
		r := Recipe{Name: ref.Name, Source: ref.Source, Local: local}
		if local {
			r.Dir = target
		} else {
			r.Dir = filepath.Join(recipesDir(), ref.Name)
		}
		if _, err := os.Stat(r.Dir); err != nil {
			return nil, fmt.Errorf("recipe %s: checkout missing at %s (run `omasushi use %s`)", ref.Name, r.Dir, ref.Source)
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

// recipeFromDir treats an arbitrary directory as the single active recipe
// (for `-f path/omasushi.yaml`, and for running inside a recipe checkout).
func recipeFromDir(manifestPath string) (Recipe, error) {
	abs, err := filepath.Abs(manifestPath)
	if err != nil {
		return Recipe{}, err
	}
	m, err := LoadManifest(abs)
	if err != nil {
		return Recipe{}, err
	}
	dir := filepath.Dir(abs)
	return Recipe{Name: filepath.Base(dir), Source: dir, Dir: dir, Local: true, Manifest: m}, nil
}

// Use adds a recipe: clones remote sources into recipesDir, records local
// paths as they are. Re-using an existing name refreshes the checkout.
func (c *Config) Use(source string) (Recipe, error) {
	name, target, local, err := resolveSource(source)
	if err != nil {
		return Recipe{}, err
	}
	r := Recipe{Name: name, Source: source, Local: local}
	if local {
		r.Dir = target
		if _, err := os.Stat(filepath.Join(r.Dir, ManifestFile)); err != nil {
			return Recipe{}, fmt.Errorf("%s has no %s", r.Dir, ManifestFile)
		}
	} else {
		r.Dir = filepath.Join(recipesDir(), name)
		if _, err := os.Stat(r.Dir); err == nil {
			if err := runVisible("git", "-C", r.Dir, "pull", "--ff-only"); err != nil {
				return Recipe{}, err
			}
		} else {
			if err := os.MkdirAll(recipesDir(), 0o755); err != nil {
				return Recipe{}, err
			}
			if err := runVisible("git", "clone", "--depth", "1", target, r.Dir); err != nil {
				return Recipe{}, err
			}
		}
	}
	m, err := LoadManifest(r.ManifestPath())
	if err != nil {
		return Recipe{}, err
	}
	r.Manifest = m
	for i, ref := range c.Recipes {
		if ref.Name == name {
			c.Recipes[i].Source = source
			return r, c.Save()
		}
	}
	c.Recipes = append(c.Recipes, RecipeRef{Name: name, Source: source})
	return r, c.Save()
}

func (c *Config) Remove(name string) error {
	for i, ref := range c.Recipes {
		if ref.Name != name {
			continue
		}
		c.Recipes = append(c.Recipes[:i], c.Recipes[i+1:]...)
		if _, _, local, _ := resolveSource(ref.Source); !local {
			dir := filepath.Join(recipesDir(), name)
			if strings.HasPrefix(dir, recipesDir()) {
				os.RemoveAll(dir)
			}
		}
		return c.Save()
	}
	return fmt.Errorf("no recipe named %q", name)
}

// Update pulls every remote recipe. Local recipes are left alone.
func Update(recipes []Recipe) error {
	for _, r := range recipes {
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
