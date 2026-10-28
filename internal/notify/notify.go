// Package notify posts alerts about server events to a webhook.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Revise0592/McPanel/internal/activity"
	"github.com/Revise0592/McPanel/internal/install"
	"github.com/Revise0592/McPanel/internal/store"
)

var client = &http.Client{Timeout: 15 * time.Second}

// Notifier sends alerts using the panel's settings.
type Notifier struct {
	st *store.Store
}

// New returns a notifier that reads its settings from st.
func New(st *store.Store) *Notifier { return &Notifier{st: st} }

// Wanted reports whether the settings ask for alerts about events of kind.
func Wanted(s store.Settings, kind activity.Kind) bool {
	if s.WebhookURL == "" {
		return false
	}
	switch kind {
	case activity.Crash:
		return s.AlertCrashes
	case activity.Failed:
		return s.AlertFailures
	case activity.Available:
		return s.AlertUpdates
	case activity.Power:
		return s.AlertPower
	}
	return false
}

// Event sends an alert for a server event if the settings ask for one. It
// fits activity.Log.OnEvent.
func (n *Notifier) Event(serverID string, e activity.Event) {
	s := n.st.Settings()
	if !Wanted(s, e.Kind) {
		return
	}
	name := serverID
	if sv, err := n.st.Server(serverID); err == nil {
		name = sv.Name
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := Send(ctx, s.WebhookURL, s.PanelName, name, string(e.Kind), e.Text); err != nil {
		log.Printf("alert for %s: %v", name, err)
	}
}

// CheckURL reports whether u looks like a webhook address.
func CheckURL(u string) error {
	p, err := url.Parse(u)
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
		return errors.New("it should start with https://")
	}
	return nil
}

// Send posts one message. Discord and Slack webhooks get their own message
// format; anything else gets a small JSON object.
func Send(ctx context.Context, webhook, panel, server, kind, text string) error {
	line := server + ": " + text
	var body any
	switch {
	case isDiscord(webhook):
		body = map[string]any{"username": panel, "content": "**" + server + "**: " + text}
	case strings.Contains(webhook, "hooks.slack.com"):
		body = map[string]any{"text": "*" + server + "*: " + text}
	default:
		body = map[string]any{"panel": panel, "server": server, "event": kind, "message": text, "text": line, "time": time.Now().Format(time.RFC3339)}
	}
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", install.UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("the webhook answered %s %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}

func isDiscord(u string) bool {
	p, err := url.Parse(u)
	if err != nil {
		return false
	}
	h := strings.ToLower(p.Hostname())
	return h == "discord.com" || h == "discordapp.com" || strings.HasSuffix(h, ".discord.com")
}
