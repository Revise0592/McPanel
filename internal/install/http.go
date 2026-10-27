package install

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// UserAgent is sent with every outbound request.
const UserAgent = "McPanel/0.1 (+https://github.com/Revise0592/McPanel)"

var client = &http.Client{Timeout: 30 * time.Minute}

// Logf receives human-readable progress messages.
type Logf func(format string, args ...any)

func get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return resp, nil
}

// GetJSON fetches url and decodes the JSON body into v.
func GetJSON(ctx context.Context, url string, v any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := get(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(v)
}

// PostJSON sends body as JSON to url and decodes the JSON reply into v.
func PostJSON(ctx context.Context, url string, body, v any) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST %s: %s", url, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// Checksum names an expected digest of a download.
type Checksum struct {
	Algo string // "sha1", "sha256", "sha512" or ""
	Hex  string
}

// Download fetches url into dst atomically, verifying the checksum if set.
func Download(ctx context.Context, url, dst string, sum Checksum, logf Logf) error {
	resp, err := get(ctx, url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".download"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	var h hash.Hash
	switch sum.Algo {
	case "sha1":
		h = sha1.New()
	case "sha256":
		h = sha256.New()
	case "sha512":
		h = sha512.New()
	}
	var w io.Writer = out
	if h != nil {
		w = io.MultiWriter(out, h)
	}
	pr := &progress{r: resp.Body, total: resp.ContentLength, logf: logf, name: filepath.Base(dst)}
	_, err = io.Copy(w, pr)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if h != nil && sum.Hex != "" {
		if got := hex.EncodeToString(h.Sum(nil)); got != sum.Hex {
			os.Remove(tmp)
			return fmt.Errorf("checksum mismatch for %s", filepath.Base(dst))
		}
	}
	return os.Rename(tmp, dst)
}

type progress struct {
	r     io.Reader
	total int64
	done  int64
	last  time.Time
	logf  Logf
	name  string
}

func (p *progress) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.logf != nil && time.Since(p.last) > 2*time.Second {
		p.last = time.Now()
		if p.total > 0 {
			p.logf("Downloading %s: %d%%", p.name, p.done*100/p.total)
		} else {
			p.logf("Downloading %s: %d MB", p.name, p.done>>20)
		}
	}
	return n, err
}
