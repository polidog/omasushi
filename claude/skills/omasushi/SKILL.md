---
name: omasushi
description: Sync an Omarchy machine from shared "omakase" repositories with the `omasushi` CLI — AUR packages, Omarchy plugins/defaults/font, Herdr plugins, dotfiles, and AI agent skills/commands (Claude Code, Codex, Gemini CLI, ...). Use when the user wants to sync their setup, record what this machine has, set up a new machine, share their config with others, separate what stays on this machine from what they publish, or edit omasushi.yaml.
---

# omasushi

`omasushi` diffs one or more **omakases** (git repos with an `omasushi.yaml`, plus this
machine's own manifest) against the real machine and calls the existing Omarchy / Herdr
CLIs to close the gap.
It is a thin wrapper: no reimplemented yay or git clone. **It never removes anything.**

## Commands

```sh
omasushi use owner/repo          # add an omakase to this machine's use: (GitHub shorthand, URL, or local path); all parts of a split repo
omasushi use owner/repo/herdr    # one part of a split repo (name shows as repo/herdr; remove it by that name)
omasushi use --recipe owner/repo # ...as the recipe instead: the omakase this machine publishes and exports to
omasushi recipe [path|none]      # show or set the recipe (a checkout of the user's own is best: export edits it)
omasushi list                    # omakases in use ("via X" = pulled in by X's use: declaration)
omasushi update                  # git pull remote omakases
omasushi remove <name>           # drop it from use: (a part of a repo added whole is replaced by its siblings),
                                 # unlink its files, and delete the managed checkout if nothing else needs it

omasushi status [--json]         # overview: omakases (git branch/commit, modified/behind), machine setup, pending & unrecorded counts
omasushi diff [--json]           # what sync would do; each action names the omakase behind it (`<- owner/repo`).
                                 # `?` lines are installed-but-unrecorded extras
omasushi sync                    # install what is missing, symlink files/skills/commands. A failing action does not stop the others; they are listed again at the end and the exit code is 1
omasushi unlink [name] [--dry-run] # undo the symlinks (restores .bak, and names the ones with no .bak — those leave the file missing); packages stay
                                 # (plan/apply/clean are accepted as aliases of diff/sync/unlink)
omasushi export [--to machine|recipe|<omakase>] [--host <name>]   # record this machine into an omakase (add-only);
                                 # the recipe when set, else this machine; never records what
                                 # any stacked omakase already declares
omasushi init [dir]              # scaffold a new omakase repo
omasushi publish [name|owner/repo|url|path] [--dry-run] [--submit-repo owner/repo]
                                 # put it on the belt: resolves the repo URL (the checkout you are standing
                                 # in, else the recipe), warns if unpushed, and opens a prefilled "Submit an
                                 # omakase" issue on github.com/polidog/omasushi, where a workflow validates
                                 # it (the site fetches omasushi.yaml from the public repo) and comments the
                                 # plate's URL. It refuses this machine's own manifest

omasushi -f path/omasushi.yaml diff   # single-manifest mode (developing an omakase; the machine takes no part)
omasushi -H <hostname> diff            # resolve as another host
```

**This machine is an omakase too.** `~/.config/omasushi/omasushi.yaml` is an ordinary manifest —
same keys, same `use:`, same `hosts:` — with one key of its own, `recipe:`. Three layers of one
format: the omakases under `use:` at the bottom, the recipe over them, this file over both (so the
machine has the last word). Its relative `files:` paths resolve against `~/.config/omasushi/`.

```yaml
# ~/.config/omasushi/omasushi.yaml — never published, never on the belt
recipe: ~/src/my-omakase        # the omakase this machine publishes and exports to
use: [polidog/omakase/kitty]    # what it takes from other people
packages: { aur: [work-vpn] }   # this machine's own, going no further
```

`use`, `remove` and `recipe` edit this file; it is also fine to edit by hand. `use` records the source
as the user typed it, so `owner/repo` on a split repository picks up parts added to it later. Omakases
are cloned to `~/.local/share/omasushi/omakases/<owner>/<repo>` (one checkout per repository, shared by
its parts). A machine manifest that says nothing at all falls back to `./omasushi.yaml`, which is how a
checkout is driven in place. The pre-machine-manifest `config.yaml` is converted on first run
(`omakases:` -> `use:`, `mine:` -> `recipe:`).

**Where does a thing go?** Publishable and shared -> the recipe. Particular to this machine, or simply
not for sharing (work VPN, a monitor layout, a token-adjacent dotfile) -> the machine manifest
(`omasushi export --to machine`). Never `files:` in a public recipe for anything secret.

**Parts**: a repository can be split into feature-sized pieces that users mix across
repositories. Suggest this when someone wants to share one feature's config rather than a
whole machine. Two spellings, mixable in one manifest:

- **inline** (default; `parts:` as a map) — each part is written in the root `omasushi.yaml`
  and its paths stay relative to the repository root, so one `files/` tree serves every part
- **directory** (`parts:` as a list, or a map entry with an empty value) — the part is a
  directory carrying its own `omasushi.yaml`, and its paths are relative to that directory

A part takes anything a manifest can (including `hosts:`) except nested `parts:`. A manifest
that declares parts is only their index: its own top-level sections are not applied.
`omasushi export --to <repo>/<part>` folds the new entries back into that part.

## Typical workflows

1. **Installed something on machine A** → `omasushi diff` shows `?` → `omasushi export` (writes to the recipe; `--to machine` keeps it on this machine instead) → commit & push the recipe
2. **Bring machine B up** → `omasushi update` → `omasushi diff` → `omasushi sync`
3. **Fresh machine** → `go install github.com/polidog/omasushi/cmd/omasushi@latest` → `omasushi use --recipe you/your-omakase` (their own setup) and/or `omasushi use owner/repo` (other people's) → `omasushi sync`
4. **Publish your setup** → `omasushi init my-omakase` → `omasushi recipe ./my-omakase` → copy dotfiles under `files/`, skills under `skills/` → `omasushi export` → push → `omasushi publish` (opens the submission issue; the belt validates the public repo)
5. **Build on other people's omakases** → put them under `use:` in your recipe (your entries win on conflicts), so one repo carries the whole combination for the next machine; `use:` in the machine manifest is for what only this machine takes
6. **Want one package/dotfile/skill out of someone's big omakase, not the whole thing** → use the long `use:` form with `only:` → `list`/`status` show `only <paths>` for what was taken
7. **Something that must not be published** → leave it out of the recipe and put it in the machine manifest (`omasushi export --to machine`, or edit `~/.config/omasushi/omasushi.yaml`). For secrets, that file plus a private git repo of the user's own — a public recipe never hides anything

When the user says "sync", **show `diff` first, then run `sync`** — sync runs yay and
git clone, so do not run it without the user seeing the diff.

## omasushi.yaml

```yaml
name: my-setup                   # a repository's manifest. recipe: is not one of these keys —
description: short blurb shown by tools   # it belongs to the machine manifest alone
use:                             # other omakases this one builds on: loaded underneath it,
  - polidog/omakase/kitty        # so this file wins on conflicts. owner/repo[/part], URL,
  - someone/nvim-setup           # or path (relative = sibling of this repo)
  - source: someone/big          # long form: take only the items only: names, and nothing
    only:                        # else from it — their own use: chain included
      packages.aur: [kitty]      # keys are paths into their manifest: packages.pacman|aur,
      files: [files/kitty.conf]  # omarchy.font|defaults.<kind>|plugins, herdr.plugins, files,
      agent.skills: [review]     # {agent,claude}.{skills,commands}. A list under a leaf names
                                 # its entries, under a section its sub-keys (packages: [aur]);
                                 # no list takes the path whole. An unknown path is an error
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
files:
  files/kitty/kitty.conf: ~/.config/kitty/kitty.conf   # symlink; existing file moved to .bak
hosts:
  <hostname>:                    # overlay merged onto the base (lists unioned, scalars win)
    packages: {...}
    files: {...}

# root of a split repo only, instead of the sections above:
parts:
  kitty:                         # inline: paths relative to the repository root
    files: { files/kitty/kitty.conf: ~/.config/kitty/kitty.conf }
  nvim:                          # empty value: nvim/omasushi.yaml, paths relative to nvim/
parts: [herdr, kitty]            # list form: every part is a directory
```

Several omakases stack in `use` order; a later omakase wins for the same key/destination.

## Editing rules

- Never put secrets (tokens, `calendar-sync.json`, …) under `files:` of a recipe — omakases are meant to be public; the machine manifest is where machine-only things live
- To share a file: copy it into the omakase's `files/`, then add the mapping; `sync` swaps the original for a symlink
- Skills/commands are linked **per entry**, so the machine's own `~/.claude/skills` / `~/.codex/skills` stay untouched
- `omarchy font set` / `omarchy theme set` rewrite terminal configs with `sed -i`, which turns the symlink back into a real file. Copy the new file into the omakase and `sync` again (diff shows the `file-link` again)
- `font` / `defaults` empty means "don't care". `export` only fills them when the base is empty
- Machine-specific things (GPU drivers, monitor layouts): `hosts.<hostname>` when the omakase is shared
  across the user's own machines and should carry them; this machine's own manifest when they have no
  business being in a published repository at all
- First-party `omarchy.*` plugins are ignored by probe/export

## Source layout (github.com/polidog/omasushi)

- `cmd/omasushi/manifest.go` — YAML types, `Parts` (list/map unmarshalling), `Resolve(host)`, `Overlay.merge`
- `cmd/omasushi/omakase.go` — `Machine` (the machine manifest, `recipe:`, `config.yaml` migration), `use`/`remove`/`update`, source resolution (`parseSource`: owner/repo[/part]), parts expansion (`omakasesIn`, inline vs directory parts), `resolveUses` layering, `Omakase.Save`
- `cmd/omasushi/probe.go` — read the real machine (`State`). Add a `probeXxx` here for a new target
- `cmd/omasushi/plan.go` — diff → `Action{Kind, Desc, Run}`; `omakaseLinks` expands files/skills/commands
- `cmd/omasushi/main.go` — CLI, `activeOmakases` (the machine manifest is the top layer, `-f` bypasses it), `exportTarget` (recipe, else machine), `export`, `init`
- `cmd/omasushi/publish.go` — `publish`: repo URL resolution/canonicalisation and the prefilled submission issue on the submit repo (`submitRepo` default, `$OMASUSHI_SUBMIT_REPO`, `--submit-repo`); `publishDir` refuses the machine manifest
- the Omarchy bar widget lives in its own repository, [polidog/omarchy-omasushi](https://github.com/polidog/omarchy-omasushi) (`omarchy plugin add https://github.com/polidog/omarchy-omasushi.git`): shows pending actions and runs sync in a floating terminal. `plugin/omasushi.yaml` here is the part that installs it
