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
	"github.com/Revise0592/McPanel/internal/files"
	"github.com/Revise0592/McPanel/internal/heads"
	"github.com/Revise0592/McPanel/internal/importer"
	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/notify"
	"github.com/Revise0592/McPanel/internal/schedule"
	"github.com/Revise0592/McPanel/internal/store"
	"github.com/Revise0592/McPanel/internal/updates"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// App is the HTTP application.
type App struct {
	Store     *store.Store
	Mgr       *mc.Manager
	Updates   *updates.Checker
	Tasks     *schedule.Scheduler
	Notify    *notify.Notifier
	SetupCode string

	sessions *auth.Sessions
	limiter  *auth.Limiter
	pages    map[string]*template.Template
	partials *template.Template
	imports  importSet
	heads    *heads.Cache
}

// New builds the app and parses templates.
func New(st *store.Store, mgr *mc.Manager, setupCode string, secureCookies bool) (*App, error) {
	a := &App{
		Store:     st,
		Mgr:       mgr,
		Updates:   updates.New(st, mgr),
		Tasks:     schedule.New(st, mgr),
		Notify:    notify.New(st),
		SetupCode: setupCode,
		limiter:   auth.NewLimiter(),
		pages:     map[string]*template.Template{},
		heads:     heads.New(),
	}
	mgr.Activity().OnEvent(a.Notify.Event)
	mgr.OnJoin = a.heads.Forget
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
		Secure: func() bool { return secureCookies || st.Settings().BehindHTTPSProxy },
	}
	assetVersion := hashStatic()
	_, iconErr := fs.Stat(staticFS, "static/"+iconPath)
	hasIcon := iconErr == nil
	funcs := template.FuncMap{
		"asset":     func(name string) string { return "/static/" + name + "?v=" + assetVersion },
		"typeLabel": store.TypeLabel,
		"isJava":    store.IsJava,
		"isModded":  store.IsModded,
		"hasWorld":  store.HasWorld,
		"size":      backup.HumanSize,
		"ago":       ago,
		// The extra minute stops "in 6 hours" showing as "in 5 hours" a
		// moment after the task is added.
		"until":      func(t time.Time) string { return span(time.Until(t) + time.Minute) },
		"since":      since,
		"backupKind": backup.KindLabel,
		"add":        func(a, b int) int { return a + b },
		"div":        func(a, b int) int { return a / b },
		"playtime":   playtime,
		"date":       func(t time.Time) string { return t.Format("2 Jan 2006, 15:04") },
		"lower":      strings.ToLower,
		"title": func(s string) string {
			if s == "" {
				return s
			}
			return strings.ToUpper(s[:1]) + s[1:]
		},
		"pathEsc":    url.PathEscape,
		"editable":   files.Editable,
		"headURL":    headURL,
		"hasIcon":    func() bool { return hasIcon },
		"logo":       a.logoURL,
		"accentText": accentText,
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
	mux.HandleFunc("GET /favicon.ico", a.serveFavicon(static))
	mux.HandleFunc("GET /brand/logo.png", a.serveLogo)

	mux.HandleFunc("GET /setup", a.setupPage)
	mux.HandleFunc("POST /setup", a.setupSubmit)
	mux.HandleFunc("GET /login", a.loginPage)
	mux.HandleFunc("POST /login", a.loginSubmit)
	mux.HandleFunc("POST /logout", a.logout)
	mux.HandleFunc("GET /login/code", a.loginCodePage)
	mux.HandleFunc("POST /login/code", a.loginCodeSubmit)

	// Public status pages, reached by their secret link.
	mux.HandleFunc("GET /s/{token}", a.statusPage)
	mux.HandleFunc("GET /s/{token}/live", a.statusLive)
	mux.HandleFunc("GET /s/{token}/icon", a.statusIcon)
	mux.HandleFunc("GET /s/{token}/heads/{name}", a.statusHead)

	p := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, a.requireAuth(h)) }
	p("GET /{$}", a.dashboard)
	p("GET /partials/servers", a.serverCards)
	p("GET /servers/new", a.newServerPage)
	p("GET /versions", a.versionOptions)
	p("POST /servers", a.createServer)
	p("GET /modpacks", a.modpacksPage)
	p("GET /modpacks/search", a.modpacksSearch)
	p("POST /modpacks/upload", a.uploadModpack)
	p("GET /modpacks/{project}", a.modpackPage)
	p("POST /modpacks/{project}", a.createFromModpack)
	p("GET /import", a.importPage)
	p("POST /import", a.importUpload)
	p("GET /import/{token}", a.importConfirmPage)
	p("POST /import/{token}", a.importFinish)
	p("POST /import/{token}/cancel", a.importCancel)

	p("GET /servers/{id}", a.consolePage)
	p("GET /servers/{id}/status", a.statusPartial)
	p("GET /servers/{id}/activity", a.activityPage)
	p("GET /servers/{id}/usage", a.usagePartial)
	p("GET /servers/{id}/console/stream", a.consoleStream)
	p("POST /servers/{id}/command", a.sendCommand)
	p("POST /servers/{id}/power/{action}", a.power)

	p("GET /servers/{id}/players", a.playersPage)
	p("GET /servers/{id}/online", a.onlinePartial)
	p("POST /servers/{id}/players", a.playersAction)
	p("GET /heads/{name}", a.playerHead)

	p("GET /servers/{id}/settings", a.settingsPage)
	p("POST /servers/{id}/settings/game", a.saveGameSettings)
	p("POST /servers/{id}/settings/server", a.saveServerSettings)
	p("POST /servers/{id}/settings/raw", a.saveRawProperties)
	p("POST /servers/{id}/settings/proxy", a.saveProxySettings)
	p("POST /servers/{id}/settings/network", a.saveNetwork)
	p("POST /servers/{id}/settings/version", a.changeVersion)
	p("POST /servers/{id}/updates/check", a.checkUpdates)
	p("POST /servers/{id}/settings/bedrock-zip", a.uploadBedrockZip)
	p("GET /servers/{id}/icon", a.serverIcon)
	p("POST /servers/{id}/settings/icon", a.uploadServerIcon)
	p("POST /servers/{id}/settings/icon/remove", a.removeServerIcon)
	p("POST /servers/{id}/settings/status-page", a.saveStatusPage)
	p("POST /servers/{id}/delete", a.deleteServer)

	p("GET /servers/{id}/backups", a.backupsPage)
	p("POST /servers/{id}/backups", a.createBackup)
	p("POST /servers/{id}/backups/schedule", a.saveBackupSchedule)
	p("GET /servers/{id}/backups/{name}", a.downloadBackup)
	p("POST /servers/{id}/backups/{name}/restore", a.restoreBackup)
	p("POST /servers/{id}/backups/{name}/delete", a.deleteBackup)

	p("GET /backups/deleted", a.orphansPage)
	p("GET /backups/deleted/{id}/{name}", a.downloadOrphan)
	p("POST /backups/deleted/{id}/delete", a.deleteOrphanFolder)
	p("POST /backups/deleted/{id}/{name}/delete", a.deleteOrphan)

	p("GET /servers/{id}/schedule", a.schedulePage)
	p("POST /servers/{id}/schedule", a.addTask)
	p("POST /servers/{id}/schedule/delete", a.deleteTask)

	p("GET /servers/{id}/files", a.filesPage)
	p("GET /servers/{id}/files/download", a.downloadFile)
	p("POST /servers/{id}/files/upload", a.uploadFiles)
	p("POST /servers/{id}/files/mkdir", a.makeFolder)
	p("POST /servers/{id}/files/delete", a.deleteFile)
	p("GET /servers/{id}/files/edit", a.editFilePage)
	p("POST /servers/{id}/files/edit", a.saveFile)
	p("GET /servers/{id}/files/usage", a.diskUsage)
	p("POST /servers/{id}/files/clear-logs", a.clearOldLogs)

	p("GET /servers/{id}/addons", a.addonsPage)
	p("GET /servers/{id}/addons/search", a.addonsSearch)
	p("POST /servers/{id}/addons/install", a.addonsInstall)
	p("POST /servers/{id}/addons/update", a.addonsUpdate)
	p("POST /servers/{id}/addons/toggle", a.addonsToggle)
	p("POST /servers/{id}/addons/remove", a.addonsRemove)
	p("POST /servers/{id}/bedrock", a.bedrockAction)

	p("GET /settings", a.panelSettingsPage("appearance"))
	p("GET /settings/account", a.panelSettingsPage("account"))
	p("GET /settings/security", a.panelSettingsPage("security"))
	p("GET /settings/backups", a.panelSettingsPage("backups"))
	p("POST /settings/backups", a.saveBackupCopyDir)
	p("GET /settings/alerts", a.panelSettingsPage("alerts"))
	p("POST /settings/alerts", a.saveAlerts)
	p("POST /settings/alerts/test", a.testAlert)
	p("POST /settings", a.savePanelSettings)
	p("POST /settings/logo", a.uploadLogo)
	p("POST /settings/logo/remove", a.removeLogo)
	p("POST /settings/https-proxy", a.saveHTTPSProxy)
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
	// DiskFree is set when the data disk is nearly full.
	DiskFree int64
}

