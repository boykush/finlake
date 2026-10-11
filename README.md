# Finlake

お金まわりのデータ基盤。マネーフォワード ME の入出金明細を DuckDB でデータレイクに取り込み、整えた明細を
MCP サーバーで配る。主な利用先は [boykush/life](https://github.com/boykush/life) のエージェント。

```
マネーフォワード ME ──(ingest)──▶ raw 層 ──(transform)──▶ product 層 ──(mcp)──▶ life のエージェント
                     CSV(Shift_JIS)    CSV(UTF-8)          Parquet          Cloudflare Access
                                  └──── Cloudflare R2（DuckDB httpfs で読み書き）────┘
```

API サーバーとフロントエンドは、必要になった時点で product 層の上に足す。

## 構成

1つの Go バイナリをサブコマンドで使い分ける。

| サブコマンド | 役割 |
| --- | --- |
| `finlake ingest --month <月>` | マネーフォワード ME から対象月の CSV を取得し、raw 層へ置く |
| `finlake backfill --since <月> [--until <月>]` | 過去月をまとめて取得し、product の明細まで作る。明細の無い月は飛ばす |
| `finlake transform --month <月>` | raw 層の CSV を product の明細（Parquet）に変換する |
| `finlake sync [--dry-run]` | product が無いか、product より後に raw 層へ CSV が置かれた月をすべて変換する。無ければ何もしない |
| `finlake mcp --addr <host:port>` | product の明細を MCP（Streamable HTTP、`/mcp`）で配る |

`<月>` は `YYYY-MM`・`current`（`--month` の既定）・`previous`（`--until` の既定）。`current` / `previous` は JST で数える。同じ月を
流し直すと上書きするので、月の途中で何度流してもよい。

```
cmd/finlake/            エントリポイント（サブコマンド）
internal/moneyforward/  マネーフォワード ME の CSV ダウンロード（Shift_JIS → UTF-8、列の検証）
internal/lake/          DuckDB の接続とデータレイクのパス
internal/ingest/        raw 層への取り込み
internal/transform/     raw → product の変換（transactions.sql）
internal/mcpserver/     MCP サーバー（tool）
```

### データレイク

ルート（`FINLAKE_LAKE_ROOT`）は `s3://<bucket>[/prefix]`（S3 互換。本番は Cloudflare R2）かローカルのディレクトリ。
どちらも同じ配置になる。

```
raw/moneyforward/month=YYYY-MM/<名前>.csv           マネーフォワードの CSV（月に1つ）
product/transactions/month=YYYY-MM/data.parquet     明細
```

raw 層の CSV は `ingest` / `backfill` が置く（`transactions.csv`、UTF-8）ほか、マネーフォワード ME の画面から
落としたファイルをそのまま（名前も Shift_JIS も変えずに）手で置いてもよい。変換は、月のフォルダに CSV が
ちょうど1つあり、中の明細がすべてその月のものであることを確かめてから product を作る。

product の明細の列:

| 列 | 型 | 中身 |
| --- | --- | --- |
| `id` | VARCHAR | マネーフォワードの ID。重複は1件に寄せる |
| `date` | DATE | 日付 |
| `description` | VARCHAR | 内容 |
| `amount` | BIGINT | 金額（円）。収入が正、支出が負 |
| `account` / `category` / `subcategory` / `memo` | VARCHAR | 保有金融機関 / 大項目 / 中項目 / メモ |
| `is_target` / `is_transfer` | BOOLEAN | 計算対象 / 振替 |
| `kind` | VARCHAR | `income` / `expense` / `transfer`（振替）/ `excluded`（計算対象外）。収支に入るのは前の2つ |
| `month` | VARCHAR | パーティション（`YYYY-MM`）。読むときに hive partitioning で付く |

### MCP の tool

| tool | 返すもの |
| --- | --- |
| `list_months` | 取り込み済みの月と、月ごとの収入・支出・収支 |
| `get_monthly_summary` | 対象月の収支と大項目ごとの内訳 |
| `list_transactions` | 対象月の明細。区分・大項目・キーワードで絞り込める |
| `get_category_trend` | 大項目（任意で中項目）の月ごとの推移 |

サーバー自身は認証を持たない。家計は非公開の情報なので、公開するときは必ず前段で認証する——本番は
Cloudflare Access が担う（「デプロイ」）。

### トレース

`mcp` は OpenTelemetry の標準の環境変数でトレースを有効にする。`OTEL_EXPORTER_OTLP_ENDPOINT`（または
`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`）があれば、MCP のリクエストごとの span を OTLP/HTTP で送る。無ければ何も
送らない。span の名前と属性は OpenTelemetry の
[MCP の semantic conventions](https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/mcp.md)
に従い、service 名は `OTEL_SERVICE_NAME` から取る。呼び出し側が `params._meta` で trace context を渡せば、その
trace を続ける。

tool の引数（`gen_ai.tool.call.arguments`）とエラーの文面（status の description）は、
`OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT=true` のときだけ載る。conventions が引数を opt-in にしていて、
エラーの文面はその引数を引用するため。キーワードなど家計の中身が引数に入るので、普段は付けない。

[boykush/adr](https://github.com/boykush/adr) の MCP サーバー（`adi mcp`）と同じ作り。

## 環境変数

| 変数 | 使うサブコマンド | 中身 |
| --- | --- | --- |
| `FINLAKE_LAKE_ROOT` | すべて | データレイクのルート（`s3://finlake` など） |
| `FINLAKE_S3_ENDPOINT` | すべて（s3 のとき） | R2 なら `<account_id>.r2.cloudflarestorage.com` |
| `FINLAKE_S3_REGION` | すべて（s3 のとき） | R2 なら `auto` |
| `FINLAKE_S3_URL_STYLE` | すべて（s3 のとき） | `vhost`（既定）か `path`。R2 は `path` |
| `FINLAKE_S3_ACCESS_KEY_ID` / `FINLAKE_S3_SECRET_ACCESS_KEY` | すべて（s3 のとき） | R2 の API トークンのアクセスキー（secret） |
| `MONEYFORWARD_COOKIE` | `ingest` / `backfill` | `_moneybook_session=<値>`（secret。取り方は「取り込み」） |
| `FINLAKE_DUCKDB_EXTENSION_DIRECTORY` | すべて | DuckDB 拡張の置き場。イメージが設定済みなので普段は触らない |
| `OTEL_EXPORTER_OTLP_ENDPOINT` / `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` | `mcp` | トレースの送り先（OTLP/HTTP）。無ければ送らない（「トレース」） |
| `OTEL_SERVICE_NAME` | `mcp` | span の service 名 |
| `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT` | `mcp` | `true` で tool の引数とエラーの文面を span に載せる |

## 取り込み

毎月の取り込みは、マネーフォワード ME の「家計簿」→「入出金」で対象月を開いて CSV をダウンロードし、
Cloudflare のダッシュボードから R2 のバケット `finlake` の `raw/moneyforward/month=YYYY-MM/` に置く
（置き方の決まりは「データレイク」）。product にするのはクラスタの `sync` で、手元では何も流さない。
前月分は、遅れて入る明細（カードの確定、銀行の同期）を待って、月初から数日おいて置く。

過去月をまとめて入れる・入れ直すときだけ、手元から Cookie で取得して R2 に書く。

```bash
mise run pull:backfill 2024-01    # 2024-01 から前月まで
mise run pull 2026-09             # 1か月だけ
```

途中でセッションが切れても取り込み済みの月は残るので、Cookie を入れ替えて止まった月から流し直す。

- **Cookie**: `.mise.local.toml` の `[env]` に `MONEYFORWARD_COOKIE` で置く（値の形は「環境変数」の表）。開発者
  ツールの Application → Cookies → `https://moneyforward.com` から `_moneybook_session` の値を写す。HttpOnly
  なので `document.cookie` には出ない。セッションが切れると `moneyforward session expired` で落ちるので、
  そのときに入れ替える
- **R2**: 接続情報は手元に置かない。クラスタの `sync` が使うのと同じ Parameter Store の値を流すたびに引くので、
  先に `mise exec -- aws login` でログインしておく。値を入れるのは iac の側（iac の README の「finlake」）

## デプロイ

この repo が出すのは `ghcr.io/boykush/finlake` のイメージまで（`.github/workflows/image.yml`）。クラスタで
動かすのは `sync` と `mcp` で、k8s のマニフェスト・Secret・公開ホスト名・digest の追従は
[boykush/infrastructure-as-code](https://github.com/boykush/infrastructure-as-code) が持つ。

iac 側で要るもの:

- `sync` の CronJob。いつ流すかと手での流し方は iac が持つ
- `mcp` の Deployment / Service（port 8080、probe は `/healthz`）と、Cloudflare Tunnel のホスト名
- Secret: R2 のトークン（Object Read & Write）。`sync` と `mcp` で同じものを使う
- `mcp` の認証: 公開ホスト名に Cloudflare Access を掛け、クラスタ内からは経路上の Pod 以外を止める

公開エンドポイントは `https://finlake-mcp.boykush.com/mcp`。利用側（life）はこれを自分の `apm.yml` の
`dependencies.mcp` に宣言していて、クライアントの設定はそこから生成する。初回に `/mcp` から Access の
ログインを通す。

## 開発

[mise](https://mise.jdx.dev/) でツールとタスクを揃える。DuckDB を cgo でリンクするので C コンパイラが要る。

```bash
mise install
mise run check                          # fmt / vet / lint / tidy / test（CI と同じ）

# 手元のパイプライン（データレイクは .data/）
mise run ingest -- --month 2026-09      # .mise.local.toml の [env] に MONEYFORWARD_COOKIE を置く
mise run transform -- --month 2026-09
mise run sync -- --dry-run              # 変換が要る月を見るだけ
mise run mcp                            # 127.0.0.1:8080
```
