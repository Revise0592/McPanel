package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Revise0592/McPanel/internal/activity"
	"github.com/Revise0592/McPanel/internal/auth"
	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/store"
)

func newTestApp(t *testing.T) (*App, http.Handler) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateSettings(func(s *store.Settings) {
		s.Username = "admin"
		s.PasswordHash = auth.HashPassword("correct horse")
	})
	a, err := New(st, mc.NewManager(dir, st), "", false)
	if err != nil {
		t.Fatal(err)
	}
	return a, a.Handler()
}

func call(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func pair(t *testing.T, a *App, h http.Handler) string {
	t.Helper()
	code, _ := a.pairing.newCode()
	// Typed by hand, in lower case with the dash.
	w := call(h, "POST", "/api/v1/pair", "", `{"code":"`+strings.ToLower(groupCode(code))+`","name":"Pixel 9"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("pair: %d %s", w.Code, w.Body)
	}
	var resp struct {
		Token  string `json:"token"`
		Device struct{ Name string }
		Panel  struct{ Name string }
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !strings.HasPrefix(resp.Token, "mcp_") || resp.Device.Name != "Pixel 9" || resp.Panel.Name != "McPanel" {
		t.Fatalf("pair response: %s", w.Body)
	}
	return resp.Token
}

func TestAPIPairing(t *testing.T) {
	a, h := newTestApp(t)

	if w := call(h, "POST", "/api/v1/pair", "", `{"code":"AAAA-AAAA"}`); w.Code != http.StatusUnauthorized {
		t.Errorf("pair with no code waiting: %d", w.Code)
	}
	token := pair(t, a, h)

	// The code works once.
	if _, _, ok := a.pairing.current(); ok {
		t.Error("pairing code still waiting after use")
	}
	devs := a.Store.Devices()
	if len(devs) != 1 || devs[0].TokenHash == token || devs[0].TokenHash != store.HashToken(token) {
		t.Fatalf("stored devices: %+v", devs)
	}

	if w := call(h, "GET", "/api/v1/me", token, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Pixel 9") {
		t.Errorf("me: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/api/v1/me", "mcp_wrong", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("me with a wrong token: %d", w.Code)
	}
	if w := call(h, "GET", "/api/v1/nothing", token, ""); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "error") {
		t.Errorf("unknown address: %d %s", w.Code, w.Body)
	}

	if w := call(h, "POST", "/api/v1/unpair", token, ""); w.Code != http.StatusNoContent {
		t.Errorf("unpair: %d", w.Code)
	}
	if w := call(h, "GET", "/api/v1/me", token, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("me after unpairing: %d", w.Code)
	}
}

func TestAPIPairingExpiresAndLocksOut(t *testing.T) {
	a, h := newTestApp(t)
	code, _ := a.pairing.newCode()
	for range 10 {
		call(h, "POST", "/api/v1/pair", "", `{"code":"WRONG123"}`)
	}
	if w := call(h, "POST", "/api/v1/pair", "", `{"code":"`+code+`"}`); w.Code != http.StatusTooManyRequests {
		t.Errorf("right code after many wrong ones: %d", w.Code)
	}
	a.pairing.cancel()
	if a.pairing.use(code) {
		t.Error("cancelled code still works")
	}
}

func TestAPIIgnoresSessionCookie(t *testing.T) {
	_, h := newTestApp(t)
	// Without a token the API answers 401 in JSON, never a sign-in redirect.
	w := call(h, "GET", "/api/v1/servers", "", "")
	if w.Code != http.StatusUnauthorized || w.Header().Get("Content-Type") != "application/json" {
		t.Errorf("no token: %d %s", w.Code, w.Header().Get("Content-Type"))
	}
}

func TestAPIServers(t *testing.T) {
	a, h := newTestApp(t)
	token := pair(t, a, h)
	sv, err := a.Store.AddServer(store.Server{Name: "Survival", Type: store.TypePaper, Version: "1.21.4", MemoryMB: 4096})
	if err != nil {
		t.Fatal(err)
	}
	a.Mgr.Activity().Add(sv.ID, activity.Event{Kind: activity.Crash, Text: "Server exited"})

	w := call(h, "GET", "/api/v1/servers", token, "")
	var list struct {
		Servers []apiServerJSON `json:"servers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.Servers) != 1 {
		t.Fatalf("servers: %d %s", w.Code, w.Body)
	}
	if s := list.Servers[0]; s.ID != sv.ID || s.State != mc.Stopped || s.Label != "Offline" || s.Players == nil || s.MemoryLimitMB != 4096 {
		t.Errorf("server: %+v", s)
	}

	if w := call(h, "GET", "/api/v1/servers/"+sv.ID, token, ""); w.Code != 200 {
		t.Errorf("one server: %d", w.Code)
	}
	if w := call(h, "GET", "/api/v1/servers/missing", token, ""); w.Code != http.StatusNotFound {
		t.Errorf("missing server: %d", w.Code)
	}
	if w := call(h, "POST", "/api/v1/servers/"+sv.ID+"/command", token, `{"command":"/say hi"}`); w.Code != http.StatusConflict {
		t.Errorf("command to a stopped server: %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/api/v1/servers/"+sv.ID+"/power/start", token, ""); w.Code != http.StatusConflict {
		t.Errorf("starting a server that isn't installed: %d %s", w.Code, w.Body)
	}
	if w := call(h, "POST", "/api/v1/servers/"+sv.ID+"/power/explode", token, ""); w.Code != http.StatusNotFound {
		t.Errorf("unknown power action: %d", w.Code)
	}
	if w := call(h, "GET", "/api/v1/servers/"+sv.ID+"/usage?range=day", token, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"samples":[]`) {
		t.Errorf("usage: %d %s", w.Code, w.Body)
	}

	w = call(h, "GET", "/api/v1/activity", token, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"serverName":"Survival"`) || !strings.Contains(w.Body.String(), `"kind":"crash"`) {
		t.Errorf("activity: %d %s", w.Code, w.Body)
	}
	if w := call(h, "GET", "/api/v1/activity?since="+url.QueryEscape("2999-01-01T00:00:00Z"), token, ""); !strings.Contains(w.Body.String(), `"events":[]`) {
		t.Errorf("activity since the future: %s", w.Body)
	}
}

func TestMobileSettingsPage(t *testing.T) {
	a, _ := newTestApp(t)
	a.Store.AddDevice("Pixel 9")
	render := func() string {
		w := httptest.NewRecorder()
		a.mobilePage(w, httptest.NewRequest("GET", "/settings/mobile", nil))
		if w.Code != 200 {
			t.Fatalf("status %d: %s", w.Code, w.Body)
		}
		return w.Body.String()
	}
	if b := render(); !strings.Contains(b, "Pixel 9") || !strings.Contains(b, "Pair a phone") {
		t.Errorf("page without a code: %s", b)
	}
	code, _ := a.pairing.newCode()
	if b := render(); !strings.Contains(b, groupCode(code)) || !strings.Contains(b, "data:image/png;base64,") {
		t.Errorf("page with a code doesn't show it")
	}
}

func TestPasswordChangeUnpairsDevices(t *testing.T) {
	a, h := newTestApp(t)
	token := pair(t, a, h)
	form := url.Values{"current": {"correct horse"}, "password": {"battery staple"}, "confirm": {"battery staple"}}
	r := httptest.NewRequest("POST", "/settings/password", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	a.changePassword(httptest.NewRecorder(), r)
	if w := call(h, "GET", "/api/v1/me", token, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("token still works after a password change: %d", w.Code)
	}
}
