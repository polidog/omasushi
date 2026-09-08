# omasushi

[English](README.md) | 日本語

素の [Omarchy](https://omarchy.org) マシンに機能をひとつ足す ——
日本語入力、herdr の tmux 風キーバインド、フォント環境 —— それを 1 つのまとまりとして扱う。
パッケージ、Omarchy プラグイン、フォントとデフォルトアプリ、必要な設定ファイル、エージェントのスキルまで。
マシンとの差分を見て、同期して、そのまま誰かに渡せる。

```sh
omasushi use polidog/omakase/ime     # リポジトリからパックを 1 つ取る
omasushi diff                        # このマシンに足りていないもの
omasushi sync                        # 入れて、リンクする
```

## パック

**パック**は `omasushi.yaml` と、そこから参照されるファイルを置いたディレクトリ。
素の Omarchy に対して、ある機能ひとつに必要なものをすべて宣言する。

```
herdr/
├── omasushi.yaml
├── files/herdr/config.toml
└── hypr/herdr.lua           # hyprland のスニペット。他のパックのものと並べて読み込まれる
```

```yaml
# herdr/omasushi.yaml — tmux 風の herdr: プレフィックスは ctrl+b、動かすプラグイン、
# エージェントの状態を出すバーウィジェット。
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

リポジトリはパックの集まり。`omasushi.yaml` を持つサブディレクトリが、それぞれ 1 つのパックになる。
ルートの `omasushi.yaml` はリポジトリの名前を書くだけ。

```
omakase/
├── omasushi.yaml     # name: omakase / description: polidog's packs
├── ime/
├── fonts/
├── kitty/
└── herdr/
```

`omasushi use owner/repo/herdr` はパックを 1 つ、`omasushi use owner/repo` は全部取る。
別のリポジトリのパックも重ねられる。`diff` は保留中の各アクションの出どころを添えて表示する
（`install fcitx5-mozc  <- omakase/ime`）。

## パックに書けること

| キー | sync が何をするか |
|---|---|
| `use`（パック） | それらのパックも取り込む。自分より下に読み込まれるので、衝突したら自分が勝つ。「他人のパックの上に作る」を参照 |
| `packages.pacman` / `packages.aur` | 入っていないものに `omarchy-pkg-add` / `omarchy-pkg-aur-add` |
| `omarchy.font` | `omarchy-font-set` |
| `omarchy.defaults.{agent,browser,editor,terminal}` | `omarchy-default-*` |
| `omarchy.plugins[]` `{url, enable}` | `omarchy-plugin-add` / `omarchy-plugin-enable` |
| `herdr.plugins[]` `{source, ref}` | `herdr plugin install` |
| `hypr`（`.lua` ファイル） | `~/.config/hypr/omasushi/<pack>.lua` にリンクし、`hyprland.lua` から Omarchy のデフォルトの後に読み込む。複数のパックが `bindings.lua` を取り合わずにキーバインドを足せる |
| `files` `{pack-path: ~/dest}` | シンボリックリンク。実体のファイルがあれば `.bak` に退避する |
| `agent.skills`（ディレクトリ） | 各サブディレクトリを**デフォルトエージェント**のスキルディレクトリにリンクする（`~/.claude/skills/<name>`、`~/.codex/skills/<name>`、…）。エージェントは、どれかのパックが `omarchy.defaults.agent` を設定していればそれ、なければこのマシンの `omarchy-default-agent`、それもなければ claude |
| `agent.commands`（ディレクトリ） | 各 `*.md` を同様にリンク（`~/.claude/commands/`、`~/.codex/prompts/`、…） |
| `claude.skills` / `claude.commands`（ディレクトリ） | 同じだが、デフォルトエージェントが何であれ常に Claude Code に対して |

`omasushi` は何もアンインストールしない。`unlink` は `.bak` の元ファイルを戻す。

### omasushi がやらないこと

Omarchy のマニュアルは dotfiles を stow で共有しろと言っていて、dotfiles だけならそれで足りる ——
omasushi は stow ではない。バーウィジェットは Omarchy プラグインだ。
omasushi はその両方にまたがるもののためにある。パッケージであり、*かつ*プラグインであり、
*かつ* hyprland のキーバインドであり、*かつ*スキルでもある機能 ——
`stow` が終わったあとに結局手で入れることになるやつだ。

## 他人のパックの上に作る

パックは、自分が必要とするパックを書ける。それらは自分より下に読み込まれるので、
衝突したときは自分の記述が勝つ。

```yaml
# kitty/omasushi.yaml — kitty.conf は UDEV Gothic を指定する。それを入れるのが fonts パック
use: [polidog/omakase/fonts]
omarchy: { defaults: { terminal: kitty } }
files: { files/kitty.conf: ~/.config/kitty/kitty.conf }
```

`use` が引くのはパックであってリポジトリではないので、名指ししていないものは付いてこない。
`list` と `status` は、何経由で来たかを表示する（`fonts  via kitty`）。

`use:` のリストだけのパックはバンドルになる。他人のパックも含めた自分の環境まるごとを、
新しいマシンが 1 行で取れる。

```yaml
# setup/omasushi.yaml
use:
  - polidog/omakase/ime
  - polidog/omakase/herdr
  - someone/packs/nvim
```

誰かのパックの一部だけ欲しい？ パックはディレクトリなので、自分のリポジトリにコピーして好きに変えればいい。
つまみ食いのための構文はない —— 半分だけ欲しくなるパックは、2 つに分かれているべきだったパックだ。

## このマシン

このマシンが何を取っているか、そしてこのマシンだけのものは何か。それが 1 つのファイルにまとまっている:
`~/.config/omasushi/omasushi.yaml`。パックの `omasushi.yaml` と同じ形で、先頭に `use:` のリストが載る。

```yaml
use:
  - polidog/omakase/ime
  - polidog/omakase/herdr
  - someone/packs/nvim
packages:
  aur: [work-vpn]          # このマシンだけのもの。公開されることはない
files:
  files/work/gitconfig: ~/.config/git/config.work
```

`use`、`remove`、`export --local` がこのファイルを書き換える。手で編集しても構わない。
`list` と `status` はここの項目を `local` と印を付け、`publish` は受け付けない。
マシン間で共有したいなら、プライベートリポジトリに置くとよい。

## 自分で作る

```sh
omasushi init my-packs            # パックのリポジトリ
omasushi init my-packs/ime        # そこにパックを足す
omasushi export my-packs/ime      # どのパックにも入っていない導入済みのものを書き出す —— そこから要るものだけ残す
cp -r ~/.claude/skills/my-skill my-packs/ime/skills/
cd my-packs && git init && git add . && git commit -m "packs" && gh repo create --public --push
```

これで誰でも `omasushi use you/my-packs/ime` できる。
人に渡したくないものは、代わりにこのマシンのファイルへ: `omasushi export --local`。

見つけてもらえるように [omasushi.dev](https://omasushi.dev) に載せるには:

```sh
omasushi publish        # いまいるリポジトリの、内容が埋まった登録用 issue を開く
```

ベルトに流れるのはパックで、1 パック 1 皿 —— `ime`、`fonts`、`herdr` —— 環境まるごとではない。
`origin` からリポジトリの URL を読み、`omasushi.yaml` がコミットされ push 済みかを確かめ、
このリポジトリに "Submit" issue を開く。ワークフローがリポジトリを検証し、各皿の URL をコメントする。
`--dry-run` は issue の URL を表示するだけ。`--submit-repo` または `$OMASUSHI_SUBMIT_REPO` で
別の登録先リポジトリを指定できる。

## コマンド

```
omasushi use <owner/repo[/pack]|url|path>
                                     パック（またはリポジトリの全パック）をこのマシンに足す
omasushi list | update | remove <name>
omasushi status [--json]             いまどこにいるか: パックと git の状態、
                                     このマシンの設定、保留中／未記録の件数
omasushi diff [--json]               sync が何をするか（json はバーウィジェットが読む）
omasushi sync                        実行する。失敗したアクションは報告され、
                                     残りはそのまま実行される（最後に exit 1）
omasushi unlink [name] [--dry-run]   sync のリンクを戻す: シンボリックリンクを消し、
                                     .bak の元ファイルを戻す（アンインストールはしない）
omasushi export <pack-dir> | --local どのパックも宣言していない導入済みのものを、そのパックに
                                     書き出す（追加のみ）。または、このマシンのファイルへ
omasushi init <dir>                  パックのリポジトリ、またはその中のパックを雛形から作る
omasushi publish [path] [--dry-run]  パックのリポジトリを omasushi.dev に登録する
omasushi skill install|update|remove|list [--agent name]
                                     同梱の omasushi スキルを、エージェントのグローバルな
                                     スキルディレクトリにコピーする（パック不要）
omasushi -f <pack-dir> <cmd>         単一パックモード。パックの中で作業するとき用
                                     （このマシンのファイルは関与しない）
```

## リリース

タグを push するか、Actions タブから
[release ワークフロー](https://github.com/polidog/omasushi/actions/workflows/release.yml)
をバージョン入力付きで実行する（タグは自動で作られる）:

```sh
git tag v0.2.0 && git push origin v0.2.0
```

CI が `omasushi-vX.Y.Z-linux-{amd64,arm64}.tar.gz` と `checksums.txt` をビルドし、
自動生成のリリースノート付きで GitHub Release を公開する。
バーウィジェットは [polidog/omarchy-omasushi](https://github.com/polidog/omarchy-omasushi) から別にリリースされる。

## 覚え書き

- パックは公開されるもの。トークンやマシン固有の秘密は入れないこと。
  1 台のマシンに属するものは `~/.config/omasushi/omasushi.yaml` へ。こちらは公開されない。
- `omarchy font set` / `theme set` はターミナルの設定ファイルをその場で書き換えるので、
  シンボリックリンクが実体のファイルに戻る。新しいファイルをパックにコピーして、もう一度 `sync` する。
  Hyprland の設定は影響を受けない。パックはスニペットを足すだけで、`bindings.lua` を所有しない。
- このリポジトリ自体が 2 つのパックになっている: `plugin/`（バーウィジェットを入れる。実体は
  [polidog/omarchy-omasushi](https://github.com/polidog/omarchy-omasushi)）と
  `claude/`（omasushi を動かすためのスキル。[`claude/skills/omasushi`](claude/skills/omasushi) にあり、
  Omarchy のデフォルトエージェント向けにリンクされる）。`omasushi use polidog/omasushi` で両方、
  `omasushi use polidog/omasushi/claude` でスキルだけ入る。
  写して使えるパックは [polidog/omakase](https://github.com/polidog/omakase) にある。

## ライセンス

MIT
