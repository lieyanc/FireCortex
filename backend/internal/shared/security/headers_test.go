package security

import "testing"

func TestRedactHeadersJSONMasksSensitiveHeaders(t *testing.T) {
	got := RedactHeadersJSON(`{"Authorization":"Bearer secret","X-API-Key":"key","X-Title":"FireCortex"}`)
	want := `{"Authorization":"********","X-API-Key":"********","X-Title":"FireCortex"}`
	if got != want {
		t.Fatalf("unexpected redacted headers: got %s want %s", got, want)
	}
}
