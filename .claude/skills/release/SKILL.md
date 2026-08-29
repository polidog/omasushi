---
name: release
description: omasushi の新バージョンをリリースする。manifest.json を bump してコミットし、タグを push して GitHub Actions の release ワークフロー（tar.gz + checksums + GitHub Release）を起動する。トリガー: release, リリース, タグを打つ, バージョンを上げる
---

# release

`/release <version>` （例: `/release v0.2.0` または `/release 0.2.0`）。
バージョン未指定なら現在の `manifest.json` と既存タグを表示し、patch / minor / major のどれにするか AskUserQuestion で確認する。

## 前提

- リリースは `.github/workflows/release.yml` が行う（`v*` タグの push で起動）。
- ワークフローは `manifest.json` の `version` がタグと一致しないと失敗する。**必ず先に bump してコミットしてからタグを打つ。**
- タグは `main` のコミットに打つ。

## 手順

1. **状態確認**
   ```sh
   git status --porcelain           # 未コミット変更があれば中断して報告
   git branch --show-current        # main 以外なら中断（worktree 内でも同様）
   git fetch origin && git status -sb   # origin/main と ahead/behind がないこと
   jq -r .version manifest.json
   git tag --sort=-v:refname | head -5
   ```
   - 未コミット変更あり / main 以外 / origin と乖離 → 状況を伝えて止まる。
   - 指定バージョンは `vX.Y.Z` に正規化し、既存タグと重複していたら中断。
   - 新バージョンが最新タグより大きいことを確認。

2. **CI が緑か確認**
   ```sh
   gh run list --branch main --workflow ci.yml --limit 1
   ```
   失敗していたら報告して止まる。

3. **manifest.json を bump**（`v` なしの値）
   ```sh
   jq --arg v "X.Y.Z" '.version = $v' manifest.json > manifest.json.tmp && mv manifest.json.tmp manifest.json
   go test ./...
   git add manifest.json
   git commit -m "Release vX.Y.Z"
   ```
   既に一致していればこのステップはスキップ。

4. **タグを打って push**
   ```sh
   git push origin main
   git tag -a vX.Y.Z -m "vX.Y.Z"
   git push origin vX.Y.Z
   ```

5. **ワークフローを見届ける**
   ```sh
   gh run list --workflow release.yml --limit 1
   gh run watch <run-id> --exit-status
   gh release view vX.Y.Z
   ```
   失敗したら `gh run view <run-id> --log-failed` で原因を報告。
   ワークフローがタグ作成前に落ちた場合（manifest 不一致など）は、修正後に `git tag -d vX.Y.Z && git push origin :refs/tags/vX.Y.Z` でタグを消してからやり直す。

## 完了報告

- リリース URL（`gh release view vX.Y.Z --json url -q .url`）
- 添付物: `omasushi-vX.Y.Z-linux-{amd64,arm64}.tar.gz`, `checksums.txt`
- README のインストールワンライナーが新バージョンを指すことを一言添える
