package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Revise0592/McPanel/internal/auth"
	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/store"
)

func TestStatusPage(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// A panel with a login, so anything behind it would redirect.
	st.UpdateSettings(func(s *store.Settings) { s.Username, s.PasswordHash = "admin", auth.HashPassword("password1") })
	mgr := mc.NewManager(dir, st)
	a, err := New(st, mgr, "", false)
	if err != nil {
		t.Fatal(err)
	}
	h := a.Handler()
	token := strings.Repeat("ab", 16)
	sv, _ := st.AddServer(store.Server{Name: "Survival", Type: store.TypePaper, Version: "1.21.4", Installed: true, StatusToken: token, SleepAfterMins: 15})
	inst, _ := mgr.Get(sv.ID)
	os.MkdirAll(inst.Dir(), 0o755)
	os.WriteFile(filepath.Join(inst.Dir(), "server.properties"), []byte("motd=§aWelcome §lhome\nmax-players=12\nserver-port=25570\n"), 0o644)

	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "http://panel.lan:8080"+path, nil))
		return w
	}

	w := get("/s/" + token)
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, body)
	}
	for _, want := range []string{"Survival", "Welcome home", "panel.lan:25570", "Offline"} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q", want)
		}
	}
	for _, leak := range []string{sv.ID, "/servers/", "Sign out"} {
		if strings.Contains(body, leak) {
			t.Errorf("public page shows %q", leak)
		}
	}
	if w := get("/s/" + token + "/live"); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "status-card") {
		t.Errorf("live refresh: %d", w.Code)
	}
	for _, bad := range []string{"/s/" + strings.Repeat("cd", 16), "/s/ab", "/s/" + token + "x"} {
		if w := get(bad); w.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", bad, w.Code)
		}
	}
	// Only heads of players on the server are served.
	if w := get("/s/" + token + "/heads/Notch"); w.Code != http.StatusNotFound {
		t.Errorf("head of someone not online: status %d", w.Code)
	}
	// The panel's own pages still need signing in.
	if w := get("/servers/" + sv.ID); w.Code != http.StatusSeeOther {
		t.Errorf("server page without signing in: status %d", w.Code)
	}

	// Turned off, the link stops working.
	st.UpdateServer(sv.ID, func(s *store.Server) { s.StatusToken = "" })
	if w := get("/s/" + token); w.Code != http.StatusNotFound {
		t.Errorf("after turning off: status %d", w.Code)
	}
}
