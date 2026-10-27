package web

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Revise0592/McPanel/internal/importer"
	"github.com/Revise0592/McPanel/internal/install"
	"github.com/Revise0592/McPanel/internal/store"
)

const maxImport = 32 << 30

// pendingImport is an unpacked upload waiting for the user to confirm.
type pendingImport struct {
	Token string
	Base  string
	importer.Found
	PortNote string
}

type importSet struct {
	mu sync.Mutex
	m  map[string]*pendingImport
}

func (s *importSet) get(tok string) *pendingImport {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[tok]
}

func (s *importSet) put(p *pendingImport) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]*pendingImport{}
	}
	s.m[p.Token] = p
}

func (s *importSet) take(tok string) *pendingImport {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.m[tok]
	delete(s.m, tok)
	return p
}

var tokenRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

func (a *App) tmpDir() string { return filepath.Join(a.Mgr.DataDir(), "tmp") }

func (a *App) importPage(w http.ResponseWriter, r *http.Request) {
	a.render(w, r, "import", "Import a server", "servers", nil)
}

func (a *App) importUpload(w http.ResponseWriter, r *http.Request) {
	fail := func(msg string) {
		setError(w, msg)
		back(w, r, "/import")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxImport)
	mr, err := r.MultipartReader()
	if err != nil {
		fail("Choose a zip file to upload.")
		return
	}
	if err := os.MkdirAll(a.tmpDir(), 0o755); err != nil {
		fail(err.Error())
		return
	}
	importer.CleanStale(a.tmpDir())
	tok := store.RandomHex(8)
	base := filepath.Join(a.tmpDir(), "import-"+tok)
	zipPath := base + ".zip"
	defer os.Remove(zipPath)

	// Stream the upload to disk; worlds can be several GB.
	fileName := ""
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			fail("Upload failed: " + err.Error())
			return
		}
		if part.FormName() != "zip" || part.FileName() == "" {
			continue
		}
		fileName = part.FileName()
		out, err := os.Create(zipPath)
		if err == nil {
			_, err = io.Copy(out, part)
			if cerr := out.Close(); err == nil {
				err = cerr
			}
		}
		if err != nil {
			fail("Upload failed: " + err.Error())
			return
		}
	}
	if fileName == "" {
		fail("Choose a zip file to upload.")
		return
	}
	if err := importer.Extract(zipPath, base); err != nil {
		os.RemoveAll(base)
		fail("Couldn't open that zip: " + err.Error())
		return
	}
	name := strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
	found, err := importer.Detect(base, name)
	if err != nil {
		os.RemoveAll(base)
		fail("Couldn't import: " + err.Error() + ".")
		return
	}
	p := &pendingImport{Token: tok, Base: base, Found: found}
	bedrock := found.Type == store.TypeBedrock
	if who := a.Mgr.UsedPorts("")[found.Port]; who != "" || found.Port == 0 {
		old := found.Port
		def := 25565
		if bedrock {
			def = 19132
		}
		p.Port = a.Mgr.FreePort(def, bedrock)
		if old != 0 {
			p.PortNote = fmt.Sprintf("Port %d is already used by %s, so %d was picked instead.", old, who, p.Port)
		}
	}
	a.imports.put(p)
	log.Printf("import %s: %s %s from %s", tok, found.Type, found.Version, fileName)
	back(w, r, "/import/"+tok)
}

type importConfirmData struct {
	*pendingImport
	Choice  newServerData
	Unknown bool
}

func (a *App) importConfirmPage(w http.ResponseWriter, r *http.Request) {
	p := a.pendingFor(w, r)
	if p == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	vs, err := install.Versions(ctx, p.Type)
	d := importConfirmData{pendingImport: p, Choice: newServerData{Types: serverTypes, Type: p.Type, Versions: vs, Selected: p.Version}}
	if err != nil {
		d.Choice.Err = "Couldn't load versions: " + err.Error()
	}
	if p.Type != store.TypeBedrock && (p.Version == "" || !slices.Contains(vs, p.Version)) {
		d.Unknown = true
	}
	a.render(w, r, "import_confirm", "Import a server", "servers", d)
}

