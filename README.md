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
`omasushi clean` takes the symlinks back out and restores the `.bak` originals.

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
omarchy plugin add https://github.com/polidog/omasushi.git --enable
```

## Just the skill

To teach your AI agent the omasushi CLI without adopting any omakase:

```sh
omasushi skill install            # bundled skill -> the Omarchy default agent's global skills
omasushi skill install --agent claude   # or pick one: claude|codex|gemini|copilot|opencode
omasushi skill list | remove
```

The skill is embedded in the binary and copied into `~/.claude/skills/omasushi`,
`~/.codex/skills/omasushi`, … so `go install` is all you need.

## Use someone's omakase

```sh
omasushi use polidog/omakase            # GitHub shorthand, full URL, or a local path
omasushi use polidog/omakase/herdr      # one part of a split repository
omasushi plan                          # what would change
omasushi apply                         # install missing packages/plugins, link files
```

Several omakases can be stacked (`omasushi use` again); later ones win on conflicts.
`omasushi update` pulls them all.

## Publish your own

```sh
omasushi init my-omakase
cd my-omakase
omasushi export             # records this machine's AUR packages, plugins, font, defaults
cp ~/.config/kitty/kitty.conf files/kitty.conf
#   then add   files/kitty.conf: ~/.config/kitty/kitty.conf   under files:
cp -r ~/.claude/skills/my-skill skills/   # SKILL.md format is shared by Claude Code, Codex, Gemini CLI …
git init && git add . && git commit -m "my setup" && gh repo create --public --push
```

Anyone can now `omasushi use you/my-omakase`.

To put it on the [omasushi.dev](https://omasushi.dev) conveyor belt where others can find it:

```sh
omasushi publish            # registers this repo on omasushi.dev, prints the plate's URL
```

`publish` reads the repo URL from `origin`, checks that `omasushi.yaml` is committed
and pushed, and POSTs it to the site's `/api/omakase` — no account needed; the site
fetches `omasushi.yaml` from the public repo itself. `--open` opens the new plate in
your browser, `--browser` uses the web form instead of the API, `--dry-run` only
prints the URL; `--web URL` or `$OMASUSHI_WEB_URL` points at another instance
(e.g. `http://localhost:3000` when hacking on omasushi-web).

## What an omakase can declare

| key | what apply does |
|---|---|
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
| `parts` (root only) | feature-sized pieces, written inline or as sub-directories with their own `omasushi.yaml`; `use owner/repo` takes them all, `use owner/repo/<part>` one. A manifest that declares parts is only their index — its own sections are not applied |

See [`omakase-template/omasushi.yaml`](omakase-template/omasushi.yaml) for a commented example.

## Commands

```
omasushi use <owner/repo[/part]|url|path>
                                     add an omakase (or one part of a split repo)
omasushi list | update | remove <name>
omasushi status [--json]             where am I: omakases + their git state, this
                                     machine's setup, pending/unrecorded counts
omasushi plan [--json]               diff (json is what the bar widget reads)
omasushi apply                       make it so
omasushi clean [name] [--dry-run]    undo apply's links: remove the symlinks, put
                                     .bak originals back (never uninstalls)
omasushi export [--to omakase] [--host name]
                                     record installed things into an omakase (add-only)
omasushi init [dir]                  scaffold an omakase
omasushi publish [name|repo|path] [--open|--browser|--dry-run] [--web URL]
                                     register an omakase on omasushi-web
omasushi skill install|remove|list [--agent name]
                                     copy the bundled omasushi skill into an agent's
                                     global skills dir (no omakase needed)
omasushi -f omasushi.yaml <cmd>      single-manifest mode, for working inside an omakase
omasushi -H <host> <cmd>             resolve hosts.<host> as if on that machine
```

## Releasing

Bump `version` in `manifest.json`, then either push a tag or run the
[release workflow](https://github.com/polidog/omasushi/actions/workflows/release.yml)
from the Actions tab with the version as input (it creates the tag for you):

```sh
git tag v0.2.0 && git push origin v0.2.0
```

CI checks that the tag matches `manifest.json`, builds `omasushi-vX.Y.Z-linux-{amd64,arm64}.tar.gz`
with `checksums.txt`, and publishes a GitHub Release with auto-generated notes.

## Notes

- Omakases are meant to be public: keep tokens and per-machine secrets out of `files/`.
- Machine-specific bits (GPU drivers, monitor layouts) go under `hosts.<hostname>`.
- `omarchy font set` / `theme set` rewrite terminal configs in place, turning a symlink
  back into a file. Copy the new file into the omakase and `apply` again.
- This repo is itself a split omakase, of what belongs to the tool: `plugin/` (bar widget)
  and `claude/` (a skill for driving omasushi, in
  [`claude/skills/omasushi`](claude/skills/omasushi), linked for whichever agent is the
  Omarchy default: Claude Code, Codex, …). `omasushi use polidog/omasushi`
  installs both, `omasushi use polidog/omasushi/claude` just the skill. A machine setup to
  copy from lives in [polidog/omakase](https://github.com/polidog/omakase).

## License

MIT
