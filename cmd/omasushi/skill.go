package main

import (
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/polidog/omasushi/claude"
)

// skillCmd installs the skills bundled in the binary (claude/skills of this
// repository) into an agent's global skills directory, or removes them. No
// omakase is involved: this is for someone who just wants the agent to know
// omasushi. The files are copied (not linked) so they survive `go install`
// replacing the binary.
//
//	omasushi skill install [--agent codex]
//	omasushi skill remove  [--agent codex]
//	omasushi skill list
func skillCmd(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: omasushi skill install|remove|list [--agent name]")
	}
	fs_ := flag.NewFlagSet("skill", flag.ExitOnError)
	agent := fs_.String("agent", "", "agent to install for (default: the Omarchy default agent)")
	fs_.Parse(args[1:])
	if *agent == "" {
		*agent = resolveAgent(nil, "")
	}
	d, ok := agentDirs[*agent]
	if !ok {
		return fmt.Errorf("no known skills directory for agent %q (known: %s)", *agent, strings.Join(knownAgents(), ", "))
	}
	names, err := bundledSkills()
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		for _, n := range names {
			dst := expandHome(d.skills + "/" + n)
			state := "not installed"
			if _, err := os.Stat(filepath.Join(dst, "SKILL.md")); err == nil {
				state = "installed at " + tildify(dst)
			}
			fmt.Printf("%-12s %s (%s)\n", n, state, *agent)
		}
		return nil
	case "install":
		for _, n := range names {
			dst := expandHome(d.skills + "/" + n)
			if fi, err := os.Lstat(dst); err == nil && fi.Mode()&os.ModeSymlink != 0 {
				fmt.Printf("%s: %s is a symlink (managed by an omakase); left as is\n", n, tildify(dst))
				continue
			}
			if err := copySkill(n, dst); err != nil {
				return err
			}
			fmt.Printf("installed %s -> %s\n", n, tildify(dst))
		}
		return nil
	case "remove":
		for _, n := range names {
			dst := expandHome(d.skills + "/" + n)
			if _, err := os.Lstat(dst); err != nil {
				continue
			}
			if err := os.RemoveAll(dst); err != nil {
				return err
			}
			fmt.Printf("removed %s\n", tildify(dst))
		}
		return nil
	}
	return fmt.Errorf("unknown skill command %q", args[0])
}

func knownAgents() []string {
	var out []string
	for a := range agentDirs {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

func bundledSkills() ([]string, error) {
	entries, err := fs.ReadDir(claude.Skills, "skills")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// copySkill writes skills/<name> from the bundle to dst, replacing whatever
// is there.
func copySkill(name, dst string) error {
	root := "skills/" + name
	return fs.WalkDir(claude.Skills, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(p, root)
		out := filepath.Join(dst, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		b, err := claude.Skills.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
}