func (a *App) pendingFor(w http.ResponseWriter, r *http.Request) *pendingImport {
	tok := r.PathValue("token")
	p := a.imports.get(tok)
	if !tokenRe.MatchString(tok) || p == nil {
		setError(w, "That import has expired. Please upload the zip again.")
		back(w, r, "/import")
		return nil
	}
	return p
}

func (a *App) importFinish(w http.ResponseWriter, r *http.Request) {
	p := a.pendingFor(w, r)
	if p == nil {
		return
	}
	to := "/import/" + p.Token
	fail := func(msg string) {
		setError(w, msg)
		back(w, r, to)
	}
	name := strings.TrimSpace(r.FormValue("name"))
	typ := r.FormValue("type")
	version := r.FormValue("version")
	port := formInt(r, "port", 0, 0, 65535)
	switch {
	case name == "" || len(name) > 40:
		fail("Give the server a name (up to 40 characters).")
		return
	case !slices.Contains(serverTypes, typ):
		fail("Pick a server type.")
		return
	case version == "":
		fail("Pick a version.")
		return
	case port < 1:
		fail("Pick a port between 1 and 65535.")
		return
	case r.FormValue("eula") != "on":
		fail("You need to accept the Minecraft EULA to run a server.")
		return
	}
	if who := a.Mgr.UsedPorts("")[port]; who != "" {
		fail(fmt.Sprintf("Port %d is already used by %s.", port, who))
		return
	}
	// Claim the import so a double submit can't use it twice.
	if a.imports.take(p.Token) == nil {
		back(w, r, "/")
		return
	}

	kv := map[string]string{"server-port": strconv.Itoa(port)}
	for k, v := range p.Props {
		kv[k] = v
	}
	bedrock := typ == store.TypeBedrock
	if bedrock {
		kv["server-portv6"] = strconv.Itoa(port + 1)
	}
	sv, err := a.Store.AddServer(store.Server{
		Name:             name,
		Type:             typ,
		Version:          version,
		MemoryMB:         formInt(r, "memory", 2048, 512, 65536),
		RestartOnCrash:   true,
		BackupEveryHours: 24,
		BackupKeep:       7,
		InitialProps:     kv,
	})
	if err != nil {
		a.imports.put(p)
		fail(err.Error())
		return
	}
	dst := filepath.Join(a.Mgr.DataDir(), "servers", sv.ID)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err == nil {
		err = os.Rename(p.Root, dst)
	}
	if err != nil {
		a.Store.DeleteServer(sv.ID)
		a.imports.put(p)
		fail("Couldn't move the files into place: " + err.Error())
		return
	}
	os.RemoveAll(p.Base)

	if _, err := os.Stat(filepath.Join(dst, "bedrock_server")); bedrock && err == nil {
		if err := a.Mgr.AdoptBedrock(sv.ID, bedrockVersion(p, version)); err != nil {
			setError(w, "Imported, but couldn't set it up: "+err.Error())
			back(w, r, "/servers/"+sv.ID)
			return
		}
		setFlash(w, "Imported. You can start it now.")
		back(w, r, "/servers/"+sv.ID)
		return
	}
	go a.Mgr.Install(sv.ID, version, nil)
	setFlash(w, "Imported. Downloading the server software; your worlds and settings are kept.")
	back(w, r, "/servers/"+sv.ID)
}

func bedrockVersion(p *pendingImport, chosen string) string {
	if chosen != "" && chosen != "latest" {
		return chosen
	}
	return "imported"
}

func (a *App) importCancel(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	if p := a.imports.take(tok); p != nil && tokenRe.MatchString(tok) {
		os.RemoveAll(p.Base)
	}
	back(w, r, "/")
}
