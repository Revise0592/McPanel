// Command mcpanel runs the McPanel web panel.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/Revise0592/McPanel/internal/auth"
	"github.com/Revise0592/McPanel/internal/backup"
	"github.com/Revise0592/McPanel/internal/mc"
	"github.com/Revise0592/McPanel/internal/store"
	"github.com/Revise0592/McPanel/internal/web"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(version)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "reset-password" {
		resetPassword(os.Args[2:])
		return
	}
	fs := flag.NewFlagSet("mcpanel", flag.ExitOnError)
	dataDir := fs.String("data", envOr("MCPANEL_DATA", "./data"), "folder for servers, backups and settings")
	addr := fs.String("addr", envOr("MCPANEL_ADDR", ":8080"), "address to listen on")
	cert := fs.String("tls-cert", os.Getenv("MCPANEL_TLS_CERT"), "TLS certificate file (optional)")
	key := fs.String("tls-key", os.Getenv("MCPANEL_TLS_KEY"), "TLS key file (optional)")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage:\n  mcpanel [flags]\n  mcpanel reset-password [-data dir]\n\nFlags:\n")
		fs.PrintDefaults()
	}
	fs.Parse(os.Args[1:])

	dir, err := filepath.Abs(*dataDir)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		log.Fatal(err)
	}
	st, err := store.Open(dir)
	if err != nil {
		log.Fatalf("open settings: %v", err)
	}
	mgr := mc.NewManager(dir, st)

	setupCode := ""
	if st.Settings().PasswordHash == "" {
		setupCode = newSetupCode()
		log.Printf("First-time setup: open the panel in your browser and enter this code: %s", setupCode)
	}
	app, err := web.New(st, mgr, setupCode, *cert != "")
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := &http.Server{
		Addr:              *addr,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		log.Printf("McPanel %s listening on %s (data in %s)", version, *addr, dir)
		var err error
		if *cert != "" {
			err = srv.ListenAndServeTLS(*cert, *key)
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	mgr.StartAutostart()
	go backup.RunScheduler(ctx, st, mgr)
	go app.Updates.Run(ctx)
	go app.Tasks.Run(ctx)
	go mgr.RunSampler(ctx)

	<-ctx.Done()
	stop()
	log.Printf("Shutting down: stopping servers so worlds are saved (press Ctrl+C again to force)")
	sctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	force := make(chan os.Signal, 1)
	signal.Notify(force, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-force
		cancel()
	}()
	srv.Shutdown(sctx)
	mgr.StopAll(sctx)
	log.Printf("Bye")
}

func newSetupCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 8)
	rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

func resetPassword(args []string) {
	fs := flag.NewFlagSet("reset-password", flag.ExitOnError)
	dataDir := fs.String("data", envOr("MCPANEL_DATA", "./data"), "data folder")
	fs.Parse(args)
	st, err := store.Open(*dataDir)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintln(os.Stderr, "Make sure McPanel is stopped first, or it may overwrite the change.")
	pw := readPassword("New password: ")
	if len(pw) < 8 {
		log.Fatal("passwords need at least 8 characters")
	}
	if readPassword("Repeat password: ") != pw {
		log.Fatal("the passwords don't match")
	}
	twoFactor := st.Settings().TwoFactor()
	err = st.UpdateSettings(func(s *store.Settings) {
		s.PasswordHash = auth.HashPassword(pw)
		s.DisableTwoFactor()
		// Lets someone locked out by the HTTPS proxy setting back in over HTTP.
		s.BehindHTTPSProxy = false
		if s.Username == "" {
			s.Username = store.DefaultUsername
		}
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Password changed. Sign in as %q.\n", st.Settings().Username)
	if twoFactor {
		fmt.Println("Two-factor sign-in has been turned off; turn it back on in Settings.")
	}
	fmt.Println("\"Behind an HTTPS proxy\" has been turned off; turn it back on in Settings if you use one.")
}

// stdin is shared so piped input with several lines isn't lost between reads.
var stdin = bufio.NewReader(os.Stdin)

func readPassword(prompt string) string {
	fmt.Fprint(os.Stderr, prompt)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			log.Fatal(err)
		}
		return string(b)
	}
	line, _ := stdin.ReadString('\n')
	return strings.TrimRight(line, "\r\n")
}
