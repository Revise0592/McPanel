package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
