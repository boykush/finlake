// Package moneyforward はマネーフォワード ME が出す入出金明細の CSV を読む。
package moneyforward

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"slices"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/transform"

	"github.com/boykush/finlake/internal/month"
)

// Header はマネーフォワード ME が出す CSV の列。この並びでなければ取り込まない。
var Header = []string{"計算対象", "日付", "内容", "金額（円）", "保有金融機関", "大項目", "中項目", "メモ", "振替", "ID"}

// Normalize は raw 層の CSV を UTF-8 にし、列が期待どおりかを確かめる。raw 層には以前の取り込みで
// UTF-8 にしたものと、ブラウザで落としたまま（Shift_JIS）のものが混ざる。
func Normalize(raw []byte) ([]byte, error) {
	body := raw
	if !utf8.Valid(raw) {
		var err error
		if body, err = decodeShiftJIS(raw); err != nil {
			return nil, err
		}
	}
	if err := checkHeader(body); err != nil {
		return nil, err
	}
	return body, nil
}

func decodeShiftJIS(raw []byte) ([]byte, error) {
	body, err := io.ReadAll(transform.NewReader(bytes.NewReader(raw), japanese.ShiftJIS.NewDecoder()))
	if err != nil {
		return nil, fmt.Errorf("decode shift_jis: %w", err)
	}
	return body, nil
}

func checkHeader(body []byte) error {
	header, err := csv.NewReader(bytes.NewReader(body)).Read()
	if err != nil {
		return fmt.Errorf("read csv header: %w", err)
	}
	if !slices.Equal(header, Header) {
		return fmt.Errorf("unexpected csv header %q", header)
	}
	return nil
}

// Months は CSV（UTF-8）の明細が属する月を古い順に返す。明細が無ければ空。
func Months(body []byte) ([]month.Month, error) {
	rows, err := csv.NewReader(bytes.NewReader(body)).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read csv: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil
	}
	date := slices.Index(Header, "日付")
	var months []month.Month
	for _, row := range rows[1:] {
		t, err := time.Parse("2006/01/02", row[date])
		if err != nil {
			return nil, fmt.Errorf("invalid date %q", row[date])
		}
		m := month.Month{Year: t.Year(), Month: t.Month()}
		if !slices.Contains(months, m) {
			months = append(months, m)
		}
	}
	slices.SortFunc(months, month.Month.Compare)
	return months, nil
}
