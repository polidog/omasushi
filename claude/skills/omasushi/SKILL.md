---
name: omasushi
description: Sync an Omarchy machine from shared "omakase" repositories with the `omasushi` CLI — AUR packages, Omarchy plugins/defaults/font, Herdr plugins, dotfiles, and Claude Code skills/commands. Use when the user wants to sync their setup, record what this machine has, set up a new machine, share their config with others, or edit omasushi.yaml.
---

# omasushi

`omasushi` diffs one or more **omakases** (git repos with an `omasushi.yaml`) against the
real machine and calls the existing Omarchy / Herdr CLIs to close the gap.
It is a thin wrapper: no reimplemented yay or git clone. **It never removes anything.**

## Commands

```sh
omasushi use owner/repo          # add an omakase (GitHub shorthand, URL, or local path); all parts of a split repo
omasushi use owner/repo/herdr    # one part of a split repo (name shows as repo/herdr; remove it by that name)
omasushi list                    # omakases in use
omasushi update                  # git pull remote omakases
omasushi remove <name>

omasushi status [--json]         # overview: omakases (git branch/commit, modified/behind), machine setup, pending & unrecorded counts
omasushi plan [--json]           # diff. `?` lines are installed-but-unrecorded extras
omasushi apply                   # install what is missing, symlink files/skills/commands
omasushi export [--to <omakase>] [--host <name>]   # record this machine into an omakase (add-only)
omasushi init [dir]              # scaffold a new omakase repo
omasushi publish [name|owner/repo|url|path] [--open|--browser|--dry-run] [--web URL]
                                 # register on omasushi-web: resolves the repo URL (origin of ./omasushi.yaml's
                                 # checkout, or the omakase in use), warns if unpushed, POSTs it to <web>/api/omakase
                                 # (no login; the site reads omasushi.yaml from the public repo). Prints the plate URL

omasushi -f path/omasushi.yaml plan   # single-manifest mode (developing an omakase)
omasushi -H <hostname> plan            # resolve as another host
```

Omakases are cloned to `~/.local/share/omasushi/omakases/<repo>` (one checkout per repository,
shared by its parts); the list lives in `~/.config/omasushi/config.yaml` as `{name, source, part}`.
With no omakase configured, `./omasushi.yaml` is used.

**Parts**: a repository can be split into feature-sized directories (`herdr/`, `kitty/`, `claude/`…)
that each carry their own `omasushi.yaml` with paths relative to that directory. The root
`omasushi.yaml` then only has `name`, `description` and `parts: [herdr, kitty, claude]`.
Users mix parts from different repositories; suggest this layout when someone wants to share
one feature's config rather than a whole machine.

## Typical workflows

1. **Installed something on machine A** → `omasushi plan` shows `?` → `omasushi export` → commit & push the omakase
2. **Bring machine B up** → `omasushi update` → `omasushi plan` → `omasushi apply`
3. **Fresh machine** → `go install github.com/polidog/omasushi/cmd/omasushi@latest` → `omasushi use owner/repo` → `omasushi apply`
4. **Publish your setup** → `omasushi init my-omakase` → copy dotfiles under `files/`, skills under `skills/` → `omasushi -f my-omakase/omasushi.yaml export` → push → `omasushi publish` (registers on omasushi-web through its API; rate-limited to 10/hour per IP, and it fails with "not found" until `omasushi.yaml` is on the public repo's main/master)

When the user says "sync", **show `plan` first, then run `apply`** — apply runs yay and
git clone, so do not run it without the user seeing the plan.

## omasushi.yaml

```yaml
name: my-setup
description: short blurb shown by tools
packages:
  pacman: [pkg]                  # official repos; written by hand
  aur: [pkg]                     # filled by export (pacman -Qqm)
omarchy:
  font: "UDEV Gothic NF"         # `omarchy font set`; install the font package first
  defaults:                      # `omarchy default <kind>`
    agent: claude                # pi|omp|opencode|claude|codex|grok|gemini|copilot|crush
    browser: chrome              # chromium|chrome|brave|brave-origin|edge|firefox|zen
    editor: nvim                 # code|cursor|zed|sublime_text|helix|vim|emacs|nvim
    terminal: kitty              # foot|ghostty|kitty
  plugins:
    - url: https://github.com/owner/repo.git   # matched by git origin
      enable: true
herdr:
  plugins:
    - source: owner/repo[/subdir]
      ref: optional
claude:
  skills: skills                 # each subdir  -> ~/.claude/skills/<name>   (symlink)
  commands: commands             # each *.md    -> ~/.claude/commands/<name>.md
files:
  files/kitty/kitty.conf: ~/.config/kitty/kitty.conf   # symlink; existing file moved to .bak
hosts:
  <hostname>:                    # overlay merged onto the base (lists unioned, scalars win)
    packages: {...}
    files: {...}
parts: [herdr, kitty]            # root of a split repo only: each is a directory with its own omasushi.yaml
```

Several omakases stack in `use` order; a later omakase wins for the same key/destination.

## Editing rules

- Never put secrets (tokens, `calendar-sync.json`, …) under `files:` — omakases are meant to be public
- To share a file: copy it into the omakase's `files/`, then add the mapping; `apply` swaps the original for a symlink
- Skills/commands are linked **per entry**, so the machine's own `~/.claude/skills` stay untouched
- `omarchy font set` / `omarchy theme set` rewrite terminal configs with `sed -i`, which turns the symlink back into a real file. Copy the new file into the omakase and `apply` again (plan shows the `file-link` again)
- `font` / `defaults` empty means "don't care". `export` only fills them when the base is empty
- Machine-specific things (GPU drivers, monitor layouts) belong under `hosts.<hostname>`
- First-party `omarchy.*` plugins are ignored by probe/export

## Source layout (github.com/polidog/omasushi)

- `cmd/omasushi/manifest.go` — YAML types, `Resolve(host)`, `Overlay.merge`
- `cmd/omasushi/omakase.go` — config, `use`/`remove`/`update`, source resolution (`parseSource`: owner/repo[/part]), parts expansion (`omakasesIn`)
- `cmd/omasushi/probe.go` — read the real machine (`State`). Add a `probeXxx` here for a new target
- `cmd/omasushi/plan.go` — diff → `Action{Kind, Desc, Run}`; `omakaseLinks` expands files/skills/commands
- `cmd/omasushi/main.go` — CLI, `export`, `init`
- `cmd/omasushi/publish.go` — `publish`: repo URL resolution/canonicalisation and the `POST /api/omakase` call to omasushi-web (`webURL` default, `$OMASUSHI_WEB_URL`, `--web`)
- `manifest.json`, `Panel.qml`, `Model.js` (repo root) — Omarchy bar widget (`omarchy plugin add https://github.com/polidog/omasushi.git`) that shows pending actions and runs apply in a floating terminal
