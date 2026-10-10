package telemetry

import (
	"testing"

	"github.com/caarlos0/env/v11"
)

// 変数名は contentEnv から取らず、デプロイ側が書く綴りで持つ。テストで宣言を名前に縛るため。
const captureContentEnv = "OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT"

// TestContentIsCapturedOnlyOnTrue は opt-in を OpenTelemetry と同じ読み方で読む。環境は map で代わりを
// 立てるので、テストを流すプロセスの環境は読みも変えもしない。
func TestContentIsCapturedOnlyOnTrue(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{name: "unset", env: map[string]string{}},
		{name: "empty", env: map[string]string{captureContentEnv: ""}},
		{name: "true", env: map[string]string{captureContentEnv: "true"}, want: true},
		{name: "any case", env: map[string]string{captureContentEnv: "TRUE"}, want: true},
		{name: "false", env: map[string]string{captureContentEnv: "false"}},
		// strconv.ParseBool は true と読み、OpenTelemetry は読まないもの。
		{name: "not a boolean", env: map[string]string{captureContentEnv: "1"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := capturesContent(env.Options{Environment: c.env}); got != c.want {
				t.Errorf("capturesContent(%v) = %t, want %t", c.env, got, c.want)
			}
		})
	}
	// 読めない環境からは opt-in が出ない。宣言には読み損なうものが無いので、無い変数を必須にする
	// options で失敗させる。
	if capturesContent(env.Options{Environment: map[string]string{}, RequiredIfNoDef: true}) {
		t.Error("capturesContent = true for an environment that could not be read")
	}
}

// TestContentOptInIsReadFromTheProcess は変数を本当に設定する唯一のテスト。デプロイが設定するのは
// プロセスの環境なので。
func TestContentOptInIsReadFromTheProcess(t *testing.T) {
	t.Setenv(captureContentEnv, "false")
	if CapturesContent() {
		t.Error("CapturesContent() = true with the variable set to false")
	}
	t.Setenv(captureContentEnv, "true")
	if !CapturesContent() {
		t.Error("CapturesContent() = false with the variable set to true")
	}
}
