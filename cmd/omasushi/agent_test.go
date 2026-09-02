package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// agent: links go wherever the resolved default agent keeps its skills;
// claude: always goes to ~/.claude.
func TestAgentLinks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	for _, p := range []string{"skills/one/SKILL.md", "commands/go.md"} {
		os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(dir, p), []byte("x"), 0o644)
	}
	r := Omakase{Name: "t", Dir: dir}
	o := Overlay{Agent: Claude{Skills: "skills", Commands: "commands"}, Claude: Claude{Skills: "skills"}}

	dsts := func(agent string) []string {
		var out []string
		for _, l := range omakaseLinks(r, o, agent) {
			out = append(out, strings.TrimPrefix(l.dst, home))
		}
		return out
	}
	want := map[string][]string{
		"claude":  {"/.claude/skills/one", "/.claude/skills/one", "/.claude/commands/go.md"},
		"codex":   {"/.claude/skills/one", "/.codex/skills/one", "/.codex/prompts/go.md"},
		"gemini":  {"/.claude/skills/one", "/.gemini/skills/one"}, // no commands dir
		"unknown": {"/.claude/skills/one"},
	}
	for agent, w := range want {
		if got := dsts(agent); strings.Join(got, ",") != strings.Join(w, ",") {
			t.Errorf("%s: got %v, want %v", agent, got, w)
		}
	}
}

// A filtered use: picks single skills and commands out of a shared directory:
// the omakase is linked, but only for the entries only: names.
func TestAgentLinksHonourOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := t.TempDir()
	for _, p := range []string{"skills/one/SKILL.md", "skills/two/SKILL.md", "commands/go.md", "commands/stop.md"} {
		os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(dir, p), []byte("x"), 0o644)
	}
	r := Omakase{Name: "t", Dir: dir, Only: Selection{"agent.skills": {"one"}, "agent.commands": {"go"}}}
	o := Overlay{Agent: Claude{Skills: "skills", Commands: "commands"}}.filter(r.Only)

	var got []string
	for _, l := range omakaseLinks(r, o, "claude") {
		got = append(got, strings.TrimPrefix(l.dst, home))
	}
	want := []string{"/.claude/skills/one", "/.claude/commands/go.md"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestResolveAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	m := &Manifest{}
	if a := resolveAgent([]Omakase{{Manifest: m}}, ""); a != "claude" {
		t.Errorf("fallback: got %q", a)
	}
	os.MkdirAll(filepath.Join(home, ".config/omarchy/defaults"), 0o755)
	os.WriteFile(filepath.Join(home, ".config/omarchy/defaults/agent"), []byte("codex\n"), 0o644)
	if a := resolveAgent([]Omakase{{Manifest: m}}, ""); a != "codex" {
		t.Errorf("machine default: got %q", a)
	}
	m2 := &Manifest{Omarchy: Omarchy{Defaults: Defaults{Agent: "gemini"}}}
	if a := resolveAgent([]Omakase{{Manifest: m}, {Manifest: m2}}, ""); a != "gemini" {
		t.Errorf("manifest wins: got %q", a)
	}
}
