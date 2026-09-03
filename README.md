# omasushi 🍣

Share your [Omarchy](https://omarchy.org) setup as a git repository, and keep every
machine you own — or anyone who likes your setup — in sync with it.

A **omakase** is a repo with an `omasushi.yaml` plus the files it refers to:

```
my-omakase/
├── omasushi.yaml      # packages, plugins, defaults, file mappings
├── files/             # dotfiles, symlinked into place
├── skills/            # Claude Code skills   -> ~/.claude/skills/<name>
└── commands/          # Claude Code commands -> ~/.claude/commands/<name>.md
```

Or split it into feature-sized **parts** that people can mix and match. The short
way is to write them straight into the root `omasushi.yaml`, keeping one `files/`
tree for the whole repository:

```yaml
name: my-omakase
parts:
  kitty:
    files: { files/kitty/kitty.conf: ~/.config/kitty/kitty.conf }
  herdr:
    herdr: { plugins: [{ source: owner/repo }] }
    files: { files/herdr/config.toml: ~/.config/herdr/config.toml }
  agent:
    agent: { skills: skills }
```

A part large enough to want its own directory can have one — give it a directory
with an `omasushi.yaml` whose paths are relative to that directory, and leave its
entry empty (or list the parts as `parts: [kitty, herdr, claude]`, which is all
directory parts):

```
my-omakase/
├── omasushi.yaml      # parts: { kitty: {...}, nvim: }
├── files/kitty/kitty.conf
└── nvim/
    ├── omasushi.yaml  # files: { files/init.lua: ~/.config/nvim/init.lua }
    └── files/init.lua
```

`omasushi use you/my-omakase` takes every part; `omasushi use you/my-omakase/herdr`
takes one, and stacks with parts from other people's repos.

`omasushi` diffs the omakase against the real machine and drives the existing
`omarchy` / `herdr` / `yay` CLIs to close the gap. It never uninstalls anything;
`omasushi unlink` takes the symlinks back out and restores the `.bak` originals; a
link with no `.bak` leaves nothing behind, and is named at the end of the run.

## Install

Prebuilt binaries for Linux (amd64 / arm64) are on the
[releases page](https://github.com/polidog/omasushi/releases):

```sh
curl -fsSL https://github.com/polidog/omasushi/releases/latest/download/omasushi-$(curl -fsSL https://api.github.com/repos/polidog/omasushi/releases/latest | jq -r .tag_name)-linux-$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz \
  | tar -xz --strip-components=1 -C ~/.local/bin --wildcards '*/omasushi'
```

Or build from source:

```sh
go install github.com/polidog/omasushi/cmd/omasushi@latest
```

Optional bar widget (shows pending actions, applies from the bar):

```sh
omarchy plugin add https://github.com/polidog/omarchy-omasushi.git --enable
```

## Just the skill

To teach your AI agent the omasushi CLI without adopting any omakase:

```sh
omasushi skill install            # bundled skill -> the Omarchy default agent's global skills
omasushi skill install --agent claude   # or pick one: claude|codex|gemini|copilot|opencode
omasushi skill list | update | remove
```

The skill is embedded in the binary and copied into `~/.claude/skills/omasushi`,
`~/.codex/skills/omasushi`, … so `go install` is all you need. After a newer
`go install`, `omasushi skill update` (or plain `omasushi update`) rewrites it.

## Use someone's omakase

```sh
omasushi use polidog/omakase            # GitHub shorthand, full URL, or a local path
omasushi use polidog/omakase/herdr      # one part of a split repository
omasushi diff                          # what would change
omasushi sync                          # install missing packages/plugins, link files
```

Several omakases can be stacked (`omasushi use` again); later ones win on conflicts.
`omasushi update` pulls them all. `omasushi diff` names the omakase behind each
pending action (`install foo  <- polidog/omakase`), so it stays clear who put
what on your plate.

What you take is recorded in this machine's own omakase, `~/.config/omasushi/omasushi.yaml`
— see [This machine](#this-machine).

## Build your own on top of others'

Your omakase can declare the omakases it builds on with `use:`, so the whole
combination — other people's parts plus your own layer — lives in one repo:

```yaml
name: my-setup
use:                        # loaded underneath this file; your entries win on conflicts
  - polidog/omakase/kitty
  - someone/nvim-setup
packages:
  aur: [my-extra-tool]      # your own picks on top
```

`omasushi use you/my-setup` pulls the whole stack in (dependencies show as
`via my-setup` in `list`/`status`), and a new machine needs just that one line.

Don't want all of someone's omakase? Take the items you came for with `only:`,
and the rest stays on the belt:

```yaml
use:
  - source: someone/big
    only:
      packages.aur: [kitty]              # just this package
      files: [files/kitty/kitty.conf]    # just this dotfile
      agent.skills: [review]             # just this skill out of their skills/
```

`only:` keys are paths into the used omakase's manifest (`packages.aur`,
`omarchy.font`, `omarchy.plugins`, `herdr.plugins`, `files`,
`agent.skills` …). Under a leaf the list names its own entries; under a section
it names the sub-keys to descend into (`packages: [aur]`), and a key with no
list takes everything under it (`herdr.plugins:`). A path that addresses
nothing is an error, so a typo won't quietly take nothing.

The narrowing covers everything that entry pulls in — including the used
omakase's own `use:` chain — so cherry-picking one package never drags a
stranger's whole tree in behind it. `list` and `status` show what was taken
(`only packages.aur, files`).

## This machine

A machine is not a recipe. What you publish is a repository other people can
take; what a particular machine needs — a work VPN, a monitor layout, the one
package you would rather not advertise — belongs to the machine. So omasushi
keeps them in different files:

```yaml
# ~/.config/omasushi/omasushi.yaml — this machine. Never published.
recipe: ~/src/my-omakase        # the omakase this machine publishes and exports to
use:                            # ...and what it takes from other people
  - polidog/omakase/kitty
  - someone/nvim-setup
packages:
  aur: [work-vpn]               # this machine's own, going no further
files:
  files/work/gitconfig: ~/.config/git/config.work   # relative to ~/.config/omasushi
```

It is an `omasushi.yaml` like any other — the same keys, the same `use:`, the
same `hosts:` — with one key of its own, `recipe:`. That makes three layers of
one format: the omakases under `use:` at the bottom, your recipe over them, and
this file over both, so the machine always has the last word.

```sh
omasushi recipe ~/src/my-omakase   # name the omakase this machine publishes
omasushi export                    # record what is installed — into the recipe
omasushi export --to machine       # ...or into this machine, where it stays
omasushi publish                   # only ever offers the recipe
```

Only this file never leaves the machine, and it is the one place `publish` will
not touch. Keep it under a private git repository of your own if you want your
machines to share it; the belt only ever sees the recipe.

`omasushi use`, `remove` and `recipe` all edit this file, so it is also fine to
edit by hand — `use:` there reads exactly like `use:` in a repository.

## Publish your own

```sh
omasushi init my-omakase
omasushi recipe ./my-omakase   # this machine's recipe: export writes here, publish offers it
omasushi export                # records this machine's AUR packages, plugins, font, defaults
cp ~/.config/kitty/kitty.conf my-omakase/files/kitty.conf
#   then add   files/kitty.conf: ~/.config/kitty/kitty.conf   under files:
cp -r ~/.claude/skills/my-skill my-omakase/skills/   # SKILL.md format is shared by Claude Code, Codex, Gemini CLI …
cd my-omakase && git init && git add . && git commit -m "my setup" && gh repo create --public --push
```

Anyone can now `omasushi use you/my-omakase`. Anything you would rather not hand
them stays out of it by living in [this machine's own manifest](#this-machine)
instead — `omasushi export --to machine`.

To put it on the [omasushi.dev](https://omasushi.dev) conveyor belt where others can find it:

```sh
omasushi publish            # opens the prefilled submission issue in your browser
```

With no argument `publish` takes the checkout you are standing in, else this
machine's `recipe:`; this machine's own manifest it refuses outright. It reads
the repo URL from `origin`, checks that `omasushi.yaml` is committed and pushed, and opens a prefilled "Submit an omakase" issue on this repository —
Omarchy-plugin style. Press Submit there; a workflow validates the repo (the site
fetches `omasushi.yaml` from the public repo itself), puts it on the belt and
comments the plate's URL on the issue. `--dry-run` only prints the issue URL;
`--submit-repo` or `$OMASUSHI_SUBMIT_REPO` points at another submission repo.

## What an omakase can declare

| key | what sync does |
|---|---|
| `use` | load these omakases (owner/repo[/part], URL, or path) underneath this one — it wins on conflicts. An entry may be `{source, only}` to take only the items `only:` names; see "Build your own on top of others'" |
| `packages.pacman` / `packages.aur` | `omarchy-pkg-add` / `omarchy-pkg-aur-add` for missing ones |
| `omarchy.font` | `omarchy-font-set` |
| `omarchy.defaults.{agent,browser,editor,terminal}` | `omarchy-default-*` |
| `omarchy.plugins[]` `{url, enable}` | `omarchy-plugin-add` / `omarchy-plugin-enable` |
| `herdr.plugins[]` `{source, ref}` | `herdr plugin install` |
| `agent.skills` (dir) | symlink each subdirectory into the **default agent's** skills directory: `~/.claude/skills/<name>`, `~/.codex/skills/<name>`, `~/.gemini/skills/<name>`, `~/.copilot/skills/<name>`, `~/.config/opencode/skill/<name>`. The agent is `omarchy.defaults.agent` if any omakase sets it, else this machine's `omarchy-default-agent`, else claude |
| `agent.commands` (dir) | symlink each `*.md` likewise: `~/.claude/commands/`, `~/.codex/prompts/`, `~/.config/opencode/command/` (agents without a prompts directory are skipped with a note) |
| `claude.skills` / `claude.commands` (dir) | same, but always for Claude Code (`~/.claude/skills`, `~/.claude/commands`), whatever the default agent |
| `files` `{omakase-path: ~/dest}` | symlink; an existing real file is moved to `.bak` |
| `hosts.<hostname>` | overlay merged onto the base for that machine |
| `recipe` | **this machine's manifest only** (`~/.config/omasushi/omasushi.yaml`): the omakase this machine publishes and exports to |
| `parts` (root only) | feature-sized pieces, written inline or as sub-directories with their own `omasushi.yaml`; `use owner/repo` takes them all, `use owner/repo/<part>` one. A manifest that declares parts is only their index — its own sections are not applied |

See [`omakase-template/omasushi.yaml`](omakase-template/omasushi.yaml) for a commented example.

## Commands

```
omasushi use [--recipe] <owner/repo[/part]|url|path>
                                     add an omakase (or one part of a split repo) to
                                     this machine's use:; --recipe puts it in recipe:
omasushi recipe [path|none]          show or set the omakase this machine publishes
omasushi list | update | remove <name>
omasushi status [--json]             where am I: omakases + their git state, this
                                     machine's setup, pending/unrecorded counts
omasushi diff [--json]               what sync would do (json is what the bar widget reads)
omasushi sync                        make it so; an action that fails is reported
                                     and the rest still run (exit 1 at the end)
omasushi unlink [name] [--dry-run]   undo sync's links: remove the symlinks, put
                                     .bak originals back (never uninstalls); links
                                     with no .bak are listed, since they leave a hole
                                     (plan/apply/clean still work as aliases)
omasushi export [--to machine|recipe|omakase] [--host name]
                                     record installed things into an omakase (add-only);
                                     the recipe when set, else this machine
omasushi init [dir]                  scaffold an omakase
omasushi publish [name|repo|path] [--dry-run]
                                     register an omakase on omasushi-web
omasushi skill install|update|remove|list [--agent name]
                                     copy the bundled omasushi skill into an agent's
                                     global skills dir (no omakase needed)
omasushi -f omasushi.yaml <cmd>      single-manifest mode, for working inside an omakase
                                     (this machine's own manifest takes no part)
omasushi -H <host> <cmd>             resolve hosts.<host> as if on that machine
```

## Releasing

Push a tag, or run the
[release workflow](https://github.com/polidog/omasushi/actions/workflows/release.yml)
from the Actions tab with the version as input (it creates the tag for you):

```sh
git tag v0.2.0 && git push origin v0.2.0
```

CI builds `omasushi-vX.Y.Z-linux-{amd64,arm64}.tar.gz` with `checksums.txt`, and
publishes a GitHub Release with auto-generated notes. The bar widget is released
separately, from [polidog/omarchy-omasushi](https://github.com/polidog/omarchy-omasushi).

## Notes

- Omakases are meant to be public: keep tokens and per-machine secrets out of `files/`.
  What belongs to one machine belongs in `~/.config/omasushi/omasushi.yaml`, which is
  never published.
- Machine-specific bits (GPU drivers, monitor layouts) go under `hosts.<hostname>`.
- `omarchy font set` / `theme set` rewrite terminal configs in place, turning a symlink
  back into a file. Copy the new file into the omakase and `sync` again.
- This repo is itself a split omakase, of what belongs to the tool: `plugin/` (installs the
  bar widget, which lives in [polidog/omarchy-omasushi](https://github.com/polidog/omarchy-omasushi))
  and `claude/` (a skill for driving omasushi, in
  [`claude/skills/omasushi`](claude/skills/omasushi), linked for whichever agent is the
  Omarchy default: Claude Code, Codex, …). `omasushi use polidog/omasushi`
  installs both, `omasushi use polidog/omasushi/claude` just the skill. A machine setup to
  copy from lives in [polidog/omakase](https://github.com/polidog/omakase).

## License

MIT
