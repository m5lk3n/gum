package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireToken(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := RequireToken("s3cret", ok)

	cases := map[string]int{
		"":              http.StatusUnauthorized,
		"Bearer wrong":  http.StatusUnauthorized,
		"s3cret":        http.StatusUnauthorized,
		"Bearer s3cret": http.StatusNoContent,
	}
	for header, want := range cases {
		req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("Authorization %q: got %d, want %d", header, rec.Code, want)
		}
	}

	rec := httptest.NewRecorder()
	RequireToken("", ok).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/mcp", nil))
	if rec.Code != http.StatusNoContent {
		t.Errorf("empty token should disable auth, got %d", rec.Code)
	}
}
