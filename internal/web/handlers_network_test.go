package web

import (
	"archive/zip"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/store"
	"github.com/Revise0592/McPanel/internal/velocity"
)

const testPaperGlobal = "_version: 29\nproxies:\n  bungee-cord:\n    online-mode: true\n  velocity:\n    enabled: false\n    online-mode: true\n    secret: ''\n"

func TestLinkServersToProxy(t *testing.T) {
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
	mkdir := func(sv store.Server) string {
		inst, _ := mgr.Get(sv.ID)
		os.MkdirAll(filepath.Join(inst.Dir(), "config"), 0o755)
		return inst.Dir()
	}

	proxy, _ := st.AddServer(store.Server{Name: "Network", Type: store.TypeVelocity, Installed: true})
	pdir := mkdir(proxy)
	jar := filepath.Join(pdir, "velocity.jar")
	f, _ := os.Create(jar)
	z := zip.NewWriter(f)
	zw, _ := z.Create("default-velocity.toml")
	zw.Write([]byte("bind = \"0.0.0.0:25565\"\nmotd = \"x\"\n\n[servers]\nlobby = \"127.0.0.1:30066\"\ntry = [\"lobby\"]\n\n[forced-hosts]\n\"a.example.com\" = [\"lobby\"]\n"))
	z.Close()
	f.Close()
	if err := velocity.Prepare(pdir, jar, 25577, "Network"); err != nil {
		t.Fatal(err)
	}

	survival, _ := st.AddServer(store.Server{Name: "Survival World", Type: store.TypePaper, Installed: true})
	sdir := mkdir(survival)
	os.WriteFile(filepath.Join(sdir, "server.properties"), []byte("server-port=25565\nonline-mode=true\n"), 0o644)
	os.WriteFile(filepath.Join(sdir, velocity.PaperConfig), []byte(testPaperGlobal), 0o644)

	creative, _ := st.AddServer(store.Server{Name: "Creative", Type: store.TypePurpur, Installed: true})
	cdir := mkdir(creative)
	os.WriteFile(filepath.Join(cdir, "server.properties"), []byte("server-port=25566\nonline-mode=true\n"), 0o644)
	os.WriteFile(filepath.Join(cdir, velocity.PaperConfig), []byte(testPaperGlobal), 0o644)

	fresh, _ := st.AddServer(store.Server{Name: "Fresh", Type: store.TypeFolia, Installed: true})
	fdir := mkdir(fresh)
	os.WriteFile(filepath.Join(fdir, "server.properties"), []byte("server-port=25567\n"), 0o644)

	post := func(form url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/servers/"+proxy.ID+"/settings/network", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.SetPathValue("id", proxy.ID)
		w := httptest.NewRecorder()
		a.saveNetwork(w, r)
		return w
	}

	w := post(url.Values{"backend": {survival.ID, creative.ID, fresh.ID}, "start": {creative.ID}})
	flash, _ := url.QueryUnescape(w.Result().Cookies()[0].Value)
	if !strings.Contains(flash, "Fresh: start it once first") {
		t.Errorf("flash = %q", flash)
	}
	got, _ := st.Server(proxy.ID)
	if !slices.Equal(got.Backends, []string{creative.ID, survival.ID}) {
		t.Fatalf("backends = %v", got.Backends)
	}
	b, _ := os.ReadFile(filepath.Join(pdir, velocity.ConfigFile))
	for _, want := range []string{`creative = "127.0.0.1:25566"`, `survival-world = "127.0.0.1:25565"`, `try = ["creative", "survival-world"]`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("velocity.toml lacks %q:\n%s", want, b)
		}
	}
	if strings.Contains(string(b), "a.example.com") || strings.Contains(string(b), "lobby") {
		t.Errorf("old servers left in velocity.toml:\n%s", b)
	}
	secret, _ := velocity.Secret(pdir)
	for _, d := range []string{sdir, cdir} {
		g, _ := os.ReadFile(filepath.Join(d, velocity.PaperConfig))
		p, _ := os.ReadFile(filepath.Join(d, "server.properties"))
		if !strings.Contains(string(g), "enabled: true") || !strings.Contains(string(g), secret) || !strings.Contains(string(p), "online-mode=false") {
			t.Errorf("%s not linked:\n%s\n%s", d, g, p)
		}
	}
	if v := a.view(httptest.NewRequest("GET", "/", nil), survival); v.ViaProxy != "Network" {
		t.Errorf("ViaProxy = %q", v.ViaProxy)
	}

	// Unlinking Survival puts it back to normal.
	post(url.Values{"backend": {creative.ID}, "start": {creative.ID}})
	g, _ := os.ReadFile(filepath.Join(sdir, velocity.PaperConfig))
	p, _ := os.ReadFile(filepath.Join(sdir, "server.properties"))
	if !strings.Contains(string(g), "enabled: false") || !strings.Contains(string(p), "online-mode=true") {
		t.Errorf("Survival still linked:\n%s\n%s", g, p)
	}
	got, _ = st.Server(proxy.ID)
	if !slices.Equal(got.Backends, []string{creative.ID}) {
		t.Errorf("backends = %v", got.Backends)
	}
}
