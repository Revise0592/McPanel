// Package web serves the panel's pages.
package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/Revise0592/McPanel/internal/auth"
	"github.com/Revise0592/McPanel/internal/backup"
	"github.com/Revise0592/McPanel/internal/importer"
	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/store"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// App is the HTTP application.
type App struct {
	Store     *store.Store
	Mgr       *mc.Manager
	SetupCode string

	sessions *auth.Sessions
	limiter  *auth.Limiter
	pages    map[string]*template.Template
	partials *template.Template
	imports  importSet
}

// New builds the app and parses templates.
func New(st *store.Store, mgr *mc.Manager, setupCode string, secureCookies bool) (*App, error) {
	a := &App{
		Store:     st,
		Mgr:       mgr,
		SetupCode: setupCode,
		limiter:   auth.NewLimiter(),
		pages:     map[string]*template.Template{},
	}
	importer.CleanStale(filepath.Join(mgr.DataDir(), "tmp"))
	a.sessions = &auth.Sessions{
		Secret: func() string { return st.Settings().SessionSecret },
		Fingerprint: func() string {
			s := st.Settings()
			if s.PasswordHash == "" {
				return ""
			}
			return s.PasswordHash + "|" + s.TOTPSecret
		},
		Secure: secureCookies,
	}
	assetVersion := hashStatic()
	_, iconErr := fs.Stat(staticFS, "static/"+iconPath)
	hasIcon := iconErr == nil
	funcs := template.FuncMap{
		"asset":     func(name string) string { return "/static/" + name + "?v=" + assetVersion },
		"typeLabel": store.TypeLabel,
		"isJava":    store.IsJava,
		"size":      backup.HumanSize,
		"ago":       ago,
		"since":     since,
		"date":      func(t time.Time) string { return t.Format("2 Jan 2006, 15:04") },
		"lower":     strings.ToLower,
		"title": func(s string) string {
			if s == "" {
				return s
			}
			return strings.ToUpper(s[:1]) + s[1:]
		},
		"pathEsc": url.PathEscape,
		"hasIcon": func() bool { return hasIcon },
		"gb": func(mb int) string {
			if mb%1024 == 0 {
				return fmt.Sprintf("%d GB", mb/1024)
			}
			return fmt.Sprintf("%.1f GB", float64(mb)/1024)
		},
	}
	base := template.Must(template.New("").Funcs(funcs).ParseFS(templateFS, "templates/layout.html", "templates/partials.html"))
	a.partials = base
	names, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		b := path.Base(n)
		if b == "layout.html" || b == "partials.html" {
			continue
		}
		t, err := template.Must(base.Clone()).ParseFS(templateFS, n)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", n, err)
		}
		a.pages[strings.TrimSuffix(b, ".html")] = t
	}
	return a, nil
}

// Handler returns the root HTTP handler.
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheFor(http.FileServerFS(static), 365*24*time.Hour)))
	// Browsers ask for /favicon.ico on their own, before reading the page.
	mux.HandleFunc("GET /favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFileFS(w, r, static, iconPath)
	})

	mux.HandleFunc("GET /setup", a.setupPage)
	mux.HandleFunc("POST /setup", a.setupSubmit)
	mux.HandleFunc("GET /login", a.loginPage)
	mux.HandleFunc("POST /login", a.loginSubmit)
	mux.HandleFunc("POST /logout", a.logout)
	mux.HandleFunc("GET /login/code", a.loginCodePage)
	mux.HandleFunc("POST /login/code", a.loginCodeSubmit)

	p := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, a.requireAuth(h)) }
	p("GET /{$}", a.dashboard)
	p("GET /partials/servers", a.serverCards)
	p("GET /servers/new", a.newServerPage)
	p("GET /versions", a.versionOptions)
	p("POST /servers", a.createServer)
	p("GET /import", a.importPage)
	p("POST /import", a.importUpload)
	p("GET /import/{token}", a.importConfirmPage)
	p("POST /import/{token}", a.importFinish)
	p("POST /import/{token}/cancel", a.importCancel)

	p("GET /servers/{id}", a.consolePage)
	p("GET /servers/{id}/status", a.statusPartial)
	p("GET /servers/{id}/console/stream", a.consoleStream)
	p("POST /servers/{id}/command", a.sendCommand)
	p("POST /servers/{id}/power/{action}", a.power)

	p("GET /servers/{id}/players", a.playersPage)
	p("GET /servers/{id}/online", a.onlinePartial)
	p("POST /servers/{id}/players", a.playersAction)

	p("GET /servers/{id}/settings", a.settingsPage)
	p("POST /servers/{id}/settings/game", a.saveGameSettings)
	p("POST /servers/{id}/settings/server", a.saveServerSettings)
	p("POST /servers/{id}/settings/raw", a.saveRawProperties)
	p("POST /servers/{id}/settings/version", a.changeVersion)
	p("POST /servers/{id}/settings/bedrock-zip", a.uploadBedrockZip)
	p("GET /servers/{id}/icon", a.serverIcon)
	p("POST /servers/{id}/settings/icon", a.uploadServerIcon)
	p("POST /servers/{id}/settings/icon/remove", a.removeServerIcon)
	p("POST /servers/{id}/delete", a.deleteServer)

	p("GET /servers/{id}/backups", a.backupsPage)
	p("POST /servers/{id}/backups", a.createBackup)
	p("POST /servers/{id}/backups/schedule", a.saveBackupSchedule)
	p("GET /servers/{id}/backups/{name}", a.downloadBackup)
	p("POST /servers/{id}/backups/{name}/restore", a.restoreBackup)
	p("POST /servers/{id}/backups/{name}/delete", a.deleteBackup)

	p("GET /servers/{id}/files", a.filesPage)
	p("GET /servers/{id}/files/download", a.downloadFile)
	p("POST /servers/{id}/files/upload", a.uploadFiles)
	p("POST /servers/{id}/files/mkdir", a.makeFolder)
	p("POST /servers/{id}/files/delete", a.deleteFile)

	p("GET /servers/{id}/addons", a.addonsPage)
	p("GET /servers/{id}/addons/search", a.addonsSearch)
	p("POST /servers/{id}/addons/install", a.addonsInstall)
	p("POST /servers/{id}/addons/toggle", a.addonsToggle)
	p("POST /servers/{id}/addons/remove", a.addonsRemove)

	p("GET /settings", a.panelSettingsPage)
	p("POST /settings", a.savePanelSettings)
	p("POST /settings/username", a.changeUsername)
	p("POST /settings/password", a.changePassword)
	p("GET /settings/2fa", a.twoFactorSetupPage)
	p("POST /settings/2fa", a.twoFactorSetupSubmit)
	p("POST /settings/2fa/disable", a.twoFactorDisable)
	p("POST /settings/2fa/codes", a.twoFactorNewCodes)

	return securityHeaders(sameOrigin(mux))
}

