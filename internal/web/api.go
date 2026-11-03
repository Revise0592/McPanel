package web

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Revise0592/McPanel/internal/activity"
	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/store"
)

// The JSON API under /api/v1 is for the mobile app. Phones pair once with
// a code from Settings → Mobile app and then send their token as
// "Authorization: Bearer <token>". The API never reads the sign-in cookie,
// so other sites can't use a browser's session to call it.

// apiVersion is bumped when a change would break existing apps.
const apiVersion = 1

type ctxKey int

const deviceKey ctxKey = 0

// writeJSON sends v as the response body.
func writeJSON(w http.ResponseWriter, code int, v any) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("api: write response: %v", err)
	}
}

type apiError struct {
	Error string `json:"error"`
}

func apiFail(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, apiError{msg})
}

// readJSON decodes a small JSON request body into v.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		apiFail(w, http.StatusBadRequest, "The request body isn't valid JSON.")
		return false
	}
	return true
}

// bearerToken returns the token from the Authorization header.
func bearerToken(r *http.Request) string {
	scheme, tok, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(tok)
}

// requireDevice lets through requests carrying a paired device's token.
func (a *App) requireDevice(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dev, ok := a.Store.DeviceByToken(bearerToken(r))
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer realm="mcpanel"`)
			apiFail(w, http.StatusUnauthorized, "This device isn't paired with the panel, or it was removed.")
			return
		}
		h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), deviceKey, dev)))
	})
}

func deviceFrom(r *http.Request) store.Device {
	d, _ := r.Context().Value(deviceKey).(store.Device)
	return d
}

// apiRoutes adds the API's routes to mux.
func (a *App) apiRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/pair", a.apiPair)

	d := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, a.requireDevice(h)) }
	d("GET /api/v1/me", a.apiMe)
	d("POST /api/v1/unpair", a.apiUnpair)
	d("GET /api/v1/servers", a.apiServers)
	d("GET /api/v1/servers/{id}", a.apiServer)
	d("POST /api/v1/servers/{id}/power/{action}", a.apiPower)
	d("POST /api/v1/servers/{id}/command", a.apiCommand)
	d("GET /api/v1/servers/{id}/console", a.apiConsole)
	d("GET /api/v1/servers/{id}/activity", a.apiServerActivity)
	d("GET /api/v1/servers/{id}/usage", a.apiUsage)
	d("GET /api/v1/activity", a.apiActivity)
	// Anything else under /api gets a JSON 404 rather than a redirect to
	// the sign-in page.
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		apiFail(w, http.StatusNotFound, "Unknown API address.")
	})
}

type apiPanel struct {
	Name       string `json:"name"`
	Accent     string `json:"accent"`
	Version    string `json:"version"`
	APIVersion int    `json:"apiVersion"`
}

func (a *App) panelInfo() apiPanel {
	s := a.Store.Settings()
	return apiPanel{Name: s.PanelName, Accent: s.Accent, Version: a.Version, APIVersion: apiVersion}
}

type apiDevice struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

func deviceJSON(d store.Device) apiDevice {
	return apiDevice{ID: d.ID, Name: d.Name, CreatedAt: d.CreatedAt}
}

func (a *App) apiPair(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if err := a.limiter.Check(ip); err != nil {
		apiFail(w, http.StatusTooManyRequests, "Too many wrong codes. Try again in 15 minutes.")
		return
	}
	var req struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if !a.pairing.use(req.Code) {
		a.limiter.Fail(ip)
		log.Printf("wrong mobile pairing code from %s", ip)
		apiFail(w, http.StatusUnauthorized, "That pairing code is wrong or has expired. Make a new one in Settings → Mobile app.")
		return
	}
	a.limiter.Reset(ip)
	name := cleanDeviceName(req.Name)
	dev, token, err := a.Store.AddDevice(name)
	if err != nil {
		apiFail(w, http.StatusInternalServerError, "Couldn't save the device: "+err.Error())
		return
	}
	log.Printf("paired mobile device %q from %s", name, ip)
	writeJSON(w, http.StatusCreated, struct {
		Token  string    `json:"token"`
		Device apiDevice `json:"device"`
		Panel  apiPanel  `json:"panel"`
	}{token, deviceJSON(dev), a.panelInfo()})
}

// cleanDeviceName keeps a device name short and printable.
func cleanDeviceName(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	if rs := []rune(s); len(rs) > 40 {
		s = strings.TrimSpace(string(rs[:40]))
	}
	return cmp.Or(s, "Phone")
}

func (a *App) apiMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		Device apiDevice `json:"device"`
		Panel  apiPanel  `json:"panel"`
	}{deviceJSON(deviceFrom(r)), a.panelInfo()})
}

func (a *App) apiUnpair(w http.ResponseWriter, r *http.Request) {
	if err := a.Store.RemoveDevice(deviceFrom(r).ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		apiFail(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// apiServerJSON is one server as the app sees it.
type apiServerJSON struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Type      string     `json:"type"`
	TypeLabel string     `json:"typeLabel"`
	Version   string     `json:"version"`
	Installed bool       `json:"installed"`
	State     mc.State   `json:"state"`
	Label     string     `json:"label"`
	Task      string     `json:"task,omitempty"`
	Progress  int        `json:"progress"`
	StartedAt *time.Time `json:"startedAt,omitempty"`
	Players   []string   `json:"players"`
	Address   string     `json:"address,omitempty"`
	// BedrockAddress is where Bedrock players join through Geyser.
	BedrockAddress string    `json:"bedrockAddress,omitempty"`
	MemoryLimitMB  int       `json:"memoryLimitMB,omitempty"`
	Usage          *apiUsage `json:"usage,omitempty"`
	// NewBuild is a newer build of the installed version, if there is one.
	NewBuild       string `json:"newBuild,omitempty"`
	AutoStart      bool   `json:"autoStart"`
	RestartOnCrash bool   `json:"restartOnCrash"`
	SleepAfterMins int    `json:"sleepAfterMins"`
}

type apiUsage struct {
	CPU      float64 `json:"cpu"` // percent of the machine; -1 until known
	MemoryMB int     `json:"memoryMB"`
	TPS      float64 `json:"tps,omitempty"`
}

func serverJSON(v serverView) apiServerJSON {
	out := apiServerJSON{
		ID: v.ID, Name: v.Name, Type: v.Type, TypeLabel: store.TypeLabel(v.Type),
		Version: v.Version, Installed: v.Installed,
		State: cmp.Or(v.Status.State, mc.Stopped), Label: v.Status.Label(),
		Task: v.Status.Task, Progress: v.Status.Progress,
		Players: v.Status.Players, Address: v.Address, BedrockAddress: v.BedrockAddress,
		MemoryLimitMB: v.MemoryMB, NewBuild: v.NewBuild,
		AutoStart: v.AutoStart, RestartOnCrash: v.RestartOnCrash, SleepAfterMins: v.SleepAfterMins,
	}
	if out.Players == nil {
		out.Players = []string{}
	}
	if v.Status.Online() && !v.Status.StartedAt.IsZero() {
		t := v.Status.StartedAt
		out.StartedAt = &t
	}
	if v.HasUsage {
		out.Usage = &apiUsage{CPU: v.Usage.CPU, MemoryMB: v.Usage.MemoryMB, TPS: v.Usage.TPS}
	}
	return out
}

func (a *App) apiServers(w http.ResponseWriter, r *http.Request) {
	out := []apiServerJSON{}
	for _, v := range a.allViews(r) {
		out = append(out, serverJSON(v))
	}
	writeJSON(w, http.StatusOK, struct {
		Servers []apiServerJSON `json:"servers"`
	}{out})
}

// apiLoad fetches the server named in the URL, writing a JSON 404 if it is
// missing.
func (a *App) apiLoad(w http.ResponseWriter, r *http.Request) (store.Server, *mc.Instance, bool) {
	id := r.PathValue("id")
	sv, err := a.Store.Server(id)
	if err == nil {
		var inst *mc.Instance
		if inst, err = a.Mgr.Get(id); err == nil {
			return sv, inst, true
		}
	}
	apiFail(w, http.StatusNotFound, "There's no server with that ID.")
	return sv, nil, false
}

func (a *App) apiServer(w http.ResponseWriter, r *http.Request) {
	sv, _, ok := a.apiLoad(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, serverJSON(a.view(r, sv)))
}

func (a *App) apiPower(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.apiLoad(w, r)
	if !ok {
		return
	}
	var err error
	switch r.PathValue("action") {
	case "start":
		err = inst.Start()
	case "stop":
		inst.Stop()
	case "restart":
		inst.Restart()
	case "kill":
		inst.Kill()
	default:
		apiFail(w, http.StatusNotFound, "Unknown action. Use start, stop, restart or kill.")
		return
	}
	if err != nil {
		apiFail(w, http.StatusConflict, err.Error())
		return
	}
	// Give the state a moment to change so the reply reflects it.
	time.Sleep(150 * time.Millisecond)
	writeJSON(w, http.StatusOK, serverJSON(a.view(r, sv)))
}

// apiConsole streams the console as server-sent events, one line per
// "data:" event, starting with the recent backlog.
func (a *App) apiConsole(w http.ResponseWriter, r *http.Request) {
	_, inst, ok := a.apiLoad(w, r)
	if !ok {
		return
	}
	streamConsole(w, r, inst)
}

func (a *App) apiCommand(w http.ResponseWriter, r *http.Request) {
	_, inst, ok := a.apiLoad(w, r)
	if !ok {
		return
	}
	var req struct {
		Command string `json:"command"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	cmd := strings.TrimPrefix(strings.TrimSpace(req.Command), "/")
	if cmd == "" {
		apiFail(w, http.StatusBadRequest, "The command is empty.")
		return
	}
	if err := inst.Send(cmd); err != nil {
		if errors.Is(err, mc.ErrNotRunning) {
			apiFail(w, http.StatusConflict, "The server isn't running.")
			return
		}
		apiFail(w, http.StatusInternalServerError, err.Error())
		return
	}
	inst.Logf("> %s", cmd)
	w.WriteHeader(http.StatusNoContent)
}

