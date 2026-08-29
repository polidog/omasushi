# omasushi 🍣

Share your [Omarchy](https://omarchy.org) setup as a git repository, and keep every
machine you own — or anyone who likes your setup — in sync with it.

A **recipe** is a repo with an `omasushi.yaml` plus the files it refers to:

```
my-recipe/
├── omasushi.yaml      # packages, plugins, defaults, file mappings
├── files/             # dotfiles, symlinked into place
├── skills/            # Claude Code skills   -> ~/.claude/skills/<name>
└── commands/          # Claude Code commands -> ~/.claude/commands/<name>.md
```

`omasushi` diffs the recipe against the real machine and drives the existing
`omarchy` / `herdr` / `yay` CLIs to close the gap. It never uninstalls anything.

## Install

```sh
go install github.com/polidog/omasushi/cmd/omasushi@latest
```

Optional bar widget (shows pending actions, applies from the bar):

```sh
omarchy plugin add https://github.com/polidog/omasushi.git --enable
```

## Use someone's recipe

```sh
omasushi use polidog/omasushi-recipe   # GitHub shorthand, full URL, or a local path
omasushi plan                          # what would change
omasushi apply                         # install missing packages/plugins, link files
```

Several recipes can be stacked (`omasushi use` again); later ones win on conflicts.
`omasushi update` pulls them all.

## Publish your own

```sh
omasushi init my-recipe
cd my-recipe
omasushi export             # records this machine's AUR packages, plugins, font, defaults
cp ~/.config/kitty/kitty.conf files/kitty.conf
#   then add   files/kitty.conf: ~/.config/kitty/kitty.conf   under files:
cp -r ~/.claude/skills/my-skill skills/
git init && git add . && git commit -m "my setup" && gh repo create --public --push
```

Anyone can now `omasushi use you/my-recipe`.

## What a recipe can declare

| key | what apply does |
|---|---|
| `packages.pacman` / `packages.aur` | `omarchy-pkg-add` / `omarchy-pkg-aur-add` for missing ones |
| `omarchy.font` | `omarchy-font-set` |
| `omarchy.defaults.{agent,browser,editor,terminal}` | `omarchy-default-*` |
| `omarchy.plugins[]` `{url, enable}` | `omarchy-plugin-add` / `omarchy-plugin-enable` |
| `herdr.plugins[]` `{source, ref}` | `herdr plugin install` |
| `claude.skills` (dir) | symlink each subdirectory to `~/.claude/skills/<name>` |
| `claude.commands` (dir) | symlink each `*.md` to `~/.claude/commands/<name>.md` |
| `files` `{recipe-path: ~/dest}` | symlink; an existing real file is moved to `.bak` |
| `hosts.<hostname>` | overlay merged onto the base for that machine |

See [`recipe-template/omasushi.yaml`](recipe-template/omasushi.yaml) for a commented example.

## Commands

```
omasushi use <owner/repo|url|path>   add a recipe
omasushi list | update | remove <name>
omasushi plan [--json]               diff (json is what the bar widget reads)
omasushi apply                       make it so
omasushi export [--to recipe] [--host name]
                                     record installed things into a recipe (add-only)
omasushi init [dir]                  scaffold a recipe
omasushi -f omasushi.yaml <cmd>      single-manifest mode, for working inside a recipe
omasushi -H <host> <cmd>             resolve hosts.<host> as if on that machine
```

## Notes

- Recipes are meant to be public: keep tokens and per-machine secrets out of `files/`.
- Machine-specific bits (GPU drivers, monitor layouts) go under `hosts.<hostname>`.
- `omarchy font set` / `theme set` rewrite terminal configs in place, turning a symlink
  back into a file. Copy the new file into the recipe and `apply` again.
- A Claude Code skill for driving omasushi lives in [`skills/omasushi`](skills/omasushi);
  this repo is itself a valid recipe, so `omasushi use polidog/omasushi` installs it.

## License

MIT