// iconPath is the optional custom icon, used as the favicon and header logo.
// Without it the panel shows a square in the accent colour.
const iconPath = "brand/favicon.ico"

// hashStatic fingerprints the embedded assets so browsers fetch new copies
// after an upgrade.
func hashStatic() string {
	h := sha256.New()
	fs.WalkDir(staticFS, "static", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := staticFS.ReadFile(p)
			h.Write(b)
		}
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:12]
}

func cacheFor(h http.Handler, d time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", int(d.Seconds())))
		h.ServeHTTP(w, r)
	})
}

func securityHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("X-Frame-Options", "DENY")
		hd.Set("Referrer-Policy", "same-origin")
		hd.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' https: data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'")
		h.ServeHTTP(w, r)
	})
}

// sameOrigin rejects cross-site form posts.
func sameOrigin(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			ok := true
			if o := r.Header.Get("Origin"); o != "" {
				u, err := url.Parse(o)
				ok = err == nil && u.Host == r.Host
			} else if s := r.Header.Get("Sec-Fetch-Site"); s != "" {
				ok = s == "same-origin" || s == "none"
			}
			if !ok {
				http.Error(w, "cross-site request blocked", http.StatusForbidden)
				return
			}
		}
		h.ServeHTTP(w, r)
	})
}

func (a *App) requireAuth(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		to := ""
		switch {
		case a.Store.Settings().PasswordHash == "":
			to = "/setup"
		case !a.sessions.Valid(r):
			to = "/login"
		}
		if to != "" {
			if r.Header.Get("HX-Request") != "" {
				w.Header().Set("HX-Redirect", to)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, to, http.StatusSeeOther)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// page is the data passed to every full page template.
type page struct {
	Title    string
	Panel    store.Settings
	Flash    string
	FlashErr bool
	Nav      string
	Data     any
}

const flashCookie = "mcpanel_flash"

func setFlash(w http.ResponseWriter, msg string) {
	http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: url.QueryEscape(msg), Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func setError(w http.ResponseWriter, msg string) { setFlash(w, "!"+msg) }

func (a *App) render(w http.ResponseWriter, r *http.Request, name, title, nav string, data any) {
	p := page{Title: title, Panel: a.Store.Settings(), Nav: nav, Data: data}
	if c, err := r.Cookie(flashCookie); err == nil {
		msg, _ := url.QueryUnescape(c.Value)
		if strings.HasPrefix(msg, "!") {
			p.FlashErr, msg = true, msg[1:]
		}
		p.Flash = msg
		http.SetCookie(w, &http.Cookie{Name: flashCookie, Path: "/", MaxAge: -1})
	}
	t, ok := a.pages[name]
	if !ok {
		http.Error(w, "missing template "+name, http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", p); err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "page error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

func (a *App) partial(w http.ResponseWriter, name string, data any) {
	var buf bytes.Buffer
	if err := a.partials.ExecuteTemplate(&buf, name, data); err != nil {
		log.Printf("partial %s: %v", name, err)
		http.Error(w, "page error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

func back(w http.ResponseWriter, r *http.Request, to string) {
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func hostOnly(r *http.Request) string {
	h := r.Host
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	if strings.Contains(h, ":") {
		return "[" + h + "]"
	}
	return h
}

func ago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return since(t) + " ago"
}

func since(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "less than a minute"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute")
	case d < 48*time.Hour:
		return plural(int(d.Hours()), "hour")
	}
	return plural(int(d.Hours()/24), "day")
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