// lowDisk is when the panel starts warning about free space. Big worlds
// and their backups can need a few GB at once.
const lowDisk = 5 << 30

const flashCookie = "mcpanel_flash"

func setFlash(w http.ResponseWriter, msg string) {
	http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: url.QueryEscape(msg), Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func setError(w http.ResponseWriter, msg string) { setFlash(w, "!"+msg) }

func (a *App) render(w http.ResponseWriter, r *http.Request, name, title, nav string, data any) {
	p := page{Title: title, Panel: a.Store.Settings(), Nav: nav, Data: data}
	if free, ok := backup.FreeSpace(a.Mgr.DataDir()); ok && free < lowDisk && nav != "" {
		p.DiskFree = free
	}
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

// viaHTTPS reports whether the browser reached McPanel over HTTPS, either
// directly or through a proxy that says so. The header can be faked, so
// only use this for warnings, never for access decisions.
func viaHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
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

func since(t time.Time) string { return span(time.Since(t)) }

func span(d time.Duration) string {
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

// playtime is a short total such as "45 min" or "12 h 5 min".
func playtime(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "under a minute"
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	}
	h, m := int(d.Hours()), int(d.Minutes())%60
	if m == 0 || h >= 100 {
		return fmt.Sprintf("%d h", h)
	}
	return fmt.Sprintf("%d h %d min", h, m)
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}
