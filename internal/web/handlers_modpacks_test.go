package web

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/store"
)

func TestUploadRejectsNonPack(t *testing.T) {
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(st, mc.NewManager(dir, st), "", false)
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("pack", "notapack.mrpack")
	fw.Write([]byte("hello"))
	mw.WriteField("eula", "on")
	mw.Close()
	r := httptest.NewRequest("POST", "/modpacks/upload", &body)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	a.uploadModpack(w, r)

	if loc := w.Header().Get("Location"); loc != "/modpacks" {
		t.Errorf("redirected to %q", loc)
	}
	flash, _ := url.QueryUnescape(w.Result().Cookies()[0].Value)
	if !strings.Contains(flash, "isn't a Modrinth modpack") {
		t.Errorf("flash = %q", flash)
	}
	if len(st.Servers()) != 0 {
		t.Error("a server was made")
	}
	left, _ := filepath.Glob(filepath.Join(dir, "tmp", "*"))
	if len(left) != 0 {
		t.Errorf("upload left behind: %v", left)
	}
}
