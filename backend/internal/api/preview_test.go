package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProtectBrowserWrites(t *testing.T) {
	const appOrigin = "http://localhost:3000"
	handler := protectBrowserWrites(appOrigin)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, tc := range []struct {
		origin, site string
		code         int
	}{
		{appOrigin, "same-origin", 204},
		{"", "", 204},
		{"null", "cross-site", 403},
		{"http://127.0.0.1:8081", "cross-site", 403},
		{"", "cross-site", 403},
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/runs", nil)
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Sec-Fetch-Site", tc.site)
		result := httptest.NewRecorder()
		handler.ServeHTTP(result, req)
		if result.Code != tc.code {
			t.Errorf("Origin=%q Site=%q: got %d, want %d", tc.origin, tc.site, result.Code, tc.code)
		}
	}
}
