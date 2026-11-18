package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSponsorEndpointsAreNotRegistered(t *testing.T) {
	engine := newTestEngine(t)
	for _, path := range []string{"/sponsors", "/sponsors/logo/acme.png"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		engine.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404", path, w.Code)
		}
	}
}
