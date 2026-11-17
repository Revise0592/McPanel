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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Revise0592/McPanel/internal/activity"
	"github.com/Revise0592/McPanel/internal/install"
	"github.com/Revise0592/McPanel/internal/store"
)

var client = &http.Client{Timeout: 15 * time.Second}

// queueSize is how many alerts can wait to be sent. More are dropped (and
// logged) rather than holding up the server that raised them.
const queueSize = 100

// attempts is how many times an alert is tried before giving up.
const attempts = 4

type job struct {
	server, kind, text string
}

// Notifier sends alerts using the panel's settings. Alerts go out one at a
// time, in the order they happened, and are retried if the webhook is
// down or rate-limiting.
type Notifier struct {
	st    *store.Store
	queue chan job
	idle  sync.WaitGroup

	mu       sync.Mutex
	lastErr  string
	lastFail time.Time
	lastOK   time.Time
}

// New returns a notifier that reads its settings from st.
func New(st *store.Store) *Notifier {
	n := &Notifier{st: st, queue: make(chan job, queueSize)}
	go n.run()
	return n
}

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

// Event queues an alert for a server event if the settings ask for one.
// It fits activity.Log.OnEvent.
func (n *Notifier) Event(serverID string, e activity.Event) {
	if !Wanted(n.st.Settings(), e.Kind) {
		return
	}
	n.enqueue(job{server: n.serverName(serverID), kind: string(e.Kind), text: e.Text})
}

// Player queues an alert for a player joining or leaving a server if the
// settings ask for one. It fits mc.Manager.OnPlayer.
func (n *Notifier) Player(serverID, name string, joined bool) {
	s := n.st.Settings()
	if s.WebhookURL == "" || !s.AlertPlayers {
		return
	}
	text, kind := name+" joined", "join"
	if !joined {
		text, kind = name+" left", "leave"
	}
	n.enqueue(job{server: n.serverName(serverID), kind: kind, text: text})
}

func (n *Notifier) serverName(id string) string {
	if sv, err := n.st.Server(id); err == nil {
		return sv.Name
	}
	return id
}

func (n *Notifier) enqueue(j job) {
	n.idle.Add(1)
	select {
	case n.queue <- j:
	default:
		n.idle.Done()
		log.Printf("alert for %s dropped, too many waiting: %s", j.server, j.text)
	}
}

func (n *Notifier) run() {
	for j := range n.queue {
		n.deliver(j)
		n.idle.Done()
	}
}

func (n *Notifier) deliver(j job) {
	var err error
	for try := 1; try <= attempts; try++ {
		// Read the settings each try, so a fixed webhook address is used
		// and a cleared one stops the retries.
		s := n.st.Settings()
		if s.WebhookURL == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err = Send(ctx, s.WebhookURL, s.PanelName, j.server, j.kind, j.text)
		cancel()
		if err == nil {
			n.Note(nil)
			return
		}
		var re *retryError
		if !errors.As(err, &re) || try == attempts {
			break
		}
		time.Sleep(re.after(try))
	}
	log.Printf("alert for %s: %v", j.server, err)
	n.Note(err)
}

// Note records the outcome of sending an alert, for Status.
func (n *Notifier) Note(err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if err != nil {
		n.lastErr, n.lastFail = err.Error(), time.Now()
	} else {
		n.lastOK = time.Now()
	}
}

// Status is the outcome of recent alerts, for the settings page.
type Status struct {
	LastError string
	Failed    time.Time // when LastError happened
	Sent      time.Time // last alert that went through
}

// Status reports how recent alerts went.
func (n *Notifier) Status() Status {
	n.mu.Lock()
	defer n.mu.Unlock()
	return Status{LastError: n.lastErr, Failed: n.lastFail, Sent: n.lastOK}
}

// Flush waits until queued alerts are sent or ctx ends, so alerts raised
// while the panel shuts down (servers stopping) still go out.
func (n *Notifier) Flush(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		n.idle.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// retryError is a failure worth trying again: the webhook was unreachable,
// busy or rate-limiting.
type retryError struct {
	err  error
	wait time.Duration // asked for by the webhook, if it said
}

func (e *retryError) Error() string { return e.err.Error() }
func (e *retryError) Unwrap() error { return e.err }

func (e *retryError) after(try int) time.Duration {
	if e.wait > 0 {
		return min(e.wait, time.Minute)
	}
	return time.Duration(try*try) * 2 * time.Second
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
		return &retryError{err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		err := fmt.Errorf("the webhook answered %s %s", resp.Status, strings.TrimSpace(string(msg)))
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			re := &retryError{err: err}
			// Discord and Slack say how long to wait, in seconds.
			if secs, perr := strconv.ParseFloat(resp.Header.Get("Retry-After"), 64); perr == nil && secs > 0 {
				re.wait = time.Duration(secs * float64(time.Second))
			}
			return re
		}
		return err
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
