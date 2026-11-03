package resourcecache

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDelegatedHTTPAbortRemainsNormalServerAbort(t *testing.T) {
	m := manager(t, Options{})
	h := m.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) }))
	defer func() {
		if value := recover(); value != http.ErrAbortHandler {
			t.Fatalf("normal HTTP abort changed: %v", value)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/assets/test.js", nil))
	t.Fatal("HTTP abort swallowed")
}
