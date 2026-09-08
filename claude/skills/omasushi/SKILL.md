---
name: omasushi
description: Add features to an Omarchy machine as packs with the `omasushi` CLI — one pack is everything one feature needs (AUR/pacman packages, Omarchy plugins/defaults/font, Herdr plugins, a hyprland snippet, dotfiles, AI agent skills/commands for Claude Code, Codex, Gemini CLI, ...). Use when the user wants to sync their setup, take someone's pack, record what this machine has into a pack, set up a new machine, share a feature's setup, keep something on this machine only, or edit omasushi.yaml.
---

# omasushi

`omasushi` diffs the **packs** in use (directories with an `omasushi.yaml`, usually in git
repositories, plus this machine's own file) against the real machine and calls the existing
Omarchy / Herdr CLIs to close the gap. It is a thin wrapper: no reimplemented yay or git clone.
**It never removes anything.**

Where it stops: dotfiles alone are stow's job (Omarchy's manual says so); bar widgets are
Omarchy plugins. omasushi is for the feature that cuts across — a package *and* a plugin
*and* a binding *and* a skill — which would otherwise be installed by hand after stow.

## Commands

```sh
omasushi use owner/repo/ime      # take one pack (GitHub shorthand, URL, or local path); remove it by the same name
omasushi use owner/repo          # every pack of a repository (packs added to it later come along)
omasushi list                    # packs in use ("via X" = pulled in by X's use:)
omasushi update                  # git pull remote repositories
omasushi remove <name>           # drop it from use:, unlink its files, delete the managed checkout if nothing else needs it

omasushi status [--json]         # overview: packs (git branch/commit, modified/behind), machine setup, pending & unrecorded counts
omasushi diff [--json]           # what sync would do; each action names the pack behind it (`<- owner/repo/ime`).
                                 # `?` lines are installed-but-unrecorded extras
omasushi sync                    # install what is missing, link files/snippets/skills. A failing action does not stop the others; they are listed again at the end and the exit code is 1
omasushi unlink [name] [--dry-run] # undo the links (restores .bak, and names the ones with no .bak — those leave the file missing); packages stay
                                 # (plan/apply/clean are accepted as aliases of diff/sync/unlink)
omasushi export <pack-dir>       # record installed things that no pack in use declares yet, into that pack (add-only)
omasushi export --local          # ...or into this machine's own file, where they stay
omasushi init <dir>              # scaffold a repository of packs; `init <repo>/<pack>` adds a pack to one
omasushi publish [name|owner/repo|url|path] [--dry-run] [--submit-repo owner/repo]
                                 # put a repository of packs on the belt: resolves the repo URL (the checkout you
                                 # stand in by default), warns if unpushed, and opens a prefilled "Submit packs"
                                 # issue on github.com/polidog/omasushi, where a workflow validates it and comments
                                 # the plates' URLs (one per pack). It refuses this machine's own file

omasushi -f <pack-dir> diff      # single-pack mode (developing a pack; this machine's file takes no part)
```

**This machine's own file** is `~/.config/omasushi/omasushi.yaml`: the packs it takes under
`use:`, plus what belongs to the machine alone in the same sections a pack has. It is layered on
top of everything it uses, so the machine has the last word. `list`/`status` mark it `local`;
`diff` says `<- local`. Its relative `files:` paths resolve against `~/.config/omasushi/`.

```yaml
# ~/.config/omasushi/omasushi.yaml — never published
use:
  - polidog/omakase/ime
  - polidog/omakase/herdr
packages: { aur: [work-vpn] }   # this machine's own, going no further
```

`use` and `remove` edit this file; it is also fine to edit by hand. Repositories are cloned to
`~/.local/share/omasushi/packs/<owner>/<repo>` (one checkout per repository, shared by its packs).
A file that says nothing at all falls back to the working directory, which is how a checkout is
driven in place. A `recipe:` key from before packs is folded into `use:` on first run.

**Where does a thing go?** Publishable and shared -> a pack in a repository. Particular to this
machine, or simply not for sharing (work VPN, a monitor layout, a token-adjacent dotfile) -> this
machine's file (`omasushi export --local`). Never `files:` in a public pack for anything secret.

## Packs and repositories

A pack is a directory with an `omasushi.yaml` plus the files it refers to. A repository holds one
pack per subdirectory; its root `omasushi.yaml` only names the repository (`name`, `description`)
and may be absent. A repository with no pack subdirectory is itself one pack. A root that has packs
*and* declares sections is refused.

```
omakase/
├── omasushi.yaml        # name: omakase — nothing else
├── ime/omasushi.yaml
├── fonts/
│   ├── omasushi.yaml
│   └── files/fontconfig/conf.d/...
└── herdr/
    ├── omasushi.yaml
    ├── files/herdr/config.toml
    └── hypr/herdr.lua   # a hyprland snippet
```

Suggest a pack whenever someone wants to share one feature's setup rather than a whole machine.
A pack you would want half of is a pack that should have been two — there is no cherry-picking
syntax; copy the directory and change it.

**Depending on other packs**: `use:` in a pack names the packs it needs. They load underneath it
(its own entries win on conflicts); `use` pulls packs, not repositories. A relative path is a
sibling in the same repository (`../fonts`) and keeps that repository's name. A pack that is only
a `use:` list is a bundle — a whole setup a new machine takes in one line.