type apiEvent struct {
	ServerID   string        `json:"serverId,omitempty"`
	ServerName string        `json:"serverName,omitempty"`
	Time       time.Time     `json:"time"`
	Kind       activity.Kind `json:"kind,omitempty"`
	Text       string        `json:"text"`
	Repeats    int           `json:"repeats,omitempty"`
}

// queryLimit reads ?limit=, defaulting to def and capped at 200.
func queryLimit(r *http.Request, def int) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n <= 0 {
		return def
	}
	return min(n, 200)
}

func (a *App) apiServerActivity(w http.ResponseWriter, r *http.Request) {
	sv, _, ok := a.apiLoad(w, r)
	if !ok {
		return
	}
	out := []apiEvent{}
	for _, e := range a.Mgr.Activity().List(sv.ID, queryLimit(r, 50)) {
		out = append(out, apiEvent{Time: e.Time, Kind: e.Kind, Text: e.Text, Repeats: e.Repeats})
	}
	writeJSON(w, http.StatusOK, struct {
		Events []apiEvent `json:"events"`
	}{out})
}

// apiActivity lists recent events across every server, newest first.
// ?since= (RFC 3339) returns only newer ones, so the app can poll for
// things to tell the user about.
func (a *App) apiActivity(w http.ResponseWriter, r *http.Request) {
	var since time.Time
	if s := r.URL.Query().Get("since"); s != "" {
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			apiFail(w, http.StatusBadRequest, "since should be a time like 2026-01-02T15:04:05Z.")
			return
		}
		since = t
	}
	limit := queryLimit(r, 50)
	out := []apiEvent{}
	for _, sv := range a.Store.Servers() {
		for _, e := range a.Mgr.Activity().List(sv.ID, limit) {
			if !e.Time.After(since) {
				break
			}
			out = append(out, apiEvent{ServerID: sv.ID, ServerName: sv.Name, Time: e.Time, Kind: e.Kind, Text: e.Text, Repeats: e.Repeats})
		}
	}
	slices.SortStableFunc(out, func(x, y apiEvent) int { return y.Time.Compare(x.Time) })
	if len(out) > limit {
		out = out[:limit]
	}
	writeJSON(w, http.StatusOK, struct {
		Events []apiEvent `json:"events"`
	}{out})
}

type apiSample struct {
	At       time.Time `json:"at"`
	CPU      float64   `json:"cpu"`
	MemoryMB int       `json:"memoryMB"`
	TPS      float64   `json:"tps,omitempty"`
}

// apiUsage returns the CPU, memory and TPS samples for ?range=hour (the
// default) or day, oldest first.
func (a *App) apiUsage(w http.ResponseWriter, r *http.Request) {
	_, inst, ok := a.apiLoad(w, r)
	if !ok {
		return
	}
	rg := chartRanges[0]
	if key := r.URL.Query().Get("range"); key != "" {
		i := slices.IndexFunc(chartRanges, func(c chartRange) bool { return c.Key == key })
		if i < 0 {
			apiFail(w, http.StatusBadRequest, "range should be hour or day.")
			return
		}
		rg = chartRanges[i]
	}
	out := []apiSample{}
	for _, s := range inst.History(time.Now().Add(-rg.Span)) {
		out = append(out, apiSample{At: s.At, CPU: s.CPU, MemoryMB: s.MemoryMB, TPS: s.TPS})
	}
	writeJSON(w, http.StatusOK, struct {
		Range   string      `json:"range"`
		Samples []apiSample `json:"samples"`
	}{rg.Key, out})
}
