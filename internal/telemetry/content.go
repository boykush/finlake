package telemetry

import (
	"strings"

	"github.com/caarlos0/env/v11"
)

// contentEnv は、呼び出し側が送ったものを span に残すための opt-in。GenAI の semantic conventions は
// そうした中身を既定で外し、求め方の例としてこの変数を挙げている。
type contentEnv struct {
	Capture boolean `env:"OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT"`
}

// boolean は OpenTelemetry が環境変数から読む Boolean。大文字小文字を問わず "true" なら true、それ以外は
// false。bool のフィールドだと strconv.ParseBool を通り、"1" を true と読み "yes" で失敗する。
type boolean bool

func (b *boolean) UnmarshalText(text []byte) error {
	*b = boolean(strings.EqualFold(string(text), "true"))
	return nil
}

// CapturesContent は、呼び出し側が送ったものを span に残すよう環境変数が求めているかを返す。
func CapturesContent() bool {
	return capturesContent(env.Options{})
}

// capturesContent は opts が指す環境（指定が無ければプロセスの環境）から contentEnv を読む。テストが map を
// 渡してプロセスの環境に触れずに済むように分けてある。
func capturesContent(opts env.Options) bool {
	content, err := env.ParseAsWithOptions[contentEnv](opts)
	// 読めない opt-in は与えられていないものとする。
	return err == nil && bool(content.Capture)
}