**hypr snippets**: `hypr: hypr/herdr.lua` links the file into `~/.config/hypr/omasushi.d/<pack>.lua`;
omasushi writes `~/.config/hypr/omasushi.lua` (loads every file there, name order) and adds
`require("hypr.omasushi")` to `hyprland.lua` once, after Omarchy's defaults and the user's own
overrides. Several packs can add bindings without any of them owning `bindings.lua`. Prefer this to
`files:` on `bindings.lua`, which only one pack can hold at a time.

## Typical workflows

1. **Installed something on machine A** → `omasushi diff` shows `?` → `omasushi export <pack-dir>` (or `--local` to keep it on this machine) → commit & push
2. **Bring machine B up** → `omasushi update` → `omasushi diff` → `omasushi sync`
3. **Fresh machine** → `go install github.com/polidog/omasushi/cmd/omasushi@latest` → `omasushi use you/setup` (a bundle) and/or `omasushi use owner/repo/pack` → `omasushi sync`
4. **Share a feature** → `omasushi init my-packs` → `omasushi init my-packs/ime` → `omasushi export my-packs/ime` → copy config under `files/`, a hyprland snippet under `hypr/`, skills under `skills/` → push → `omasushi publish`
5. **Build on someone's pack** → `use:` it from your own pack (your entries win), or take both on the machine with two `use` lines
6. **Something that must not be published** → this machine's file (`omasushi export --local`, or edit `~/.config/omasushi/omasushi.yaml`). For secrets, that file plus a private git repo of the user's own — a public pack never hides anything

When the user says "sync", **show `diff` first, then run `sync`** — sync runs yay and
git clone, so do not run it without the user seeing the diff.

## omasushi.yaml (a pack)

```yaml
description: short blurb shown by tools   # name: is for a repository root; packs are named by their directory
use:                             # packs this one needs: loaded underneath it, so this file wins
  - ../fonts                     # a sibling in this repository
  - polidog/omakase/ime          # owner/repo[/pack], URL, or path
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
agent:                           # for the Omarchy default agent: omarchy.defaults.agent if set,
                                 # else this machine's `omarchy-default-agent`, else claude
  skills: skills                 # each subdir  -> ~/.claude/skills/<name> | ~/.codex/skills/<name>
                                 #                 | ~/.gemini/skills | ~/.copilot/skills | ~/.config/opencode/skill
  commands: commands             # each *.md    -> ~/.claude/commands/ | ~/.codex/prompts/ | ~/.config/opencode/command/
                                 #                 (gemini/copilot: no prompts dir, skipped with a note)
claude:                          # same shape, but always Claude Code (~/.claude) whatever the default agent
  skills: skills
  commands: commands
hypr: hypr/herdr.lua             # a hyprland snippet -> ~/.config/hypr/omasushi.d/<pack>.lua, loaded from hyprland.lua
files:
  files/herdr/config.toml: ~/.config/herdr/config.toml   # symlink; existing file moved to .bak
```

Packs stack in `use` order; a later pack wins for the same key/destination.

## Editing rules

- Never put secrets (tokens, `calendar-sync.json`, …) under `files:` of a pack — packs are meant to be public; this machine's file is where machine-only things live
- To share a file: copy it into the pack's `files/`, then add the mapping; `sync` swaps the original for a symlink
- Hyprland bindings go in a `hypr:` snippet, not a `files:` link on `bindings.lua`
- Skills/commands are linked **per entry**, so the machine's own `~/.claude/skills` / `~/.codex/skills` stay untouched
- `omarchy font set` / `omarchy theme set` rewrite terminal configs with `sed -i`, which turns the symlink back into a real file. Copy the new file into the pack and `sync` again (diff shows the `file-link` again)
- `font` / `defaults` empty means "don't care". `export` only fills them when nothing in use sets them
- First-party `omarchy.*` plugins are ignored by probe/export

## Source layout (github.com/polidog/omasushi)

- `cmd/omasushi/manifest.go` — YAML types, `Manifest.merge`, `declares`
- `cmd/omasushi/pack.go` — `Pack`, `Local` (this machine's file, `recipe:` migration), `use`/`remove`/`update`, source resolution (`parseSource`: owner/repo[/pack]), pack discovery (`packDirs`, `packsIn`), `resolveUses` layering (`loadUse` keeps a sibling's repository name)
- `cmd/omasushi/hypr.go` — the `hypr:` snippet directory, loader and `hyprland.lua` line
- `cmd/omasushi/probe.go` — read the real machine (`State`). Add a `probeXxx` here for a new target
- `cmd/omasushi/plan.go` — diff → `Action{Kind, Desc, Pack, Run}`; `packLinks` expands files/hypr/skills/commands
- `cmd/omasushi/unlink.go` — the reverse of the links; rewrites the hypr loader
- `cmd/omasushi/main.go` — CLI, `activePacks` (this machine's file is the top layer, `-f` bypasses it), `exportTarget`, `export`, `init`
- `cmd/omasushi/publish.go` — `publish`: repo URL resolution/canonicalisation and the prefilled submission issue on the submit repo (`submitRepo` default, `$OMASUSHI_SUBMIT_REPO`, `--submit-repo`); `publishDir` refuses this machine's file
- the Omarchy bar widget lives in its own repository, [polidog/omarchy-omasushi](https://github.com/polidog/omarchy-omasushi) (`omarchy plugin add https://github.com/polidog/omarchy-omasushi.git`): shows pending actions (`diff --json`: `packs`, `actions[].pack`) and runs sync in a floating terminal. `plugin/omasushi.yaml` here is the pack that installs it
