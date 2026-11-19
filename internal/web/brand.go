package web

import (
	"fmt"
	"io/fs"
	"math"
	"net/http"
	"os"
	"path/filepath"

	"github.com/Revise0592/McPanel/internal/icon"
)

// The panel logo is uploaded in Settings and kept in the data folder, so it
// survives upgrades. Without one the panel falls back to the icon built into
// the binary (iconPath), then to a square in the accent colour.
const (
	logoSize      = 128
	maxLogoUpload = 10 << 20
)

func (a *App) logoPath() string { return filepath.Join(a.Mgr.DataDir(), "brand", "logo.png") }

// logoURL returns the uploaded logo's address, or "" when there isn't one.
func (a *App) logoURL() string {
	fi, err := os.Stat(a.logoPath())
	if err != nil {
		return ""
	}
	return fmt.Sprintf("/brand/logo.png?v=%d", fi.ModTime().UnixNano())
}

// serveLogo is public because the sign-in page shows the logo too.
func (a *App) serveLogo(w http.ResponseWriter, r *http.Request) {
	setImageCache(w, r)
	http.ServeFile(w, r, a.logoPath())
}

// setImageCache lets browsers keep uploaded pictures whose URL carries a
// ?v= change time, since a new upload changes the URL. Others are rechecked.
func setImageCache(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("v") != "" {
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
}

// serveFavicon answers browsers asking for /favicon.ico on their own.
func (a *App) serveFavicon(static fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := os.Stat(a.logoPath()); err == nil {
			a.serveLogo(w, r)
			return
		}
		http.ServeFileFS(w, r, static, iconPath)
	}
}

func (a *App) uploadLogo(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxLogoUpload)
	file, _, err := r.FormFile("logo")
	if err != nil {
		setError(w, "Choose a picture to upload (up to 10 MB).")
		back(w, r, appearanceURL)
		return
	}
	defer file.Close()
	b, err := icon.MakeSize(file, logoSize)
	if err == nil {
		err = os.MkdirAll(filepath.Dir(a.logoPath()), 0o755)
	}
	if err == nil {
		err = os.WriteFile(a.logoPath(), b, 0o644)
	}
	if err != nil {
		setError(w, "Couldn't set the logo: "+err.Error())
	} else {
		setFlash(w, "Logo saved.")
	}
	back(w, r, appearanceURL)
}

func (a *App) removeLogo(w http.ResponseWriter, r *http.Request) {
	if err := os.Remove(a.logoPath()); err != nil && !os.IsNotExist(err) {
		setError(w, "Couldn't remove the logo: "+err.Error())
	} else {
		setFlash(w, "Logo removed.")
	}
	back(w, r, appearanceURL)
}

// accentText picks white or near-black text for buttons filled with the
// accent colour: whichever has the higher WCAG contrast against it, so light
// accents like yellow and mid blues both stay readable.
func accentText(hex string) string {
	var r, g, b int
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return "#fff"
	}
	l := luminance(r, g, b)
	dark := luminance(0x16, 0x19, 0x1d)
	if (l+0.05)/(dark+0.05) > 1.05/(l+0.05) {
		return "#16191d"
	}
	return "#fff"
}

// luminance is the WCAG relative luminance of an sRGB colour, 0 to 1.
func luminance(r, g, b int) float64 {
	lin := func(c int) float64 {
		v := float64(c) / 255
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}
