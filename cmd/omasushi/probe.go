package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// State is what is actually installed on this machine.
type State struct {
	Aur            map[string]bool
	Pacman         map[string]bool
	Provides       map[string]bool                   // virtual names satisfied by an installed package
	OmarchyPlugins map[string]InstalledOmarchyPlugin // keyed by normalized origin URL
	HerdrPlugins   map[string]bool                   // keyed by owner/repo[/subdir]
	Defaults       Defaults
	Font           string
}

type InstalledOmarchyPlugin struct {
	ID      string
	URL     string
	Enabled bool
}

func Probe() (*State, error) {
	s := &State{
		Aur:            map[string]bool{},
		Pacman:         map[string]bool{},
		Provides:       map[string]bool{},
		OmarchyPlugins: map[string]InstalledOmarchyPlugin{},
		HerdrPlugins:   map[string]bool{},
	}
	for _, p := range lines(run("pacman", "-Qqm")) {
		s.Aur[p] = true
	}
	for _, p := range lines(run("pacman", "-Qqe")) {
		if !s.Aur[p] {
			s.Pacman[p] = true
		}
	}
	s.probeProvides()
	s.probeDefaults()
	if err := s.probeOmarchy(); err != nil {
		return nil, err
	}
	if err := s.probeHerdr(); err != nil {
		return nil, err
	}
	return s, nil
}

// probeProvides records the names installed packages provide, so a manifest
// entry already covered by another package (slack-desktop-wayland provides
// slack-desktop) is not reinstalled into a conflict.
func (s *State) probeProvides() {
	dbPath := strings.TrimSpace(run("pacman-conf", "DBPath"))
	if dbPath == "" {
		dbPath = "/var/lib/pacman"
	}
	descs, err := filepath.Glob(filepath.Join(dbPath, "local", "*", "desc"))
	if err != nil {
		return
	}
	for _, d := range descs {
		b, err := os.ReadFile(d)
		if err != nil {
			continue
		}
		inProvides := false
		for _, l := range strings.Split(string(b), "\n") {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "%") {
				inProvides = l == "%PROVIDES%"
				continue
			}
			if !inProvides || l == "" {
				continue
			}
			if i := strings.IndexAny(l, "=<>"); i >= 0 {
				l = l[:i] // drop the version constraint
			}
			s.Provides[l] = true
		}
	}
}

func (s *State) probeDefaults() {
	if _, err := exec.LookPath("omarchy-default-agent"); err != nil {
		return
	}
	s.Font = run("omarchy-font-current")
	s.Defaults = Defaults{
		Agent:    run("omarchy-default-agent"),
		Browser:  run("omarchy-default-browser"),
		Editor:   run("omarchy-default-editor"),
		Terminal: run("omarchy-default-terminal"),
	}
}

func (s *State) probeOmarchy() error {
	if _, err := exec.LookPath("omarchy-plugin-list"); err != nil {
		return nil
	}
	out, err := exec.Command("omarchy-plugin-list", "--json").Output()
	if err != nil {
		return fmt.Errorf("omarchy-plugin-list: %w", err)
	}
	var list []struct {
		ID         string `json:"id"`
		Enabled    bool   `json:"enabled"`
		FirstParty bool   `json:"firstParty"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return fmt.Errorf("omarchy-plugin-list: %w", err)
	}
	dir := filepath.Join(expandHome("~"), ".config/omarchy/plugins")
	for _, p := range list {
		if p.FirstParty {
			continue
		}
		url := strings.TrimSpace(run("git", "-C", filepath.Join(dir, p.ID), "remote", "get-url", "origin"))
		if url == "" {
			continue
		}
		s.OmarchyPlugins[normalizeGitURL(url)] = InstalledOmarchyPlugin{ID: p.ID, URL: url, Enabled: p.Enabled}
	}
	return nil
}

func (s *State) probeHerdr() error {
	if _, err := exec.LookPath("herdr"); err != nil {
		return nil
	}
	out, err := exec.Command("herdr", "plugin", "list", "--json").Output()
	if err != nil {
		return fmt.Errorf("herdr plugin list: %w", err)
	}
	var resp struct {
		Result struct {
			Plugins []struct {
				Source struct {
					Kind   string `json:"kind"`
					Owner  string `json:"owner"`
					Repo   string `json:"repo"`
					Subdir string `json:"subdir"`
				} `json:"source"`
			} `json:"plugins"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return fmt.Errorf("herdr plugin list: %w", err)
	}
	for _, p := range resp.Result.Plugins {
		if p.Source.Kind != "github" {
			continue
		}
		src := p.Source.Owner + "/" + p.Source.Repo
		if p.Source.Subdir != "" {
			src += "/" + p.Source.Subdir
		}
		s.HerdrPlugins[src] = true
	}
	return nil
}

func run(name string, args ...string) string {
	out, _ := exec.Command(name, args...).Output()
	return string(bytes.TrimSpace(out))
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// runVisible executes a command with stdio attached, for apply.
func runVisible(name string, args ...string) error {
	fmt.Printf("$ %s %s\n", name, strings.Join(args, " "))
	c := exec.Command(name, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}
