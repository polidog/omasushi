package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Status is a snapshot of "where am I": the omakases in use and their git
// state, what this machine currently has, and how far it is from the
// omakases. diff answers "what would sync do"; status answers "how am I
// doing overall".
type Status struct {
	Host     string          `json:"host"`
	Config   string          `json:"config"`
	Omakases []OmakaseStatus `json:"omakases"`
	Machine  MachineStatus   `json:"machine"`
	Sync     SyncStatus      `json:"sync"`
}

type OmakaseStatus struct {
	Name     string `json:"name"`
	Source   string `json:"source"`
	Dir      string `json:"dir"`
	Local    bool   `json:"local"`
	HasHost  bool   `json:"hasHostOverlay"` // declares hosts.<host>
	Branch   string `json:"branch,omitempty"`
	Commit   string `json:"commit,omitempty"`
	Modified int    `json:"modified"` // uncommitted changes (git status --porcelain)
	Ahead    int    `json:"ahead"`    // commits not pushed
	Behind   int    `json:"behind"`   // commits not pulled (as of last fetch)
}

type MachineStatus struct {
	Font           string   `json:"font"`
	Defaults       Defaults `json:"defaults"`
	AurPackages    int      `json:"aurPackages"`
	OmarchyPlugins int      `json:"omarchyPlugins"`
	HerdrPlugins   int      `json:"herdrPlugins"`
}

type SyncStatus struct {
	Pending     int            `json:"pending"`     // actions sync would run
	PendingKind map[string]int `json:"pendingKind"` // by Action.Kind
	Extras      int            `json:"extras"`      // installed but unrecorded
	Links       int            `json:"links"`       // files/skills/commands managed
	Linked      int            `json:"linked"`      // ...of which are already in place
}

func gatherStatus(omakases []Omakase, host string, have *State) Status {
	st := Status{Host: host, Config: configPath(), Omakases: []OmakaseStatus{}}
	for _, r := range omakases {
		o := OmakaseStatus{Name: r.Name, Source: r.Source, Dir: r.Dir, Local: r.Local}
		if r.Manifest != nil {
			_, o.HasHost = r.Manifest.Hosts[host]
		}
		if isGitRepo(r.Repo) {
			o.Branch = run("git", "-C", r.Repo, "rev-parse", "--abbrev-ref", "HEAD")
			o.Commit = run("git", "-C", r.Repo, "rev-parse", "--short", "HEAD")
			o.Modified = len(lines(run("git", "-C", r.Repo, "status", "--porcelain")))
			if ab := strings.Fields(run("git", "-C", r.Repo, "rev-list", "--left-right", "--count", "HEAD...@{u}")); len(ab) == 2 {
				o.Ahead, _ = strconv.Atoi(ab[0])
				o.Behind, _ = strconv.Atoi(ab[1])
			}
		}
		st.Omakases = append(st.Omakases, o)
	}

	st.Machine = MachineStatus{
		Font:           have.Font,
		Defaults:       have.Defaults,
		AurPackages:    len(have.Aur),
		OmarchyPlugins: len(have.OmarchyPlugins),
		HerdrPlugins:   len(have.HerdrPlugins),
	}

	actions, extras := Plan(omakases, host, have)
	st.Sync = SyncStatus{Pending: len(actions), PendingKind: map[string]int{}, Extras: len(extras)}
	for _, a := range actions {
		st.Sync.PendingKind[a.Kind]++
	}
	agent := resolveAgent(omakases, host)
	for _, r := range omakases {
		for _, l := range omakaseLinks(r, r.Manifest.Resolve(host), agent) {
			st.Sync.Links++
			if cur, err := os.Readlink(l.dst); err == nil && cur == l.src {
				st.Sync.Linked++
			}
		}
	}
	return st
}

func isGitRepo(dir string) bool {
	if !gitAvailable() {
		return false
	}
	return exec.Command("git", "-C", dir, "rev-parse", "--git-dir").Run() == nil
}

func printStatus(st Status) {
	fmt.Printf("host      %s\n", st.Host)
	fmt.Printf("config    %s\n", tildify(st.Config))

	fmt.Println("\nomakases")
	if len(st.Omakases) == 0 {
		fmt.Println("  none in use (try: omasushi use owner/repo)")
	}
	for _, o := range st.Omakases {
		kind := "git"
		if o.Local {
			kind = "local"
		}
		rev := "-"
		if o.Commit != "" {
			rev = o.Branch + "@" + o.Commit
		}
		var notes []string
		if o.Modified > 0 {
			notes = append(notes, fmt.Sprintf("%d modified", o.Modified))
		}
		if o.Ahead > 0 {
			notes = append(notes, fmt.Sprintf("%d to push", o.Ahead))
		}
		if o.Behind > 0 {
			notes = append(notes, fmt.Sprintf("%d behind", o.Behind))
		}
		if o.HasHost {
			notes = append(notes, "host overlay")
		}
		note := "clean"
		if o.Commit == "" {
			note = ""
		}
		if len(notes) > 0 {
			note = strings.Join(notes, ", ")
		}
		fmt.Printf("  %-22s %-6s %-20s %-24s %s\n", o.Name, kind, rev, note, tildify(o.Dir))
	}

	m := st.Machine
	fmt.Println("\nmachine")
	fmt.Printf("  font       %s\n", orDash(m.Font))
	fmt.Printf("  defaults   agent=%s browser=%s editor=%s terminal=%s\n",
		orDash(m.Defaults.Agent), orDash(m.Defaults.Browser), orDash(m.Defaults.Editor), orDash(m.Defaults.Terminal))
	fmt.Printf("  installed  %d aur, %d omarchy plugins, %d herdr plugins\n", m.AurPackages, m.OmarchyPlugins, m.HerdrPlugins)

	s := st.Sync
	fmt.Println("\nsync")
	if s.Pending == 0 {
		fmt.Println("  pending    nothing — up to date")
	} else {
		kinds := make([]string, 0, len(s.PendingKind))
		for k := range s.PendingKind {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		var parts []string
		for _, k := range kinds {
			parts = append(parts, fmt.Sprintf("%s %d", k, s.PendingKind[k]))
		}
		fmt.Printf("  pending    %d actions (%s)   -> omasushi sync\n", s.Pending, strings.Join(parts, ", "))
	}
	fmt.Printf("  links      %d/%d in place\n", s.Linked, s.Links)
	if s.Extras == 0 {
		fmt.Println("  unrecorded nothing")
	} else {
		fmt.Printf("  unrecorded %d installed but not in any omakase   -> omasushi export\n", s.Extras)
	}
	if len(st.Omakases) > 0 {
		var hints []string
		for _, o := range st.Omakases {
			if o.Behind > 0 {
				hints = append(hints, "omasushi update")
				break
			}
		}
		for _, o := range st.Omakases {
			if o.Modified > 0 || o.Ahead > 0 {
				hints = append(hints, "commit & push "+o.Name)
			}
		}
		if len(hints) > 0 {
			fmt.Printf("  next       %s\n", strings.Join(hints, "; "))
		}
	}
}

func printStatusJSON(st Status) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	enc.Encode(st)
}

func tildify(p string) string {
	home := expandHome("~")
	if home != "" && strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
