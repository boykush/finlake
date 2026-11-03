# 毎月の取り込み

前月分のマネーフォワード ME の明細を、MCP で引けるようにする。CSV を R2 に置き、クラスタの `sync` を手で流して
明細（product）にする。

前月分は、遅れて入る明細（カードの確定、銀行の同期）を待って、月初から数日おいて置く。

## 手順

1. マネーフォワード ME の[入出金](https://moneyforward.com/cf)で対象月を開き、CSV をダウンロードする
2. Cloudflare のダッシュボードで、R2 のバケット [`finlake`](https://dash.cloudflare.com/?to=/:account/r2/default/buckets/finlake)
   の `raw/moneyforward/month=YYYY-MM/` に置く。
   ファイル名も文字コード（Shift_JIS）も変えずにそのまま置く。置き方の決まりは README の
   [データレイク](https://github.com/boykush/finlake#データレイク)
3. Argo CD の [`finlake`](https://argocd.boykush.com/applications/argocd/finlake) アプリで、CronJob `sync` の
   メニューから Create Job を選ぶ。Job が Healthy（完了）になれば明細になっている

## 気にしなくてよいもの

- **R2 のプレビューが文字化けする**: CSV が Shift_JIS のままだから。変換のときに UTF-8 に直す
- **CronJob に `Saw a job that the controller did not create or forgot` の event が出る**: 手で作った Job が
  CronJob の管理外として見えて出る Warning。Job の成否とは関係ない

## 失敗したとき

置き間違い（フォルダ名、CSV が2つ、明細の月がフォルダと違う）があると、その月を変換せずに Job が失敗する。
Job のログに月と理由が出るので、R2 の側を直してから手順3をやり直す。
