# AGENTS.md

このリポジトリで作業するエージェント共通の指示。Claude Code も Codex も本ファイルを直接読む。

## プロジェクト概要

[README.md](README.md) を参照。

## 言語

- コミットメッセージ: 英語
- コミュニケーション・ドキュメント・コメント: 日本語

## Git

トランクベース開発。worktree を使う場合を除き、main に直接コミットする。

この repo が出すのはイメージ（`ghcr.io/boykush/finlake`）まで。k8s のマニフェスト・Secret・公開ホスト名は
[boykush/infrastructure-as-code](https://github.com/boykush/infrastructure-as-code) が持つので、ここには置かない。
`mcp` の環境変数や引数を変えたら、iac 側の manifest も合わせて変える必要がある（README の「デプロイ」）。

## mise

ツールの版と検査は `.mise.toml` に置く。版は `latest` を使わず具体的に書く。

エージェントのシェルでは `mise exec` 経由で実行する。

```bash
mise exec -- go test ./...
mise run check   # fmt / vet / lint / tidy / test。CI と同じもの
```

push の前に `mise run check` を通す。

## データ

- 明細の変換は `internal/transform/transactions.sql`（DuckDB の SQL）にある。product の列を変えたら MCP の
  クエリ（`internal/mcpserver/query.go`）とテストのサンプル CSV も合わせる
- 実データ（マネーフォワードの CSV、Parquet）や Cookie・トークンを commit しない。手元のデータレイクは
  `.data/`（gitignore 済み）
- `mise run pull` はマネーフォワードから取って本番の R2 に書く。頼まれたときだけ流し、試すときは `.data/` に
  向く `ingest` / `transform` を使う
