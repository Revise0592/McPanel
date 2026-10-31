package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/store"
)

// TestPlayerHeadRevalidates checks heads are served so the browser asks
// again each time (a changed skin shows up) but gets a 304 when nothing changed.
func TestPlayerHeadRevalidates(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	mgr := mc.NewManager(dir, st)
	a, err := New(st, mgr, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if mgr.OnJoin == nil {
		t.Error("joins don't refresh player heads")
	}

	get := func(etag string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/heads/-", nil)
		r.SetPathValue("name", "-") // the Bedrock placeholder, so no network
		if etag != "" {
			r.Header.Set("If-None-Match", etag)
		}
		w := httptest.NewRecorder()
		a.playerHead(w, r)
		return w
	}
	first := get("")
	etag := first.Header().Get("ETag")
	if first.Code != http.StatusOK || etag == "" || first.Body.Len() == 0 {
		t.Fatalf("first: code %d, etag %q, %d bytes", first.Code, etag, first.Body.Len())
	}
	if cc := first.Header().Get("Cache-Control"); cc != "private, no-cache" {
		t.Errorf("Cache-Control = %q", cc)
	}
	if again := get(etag); again.Code != http.StatusNotModified {
		t.Errorf("same ETag: code %d, want 304", again.Code)
	}
}
