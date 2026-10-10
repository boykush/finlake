package lake

import "testing"

func TestEndpointHost(t *testing.T) {
	const want = "account.r2.cloudflarestorage.com"
	for _, in := range []string{want, "https://" + want, "https://" + want + "/", "http://" + want} {
		if got := endpointHost(in); got != want {
			t.Errorf("endpointHost(%q) = %q, want %q", in, got, want)
		}
	}
	if got := endpointHost(""); got != "" {
		t.Errorf("endpointHost(\"\") = %q", got)
	}
}
