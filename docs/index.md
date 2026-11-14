# Protocol Documentation
<a name="top"></a>

## Table of Contents

- [finlake/v1/ledger.proto](#finlake_v1_ledger-proto)
    - [AggregateRow](#finlake-v1-AggregateRow)
    - [AggregateTransactionsRequest](#finlake-v1-AggregateTransactionsRequest)
    - [AggregateTransactionsResponse](#finlake-v1-AggregateTransactionsResponse)
    - [CategoryTotal](#finlake-v1-CategoryTotal)
    - [GetMonthlySummaryRequest](#finlake-v1-GetMonthlySummaryRequest)
    - [GetMonthlySummaryResponse](#finlake-v1-GetMonthlySummaryResponse)
    - [ListMonthsRequest](#finlake-v1-ListMonthsRequest)
    - [ListMonthsResponse](#finlake-v1-ListMonthsResponse)
    - [ListTransactionsRequest](#finlake-v1-ListTransactionsRequest)
    - [ListTransactionsResponse](#finlake-v1-ListTransactionsResponse)
    - [MonthTotal](#finlake-v1-MonthTotal)
    - [PeriodStats](#finlake-v1-PeriodStats)
    - [Transaction](#finlake-v1-Transaction)
  
    - [LedgerService](#finlake-v1-LedgerService)
  
- [Scalar Value Types](#scalar-value-types)



<a name="finlake_v1_ledger-proto"></a>
<p align="right"><a href="#top">Top</a></p>

## finlake/v1/ledger.proto



<a name="finlake-v1-AggregateRow"></a>

### AggregateRow
切り口の値の組み合わせ1つぶんの集計。group_by に無い切り口は入らない。


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| category | [string](#string) | optional | 大項目。空の明細は「未分類」 |
| subcategory | [string](#string) | optional | 中項目。空の明細は「未分類」 |
| account | [string](#string) | optional | 保有金融機関。空の明細は「未設定」 |
| amounts | [int64](#int64) | repeated | 列ごとの金額（円、正の値）。periods と同じ順で、明細の無い期間は 0 |
| total | [int64](#int64) |  | 期間全体の合計（円） |
| count | [int64](#int64) |  | 期間全体の件数 |
| stats | [PeriodStats](#finlake-v1-PeriodStats) | optional | 列ごとの金額の中央値・最小・最大。明細の無い期間も 0 として数える。periods があるときだけ |






<a name="finlake-v1-AggregateTransactionsRequest"></a>

### AggregateTransactionsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| group_by | [string](#string) | repeated | 切り口。month / date / category / subcategory / account から選ぶ（month と date は片方だけ）。 例: [month, category] は大項目ごとの月の推移、[category, subcategory] は期間の中項目の内訳 |
| from | [string](#string) | optional | 開始（YYYY-MM か YYYY-MM-DD、含む）。省くと終了から数えて12か月 |
| to | [string](#string) | optional | 終了（YYYY-MM か YYYY-MM-DD、含む）。省くと取り込み済みの最後の月の末日。 group_by に month があれば 24 か月まで、date があれば 400 日まで |
| kind | [string](#string) | optional | expense（既定）か income |
| categories | [string](#string) | repeated | 絞り込む大項目（完全一致）。省くとすべて |
| exclude_categories | [string](#string) | repeated | 除く大項目（完全一致）。固定費を外して見るときなど |
| accounts | [string](#string) | repeated | 絞り込む保有金融機関（完全一致） |
| keyword | [string](#string) | optional | 内容とメモの部分一致 |
| limit | [int32](#int32) | optional | 返す行の上限（既定 100、上限 500）。合計の大きい順に切り、残りは omitted にまとめる |






<a name="finlake-v1-AggregateTransactionsResponse"></a>

### AggregateTransactionsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| periods | [string](#string) | repeated | 表の列。group_by に month か date があるときだけ、古い順に。取り込み済みの月の範囲に限る |
| rows | [AggregateRow](#finlake-v1-AggregateRow) | repeated | 行。期間の合計の大きい順 |
| period_totals | [int64](#int64) | repeated | 列ごとの全体の合計（円、正の値）。periods と同じ順。上限で切った行も含む |
| total | [int64](#int64) |  | 期間全体の合計（円）。上限で切った行も含む |
| count | [int64](#int64) |  | 期間全体の件数。上限で切った行も含む |
| omitted_rows | [int64](#int64) |  | 上限で切った残りの行の数 |
| omitted_total | [int64](#int64) |  | 上限で切った残りの行の合計（円） |






<a name="finlake-v1-CategoryTotal"></a>

### CategoryTotal
大項目ごとの合計。


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| kind | [string](#string) |  | income か expense |
| category | [string](#string) |  | 大項目 |
| amount | [int64](#int64) |  | 合計（円、正の値） |
| count | [int64](#int64) |  | 件数 |






<a name="finlake-v1-GetMonthlySummaryRequest"></a>

### GetMonthlySummaryRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| month | [string](#string) |  | 対象月（YYYY-MM） |






<a name="finlake-v1-GetMonthlySummaryResponse"></a>

### GetMonthlySummaryResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| month | [string](#string) |  | 対象月（YYYY-MM） |
| income | [int64](#int64) |  | 収入の合計（円） |
| expense | [int64](#int64) |  | 支出の合計（円、正の値） |
| balance | [int64](#int64) |  | 収入 - 支出（円） |
| count | [int64](#int64) |  | 収支に入る明細の件数 |
| categories | [CategoryTotal](#finlake-v1-CategoryTotal) | repeated | 大項目ごとの内訳。金額の大きい順 |






<a name="finlake-v1-ListMonthsRequest"></a>

### ListMonthsRequest







<a name="finlake-v1-ListMonthsResponse"></a>

### ListMonthsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| months | [MonthTotal](#finlake-v1-MonthTotal) | repeated |  |






<a name="finlake-v1-ListTransactionsRequest"></a>

### ListTransactionsRequest



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| month | [string](#string) | optional | 対象月（YYYY-MM）。from / to と合わせては使えない |
| kind | [string](#string) | optional | income / expense / transfer / excluded のどれか |
| category | [string](#string) | optional | 大項目（完全一致） |
| keyword | [string](#string) | optional | 内容とメモの部分一致 |
| limit | [int32](#int32) | optional | 最大件数（既定 200、上限 1000） |
| from | [string](#string) | optional | 期間の開始日（YYYY-MM-DD、含む）。month の代わりに to と組で使う |
| to | [string](#string) | optional | 期間の終了日（YYYY-MM-DD、含む） |






<a name="finlake-v1-ListTransactionsResponse"></a>

### ListTransactionsResponse



| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| transactions | [Transaction](#finlake-v1-Transaction) | repeated |  |






<a name="finlake-v1-MonthTotal"></a>

### MonthTotal
月ごとの収支。


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| month | [string](#string) |  | 対象月（YYYY-MM） |
| income | [int64](#int64) |  | 収入の合計（円） |
| expense | [int64](#int64) |  | 支出の合計（円、正の値） |
| balance | [int64](#int64) |  | 収入 - 支出（円） |
| count | [int64](#int64) |  | 収支に入る明細の件数 |






<a name="finlake-v1-PeriodStats"></a>

### PeriodStats
列ごとの金額の散らばり（円）。


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| median | [int64](#int64) |  | 中央値。列の数が偶数なら中央の2つの平均（切り捨て） |
| min | [int64](#int64) |  |  |
| max | [int64](#int64) |  |  |






<a name="finlake-v1-Transaction"></a>

### Transaction
明細1件。


| Field | Type | Label | Description |
| ----- | ---- | ----- | ----------- |
| id | [string](#string) |  |  |
| date | [string](#string) |  | 日付（YYYY-MM-DD） |
| description | [string](#string) | optional | 内容 |
| amount | [int64](#int64) |  | 金額（円）。収入が正、支出が負 |
| kind | [string](#string) |  | income / expense / transfer（振替）/ excluded（計算対象外） |
| category | [string](#string) | optional | 大項目 |
| subcategory | [string](#string) | optional | 中項目 |
| account | [string](#string) | optional | 保有金融機関 |
| memo | [string](#string) | optional | メモ |





 

 

 


<a name="finlake-v1-LedgerService"></a>

### LedgerService
家計（マネーフォワード ME の入出金明細）を引く。
金額は円。明細の amount は収入が正、支出が負。集計の income / expense はどちらも正の値。
振替（transfer）と計算対象外（excluded）は収支に入らない。
まず取り込み済みの月を見てから、月を指定して他を使う。

| Method Name | Request Type | Response Type | Description |
| ----------- | ------------ | ------------- | ------------|
| ListMonths | [ListMonthsRequest](#finlake-v1-ListMonthsRequest) | [ListMonthsResponse](#finlake-v1-ListMonthsResponse) | 取り込み済みの月と、月ごとの収入・支出・収支を新しい順に返す |
| GetMonthlySummary | [GetMonthlySummaryRequest](#finlake-v1-GetMonthlySummaryRequest) | [GetMonthlySummaryResponse](#finlake-v1-GetMonthlySummaryResponse) | 対象月の収入・支出・収支と、大項目ごとの内訳を返す |
| ListTransactions | [ListTransactionsRequest](#finlake-v1-ListTransactionsRequest) | [ListTransactionsResponse](#finlake-v1-ListTransactionsResponse) | 対象月（または期間）の明細を日付順に返す。区分・大項目・キーワード（内容とメモの部分一致）で絞り込める |
| AggregateTransactions | [AggregateTransactionsRequest](#finlake-v1-AggregateTransactionsRequest) | [AggregateTransactionsResponse](#finlake-v1-AggregateTransactionsResponse) | 収入か支出の明細を、選んだ切り口でまとめて返す。何をどう分けて比べたいかが決まっているなら、 明細を読んで足し合わせる代わりにこれを使う。 切り口（group_by）に month か date を入れると、その期間ごとの金額を列にした表になり、行ごとに 期間の中央値・最小・最大が付く。入れなければ、期間全体の合計の行だけを返す。 |

 



## Scalar Value Types

| .proto Type | Notes | C++ | Java | Python | Go | C# | PHP | Ruby |
| ----------- | ----- | --- | ---- | ------ | -- | -- | --- | ---- |
| <a name="double" /> double |  | double | double | float | float64 | double | float | Float |
| <a name="float" /> float |  | float | float | float | float32 | float | float | Float |
| <a name="int32" /> int32 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint32 instead. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="int64" /> int64 | Uses variable-length encoding. Inefficient for encoding negative numbers – if your field is likely to have negative values, use sint64 instead. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="uint32" /> uint32 | Uses variable-length encoding. | uint32 | int | int/long | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="uint64" /> uint64 | Uses variable-length encoding. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum or Fixnum (as required) |
| <a name="sint32" /> sint32 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int32s. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sint64" /> sint64 | Uses variable-length encoding. Signed int value. These more efficiently encode negative numbers than regular int64s. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="fixed32" /> fixed32 | Always four bytes. More efficient than uint32 if values are often greater than 2^28. | uint32 | int | int | uint32 | uint | integer | Bignum or Fixnum (as required) |
| <a name="fixed64" /> fixed64 | Always eight bytes. More efficient than uint64 if values are often greater than 2^56. | uint64 | long | int/long | uint64 | ulong | integer/string | Bignum |
| <a name="sfixed32" /> sfixed32 | Always four bytes. | int32 | int | int | int32 | int | integer | Bignum or Fixnum (as required) |
| <a name="sfixed64" /> sfixed64 | Always eight bytes. | int64 | long | int/long | int64 | long | integer/string | Bignum |
| <a name="bool" /> bool |  | bool | boolean | boolean | bool | bool | boolean | TrueClass/FalseClass |
| <a name="string" /> string | A string must always contain UTF-8 encoded or 7-bit ASCII text. | string | String | str/unicode | string | string | string | String (UTF-8) |
| <a name="bytes" /> bytes | May contain any arbitrary sequence of bytes. | string | ByteString | str | []byte | ByteString | string | String (ASCII-8BIT) |

