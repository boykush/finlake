// Package moneyforward はマネーフォワード ME から入出金明細の CSV を取得する。
//
// マネーフォワード ME には公開 API が無いので、ブラウザと同じ CSV ダウンロード
// （/cf/csv）をログイン済みセッションの Cookie で叩く。CSV のダウンロードは
// プレミアム会員の機能。
package moneyforward

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"time"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"

	"github.com/boykush/finlake/internal/month"
)

// DefaultBaseURL はマネーフォワード ME の URL。
const DefaultBaseURL = "https://moneyforward.com"

// Header はマネーフォワード ME が出す CSV の列。この並びでなければ取り込まない。
var Header = []string{"計算対象", "日付", "内容", "金額（円）", "保有金融機関", "大項目", "中項目", "メモ", "振替", "ID"}

// ErrSessionExpired は Cookie のセッションが切れていてログイン画面に戻されたことを表す。
var ErrSessionExpired = errors.New("moneyforward session expired: refresh MONEYFORWARD_COOKIE")

// Client は CSV を取得するクライアント。
type Client struct {
	BaseURL string
	// Cookie はログイン済みブラウザの Cookie ヘッダをそのまま入れる。
	Cookie string
	HTTP   *http.Client
}

// NewClient は Cookie を持ったクライアントを返す。
func NewClient(cookie string) *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		Cookie:  cookie,
		HTTP: &http.Client{
			Timeout: 60 * time.Second,
			// セッション切れはログイン画面へのリダイレクトで返ってくる。追わずに検出する。
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// DownloadCSV は対象月の明細を UTF-8 の CSV で返す。
func (c *Client) DownloadCSV(ctx context.Context, m month.Month) ([]byte, error) {
	if c.Cookie == "" {
		return nil, errors.New("moneyforward cookie is empty")
	}

	q := url.Values{}
	q.Set("from", fmt.Sprintf("%04d/%02d/01", m.Year, int(m.Month)))
	q.Set("month", fmt.Sprint(int(m.Month)))
	q.Set("year", fmt.Sprint(m.Year))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/cf/csv?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", c.Cookie)
	req.Header.Set("User-Agent", "finlake (+https://github.com/boykush/finlake)")

	// 宛先は BaseURL（既定はマネーフォワード ME 固定。差し替えるのはテストだけ）。
	resp, err := c.HTTP.Do(req) //nolint:gosec // G704: see above
	if err != nil {
		return nil, fmt.Errorf("download csv: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return nil, ErrSessionExpired
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download csv: unexpected status %s", resp.Status)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}
	return Decode(raw)
}

// Decode は Shift_JIS の CSV を UTF-8 にし、列が期待どおりかを確かめる。
// セッション切れで HTML が返ったときもここで弾かれる。
func Decode(raw []byte) ([]byte, error) {
	utf8, err := io.ReadAll(transform.NewReader(bytes.NewReader(raw), japanese.ShiftJIS.NewDecoder()))
	if err != nil {
		return nil, fmt.Errorf("decode shift_jis: %w", err)
	}

	header, err := csv.NewReader(bytes.NewReader(utf8)).Read()
	if err != nil {
		return nil, fmt.Errorf("read csv header: %w", err)
	}
	if !slices.Equal(header, Header) {
		return nil, fmt.Errorf("unexpected csv header %q: %w", header, ErrSessionExpired)
	}
	return utf8, nil
}
