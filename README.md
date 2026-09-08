# omasushi

Add a feature to a stock [Omarchy](https://omarchy.org) machine — Japanese input,
tmux-style keys for herdr, a font setup — as one unit: the packages, the Omarchy
plugins, the font and default apps, the config it needs, the agent skills. Diff
the machine against it, sync, hand it to someone else.

```sh
omasushi use polidog/omakase/ime     # take one pack from a repo
omasushi diff                        # what is missing on this machine
omasushi sync                        # install it, link it
```

## Packs

A **pack** is a directory with an `omasushi.yaml` and the files it refers to.
It declares everything one feature needs on top of stock Omarchy:

```
herdr/
├── omasushi.yaml
├── files/herdr/config.toml
└── hypr/herdr.lua           # a hyprland snippet, loaded alongside other packs'
```

```yaml
# herdr/omasushi.yaml — tmux-style herdr: prefix ctrl+b, the plugins it drives,
# and the bar widget that shows agent status.
description: herdr with tmux-style keybindings
packages:
  pacman: [fcitx5, jq]
omarchy:
  plugins:
    - url: https://github.com/fabean/omarchy-herdr.git
      enable: true
herdr:
  plugins:
    - source: polidog/herdr-gh-issue-label
files:
  files/herdr/config.toml: ~/.config/herdr/config.toml
hypr: hypr/herdr.lua
```

A repo is a set of packs: every subdirectory with an `omasushi.yaml` is one.
The root `omasushi.yaml` only names the repo.

```
omakase/
├── omasushi.yaml     # name: omakase / description: polidog's packs
├── ime/
├── fonts/
├── kitty/
└── herdr/
```

`omasushi use owner/repo/herdr` takes one pack; `omasushi use owner/repo` takes
them all. Packs from different repos stack: `diff` names the pack behind each
pending action (`install fcitx5-mozc  <- omakase/ime`).

## What a pack can declare

| key | what sync does |
|---|---|
| `use` (packs) | take these packs too, loaded underneath this one — it wins on conflicts. See "Build on other people's packs" |
| `packages.pacman` / `packages.aur` | `omarchy-pkg-add` / `omarchy-pkg-aur-add` for missing ones |
| `omarchy.font` | `omarchy-font-set` |
| `omarchy.defaults.{agent,browser,editor,terminal}` | `omarchy-default-*` |
| `omarchy.plugins[]` `{url, enable}` | `omarchy-plugin-add` / `omarchy-plugin-enable` |
| `herdr.plugins[]` `{source, ref}` | `herdr plugin install` |
| `hypr` (a `.lua` file) | link it into `~/.config/hypr/omasushi/<pack>.lua`, loaded from `hyprland.lua` after Omarchy's defaults — so several packs can add bindings without fighting over `bindings.lua` |
| `files` `{pack-path: ~/dest}` | symlink; an existing real file is moved to `.bak` |
| `agent.skills` (dir) | symlink each subdirectory into the **default agent's** skills directory (`~/.claude/skills/<name>`, `~/.codex/skills/<name>`, …). The agent is `omarchy.defaults.agent` if any pack sets it, else this machine's `omarchy-default-agent`, else claude |
| `agent.commands` (dir) | symlink each `*.md` likewise (`~/.claude/commands/`, `~/.codex/prompts/`, …) |
| `claude.skills` / `claude.commands` (dir) | same, but always for Claude Code, whatever the default agent |

`omasushi` never uninstalls anything; `unlink` puts `.bak` originals back.

### Where omasushi stops

Omarchy's manual says to share dotfiles with stow, and for dotfiles alone that is
enough — omasushi is not a stow. Bar widgets are Omarchy plugins. omasushi is for
what cuts across both: the feature that is a package *and* a plugin *and* a
hyprland binding *and* a skill, which you would otherwise install by hand after
`stow` was done.

## Build on other people's packs

A pack can say which packs it needs. They load underneath it, so its own entries
win on conflicts:

```yaml
# kitty/omasushi.yaml — kitty.conf names UDEV Gothic, which the fonts pack installs
use: [polidog/omakase/fonts]
omarchy: { defaults: { terminal: kitty } }
files: { files/kitty.conf: ~/.config/kitty/kitty.conf }
```

`use` pulls packs, not repos, so nothing comes along that you did not name.
`list` and `status` show what came via what (`fonts  via kitty`).

A pack that is only a `use:` list is a bundle — your whole setup, other people's
packs included, that a new machine takes in one line:

```yaml
# setup/omasushi.yaml
use:
  - polidog/omakase/ime
  - polidog/omakase/herdr
  - someone/packs/nvim
```

Want part of someone's pack but not the rest? Packs are directories: copy it
into your repo and change what you like. There is no cherry-picking syntax — a
pack you would want half of is a pack that should have been two.

## This machine

What this machine takes, and what belongs to it alone, is one file:
`~/.config/omasushi/omasushi.yaml`. It is an `omasushi.yaml` like a pack's, with
a `use:` list on top:

```yaml
use:
  - polidog/omakase/ime
  - polidog/omakase/herdr
  - someone/packs/nvim
packages:
  aur: [work-vpn]          # this machine's own, never published
files:
  files/work/gitconfig: ~/.config/git/config.work
```

`use`, `remove` and `export --local` edit it, and it is fine to edit by hand.
`list` and `status` mark its entries `local`; `publish` refuses it. Keep it in a
private repo if your machines should share it.

## Make your own

```sh
omasushi init my-packs            # a repository of packs
omasushi init my-packs/ime        # add a pack to it
omasushi export my-packs/ime      # record installed things not yet in any pack — then keep what belongs here
cp -r ~/.claude/skills/my-skill my-packs/ime/skills/
cd my-packs && git init && git add . && git commit -m "packs" && gh repo create --public --push
```

Anyone can now `omasushi use you/my-packs/ime`. Anything you would rather not
hand them goes to this machine's file instead: `omasushi export --local`.

To put it on [omasushi.dev](https://omasushi.dev) where others can find it:

```sh
omasushi publish        # opens the prefilled submission issue for the repo you are in
```

The belt lists packs, one plate each — `ime`, `fonts`, `herdr` — not whole
setups. It reads the repo URL from `origin`, checks that `omasushi.yaml` is
committed and pushed, and opens a "Submit" issue on this repository; a workflow
validates the repo and comments the plates' URLs. `--dry-run` only prints the
issue URL; `--submit-repo` or `$OMASUSHI_SUBMIT_REPO` points at another
submission repo.

## Commands

```
omasushi use <owner/repo[/pack]|url|path>
                                     add a pack (or every pack of a repo) to this machine
omasushi list | update | remove <name>
omasushi status [--json]             where am I: packs + their git state, this
                                     machine's setup, pending/unrecorded counts
omasushi diff [--json]               what sync would do (json is what the bar widget reads)
omasushi sync                        make it so; an action that fails is reported
                                     and the rest still run (exit 1 at the end)
omasushi unlink [name] [--dry-run]   undo sync's links: remove the symlinks, put
                                     .bak originals back (never uninstalls)
omasushi export <pack-dir> | --local record installed things no pack declares into that
                                     pack (add-only), or into this machine's own file
omasushi init <dir>                  scaffold a repository of packs, or a pack inside one
omasushi publish [path] [--dry-run]  register a repo of packs on omasushi.dev
omasushi skill install|update|remove|list [--agent name]
                                     copy the bundled omasushi skill into an agent's
                                     global skills dir (no pack needed)
omasushi -f <pack-dir> <cmd>         single-pack mode, for working inside a pack
                                     (this machine's file takes no part)
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

- Packs are meant to be public: keep tokens and per-machine secrets out of them.
  What belongs to one machine belongs in `~/.config/omasushi/omasushi.yaml`,
  which is never published.
- `omarchy font set` / `theme set` rewrite terminal configs in place, turning a
  symlink back into a file. Copy the new file into the pack and `sync` again.
  Hyprland config is not affected: packs add snippets, they do not own `bindings.lua`.
- This repo is itself two packs: `plugin/` (installs the bar widget, which lives
  in [polidog/omarchy-omasushi](https://github.com/polidog/omarchy-omasushi)) and
  `claude/` (a skill for driving omasushi, in [`claude/skills/omasushi`](claude/skills/omasushi),
  linked for whichever agent is the Omarchy default). `omasushi use polidog/omasushi`
  installs both, `omasushi use polidog/omasushi/claude` just the skill. Packs to
  copy from live in [polidog/omakase](https://github.com/polidog/omakase).

## License

MIT
