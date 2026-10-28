package web

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Revise0592/McPanel/internal/notify"
	"github.com/Revise0592/McPanel/internal/store"
)

const alertsURL = "/settings/alerts"

func (a *App) saveAlerts(w http.ResponseWriter, r *http.Request) {
	hook := strings.TrimSpace(r.FormValue("webhookURL"))
	if hook != "" {
		if err := notify.CheckURL(hook); err != nil {
			setError(w, "That webhook address doesn't look right: "+err.Error()+".")
			back(w, r, alertsURL)
			return
		}
	}
	a.Store.UpdateSettings(func(s *store.Settings) {
		s.WebhookURL = hook
		s.AlertCrashes = r.FormValue("crashes") == "on"
		s.AlertFailures = r.FormValue("failures") == "on"
		s.AlertUpdates = r.FormValue("updates") == "on"
		s.AlertPower = r.FormValue("power") == "on"
	})
	if hook == "" {
		setFlash(w, "Saved. Alerts are off.")
	} else {
		setFlash(w, "Saved.")
	}
	back(w, r, alertsURL)
}

// testAlert sends a message to the webhook in the form, saved or not.
func (a *App) testAlert(w http.ResponseWriter, r *http.Request) {
	hook := strings.TrimSpace(r.FormValue("webhookURL"))
	if err := notify.CheckURL(hook); err != nil {
		setError(w, "Enter a webhook address first.")
		back(w, r, alertsURL)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	s := a.Store.Settings()
	if err := notify.Send(ctx, hook, s.PanelName, s.PanelName, "test", "Test message. Alerts are working."); err != nil {
		setError(w, "The test didn't go through: "+err.Error())
	} else {
		setFlash(w, "Test message sent. Save to keep this address.")
	}
	back(w, r, alertsURL)
}
