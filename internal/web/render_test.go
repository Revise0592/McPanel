package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Revise0592/McPanel/internal/activity"
	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/store"
)

// TestRenderServerPages renders the pages that mix several kinds of data,
// for a running server, so a template that reaches for the wrong field
// fails here instead of as a "page error".
func TestRenderServerPages(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(st, mc.NewManager(dir, st), "", false)
	if err != nil {
		t.Fatal(err)
	}
	v := serverView{
		Server:   store.Server{ID: "s", Name: "Survival", Type: store.TypePaper, Version: "1.21.4", Installed: true, MemoryMB: 4096},
		Status:   mc.Status{State: mc.Running, StartedAt: time.Now().Add(-time.Hour), Players: []string{"Steve"}},
		Usage:    mc.Usage{CPU: 12, MemoryMB: 2048, TPS: 19.9},
		HasUsage: true,
		Address:  "localhost:25565",
	}
	events := []activity.Event{{Time: time.Now(), Kind: activity.Crash, Text: "Server exited", Repeats: 1}}
	usage := usageData{serverView: v, Range: chartRanges[0], Ranges: chartRanges, HasData: true,
		Charts: []usageChart{{Title: "CPU", Now: "12%", Line: "M0,0L1,1", Area: "M0,100Z", Ticks: ticks(20, fmtPct)}}}

	pages := map[string]any{
		"console":  consoleData{activityData: activityData{serverView: v, Events: events, More: true}, Performance: usage},
		"activity": activityData{serverView: v, Events: events},
	}
	for name, data := range pages {
		w := httptest.NewRecorder()
		a.render(w, httptest.NewRequest("GET", "/", nil), name, "t", "servers", data)
		if w.Code != 200 {
			t.Errorf("%s: status %d: %s", name, w.Code, w.Body)
		}
	}
	w := httptest.NewRecorder()
	a.partial(w, "usage-card", usage)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Performance") {
		t.Errorf("usage-card: status %d: %s", w.Code, w.Body)
	}
}
