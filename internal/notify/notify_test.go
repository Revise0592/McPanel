package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/Revise0592/McPanel/internal/activity"
	"github.com/Revise0592/McPanel/internal/store"
)

func TestSendGenericJSON(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
	}))
	defer srv.Close()
	if err := Send(context.Background(), srv.URL, "McPanel", "Survival", "crash", "Server exited"); err != nil {
		t.Fatal(err)
	}
	if got["server"] != "Survival" || got["event"] != "crash" || got["text"] != "Survival: Server exited" {
		t.Errorf("unexpected body: %v", got)
	}
}

func TestSendReportsHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad token", http.StatusUnauthorized)
	}))
	defer srv.Close()
	if err := Send(context.Background(), srv.URL, "p", "s", "", "x"); err == nil {
		t.Error("expected an error for a 401")
	}
}

func TestWanted(t *testing.T) {
	s := store.Settings{WebhookURL: "https://example.com/hook", AlertCrashes: true}
	if !Wanted(s, activity.Crash) || Wanted(s, activity.Failed) || Wanted(s, activity.Info) {
		t.Error("Wanted ignored the settings")
	}
	s.WebhookURL = ""
	if Wanted(s, activity.Crash) {
		t.Error("Wanted without a webhook")
	}
	if !isDiscord("https://discord.com/api/webhooks/1/x") || isDiscord("https://evil.example/discord.com") {
		t.Error("isDiscord")
	}
}

func TestNotifierRetriesAndKeepsOrder(t *testing.T) {
	var got []string
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0.05")
			http.Error(w, "slow down", http.StatusTooManyRequests)
			return
		}
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		got = append(got, b["message"])
	}))
	defer srv.Close()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	st.UpdateSettings(func(s *store.Settings) { s.WebhookURL = srv.URL; s.AlertPower = true; s.AlertPlayers = true })
	n := New(st)
	n.Event("s", activity.Event{Kind: activity.Power, Text: "Started"})
	n.Player("s", "Steve", true)
	n.Event("s", activity.Event{Kind: activity.Power, Text: "Stopped"})
	n.Event("s", activity.Event{Kind: activity.Info, Text: "Backup saved"}) // not alerted
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	n.Flush(ctx)
	if want := []string{"Started", "Steve joined", "Stopped"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if st := n.Status(); st.LastError != "" || st.Sent.IsZero() {
		t.Errorf("status %+v", st)
	}
}
