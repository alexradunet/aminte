// Command lifelog-web is the browser of lifelog: the front door of the HTML interface. Run it as `lifelog serve --with
// web`, which gives it the address of the server, or as `lifelog web` on a life.db that exists. See README.md.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"lifelog-web/web"
	"lifelog/app"
)

const (
	// defaultListen is the address of the front door when LIFELOG_WEB_LISTEN is not set.
	defaultListen = "127.0.0.1:7780"
	// headerTimeout bounds the wait for the headers of a request. A body has no bound as a whole: an upload of a
	// large original on a slow link is slow, and the core bounds it.
	headerTimeout = 10 * time.Second
	idleTimeout   = 2 * time.Minute
	maxHeader     = 64 << 10
	// drainTimeout is how long the front door waits for the requests in progress after an interrupt. `lifelog serve`
	// waits for the program for 10 seconds (extensions/README.md, "Exit and signals").
	drainTimeout = 8 * time.Second
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	log.SetFlags(log.LstdFlags)
	log.SetPrefix("lifelog-web: ")
	err := run(ctx, os.Getenv, func(addr string) { log.Printf("the browser is on http://%s", addr) })
	stop()
	if err == nil || errors.Is(err, context.Canceled) {
		return // an interrupt is the owner's word
	}
	fmt.Fprintln(os.Stderr, "lifelog-web:", err)
	var config *configError
	if errors.As(err, &config) {
		os.Exit(app.ExitOwnerActs) // the owner must act first: `serve` does not start the program again
	}
	os.Exit(1)
}

// configError is a refusal of the environment. Another run would refuse the same, so the owner acts first.
type configError struct{ msg string }

func (e *configError) Error() string { return e.msg }

// run serves the browser until ctx ends. started is called with the address of the front door once it listens.
//
// The environment is LIFELOG_URL (the address of the core; required), LIFELOG_TOKEN (the owner's token; required: it is
// the login of the browser and the bearer token of the core), LIFELOG_WEB_LISTEN (a loopback address, 127.0.0.1:7780 by
// default), LIFELOG_WEB_ORIGINS (the origins that the guard of the writes trusts, separated by commas or spaces),
// LIFELOG_WEB_STATE (the state folder; os.UserConfigDir()/lifelog-web by default) and LIFELOG_LISTEN (the loopback
// address where the catalog of this program is served, which `lifelog serve --with web` sets; without it there is no
// catalog).
//
// Two goroutines serve: the front door and the catalog. Each has one owner, which is this function. When either ends,
// for an error or for the end of ctx, the context of both ends, both drain, and this function returns when both are done.
func run(ctx context.Context, getenv func(string) string, started func(addr string)) error {
	base := getenv("LIFELOG_URL")
	if base == "" {
		// There is no default: a guess such as 127.0.0.1:7777 may be the server of another file (extensions/README.md).
		return &configError{"no LIFELOG_URL: run it as `lifelog serve --with web`, or as `lifelog web` on a life.db that exists"}
	}
	token := getenv("LIFELOG_TOKEN")
	if token == "" {
		// The token is the login of the browser. The core hands it to a child of `serve --public`.
		return &configError{"no LIFELOG_TOKEN: it is the login of the browser. `lifelog serve --public --with web` gives it; for `lifelog web`, set it in the environment"}
	}
	state, err := stateDir(getenv)
	if err != nil {
		return &configError{"the state folder: " + err.Error()}
	}
	browser, err := web.New(web.Options{CoreURL: base, Token: token, Origins: strings.FieldsFunc(getenv("LIFELOG_WEB_ORIGINS"), isSeparator), StateDir: state})
	var fault *fs.PathError
	switch {
	case errors.As(err, &fault): // a fault of the file system (the settings cannot be read now) may pass: serve starts web again
		return err
	case err != nil: // a value that the owner must change: a bad token, an origin, a settings file that is not JSON
		return &configError{err.Error()}
	}
	addr := getenv("LIFELOG_WEB_LISTEN")
	if addr == "" {
		addr = defaultListen
	}
	if err := checkLoopback(addr); err != nil {
		return &configError{"LIFELOG_WEB_LISTEN: " + err.Error()}
	}
	catalog := getenv("LIFELOG_LISTEN") // app.Listen reads the same variable, and checks it again
	if catalog != "" {
		if err := checkLoopback(catalog); err != nil {
			return &configError{"LIFELOG_LISTEN: " + err.Error()}
		}
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	started(ln.Addr().String())

	ctx, stop := context.WithCancel(ctx)
	defer stop()
	var door, cat error
	var wg sync.WaitGroup
	wg.Go(func() { door = serve(ctx, ln, browser); stop() })
	if catalog != "" {
		wg.Go(func() { cat = app.Listen(ctx, browser.Catalog()); stop() })
	}
	wg.Wait()
	return errors.Join(door, cat)
}

// isSeparator splits LIFELOG_WEB_ORIGINS: a comma or a space.
func isSeparator(r rune) bool { return r == ',' || unicode.IsSpace(r) }

// stateDir is the state folder: LIFELOG_WEB_STATE, else os.UserConfigDir()/lifelog-web. The unit of the service lists the
// default in ReadWritePaths (extensions/README.md, "Run by serve").
func stateDir(getenv func(string) string) (string, error) {
	if dir := getenv("LIFELOG_WEB_STATE"); dir != "" {
		return dir, os.MkdirAll(dir, 0o700)
	}
	return app.StateDir("web")
}

// checkLoopback refuses an address that is not host:port with a loopback IP. The proxy of the owner reaches the browser
// through the loopback, and nothing else may: the door serves plain HTTP, and TLS ends at the proxy.
func checkLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("%q is not host:port: %w", addr, err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%q: the browser listens on a loopback address only", addr)
	}
	return nil
}

// serve answers on ln until ctx ends. Then it waits for the requests in progress, drainTimeout at most.
func serve(ctx context.Context, ln net.Listener, h http.Handler) error {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: headerTimeout, IdleTimeout: idleTimeout, MaxHeaderBytes: maxHeader}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case err := <-done: // the server failed before the end of ctx
		return err
	case <-ctx.Done():
	}
	drain, cancel := context.WithTimeout(context.Background(), drainTimeout) // ctx has ended: the drain needs its own
	defer cancel()
	if err := srv.Shutdown(drain); err != nil {
		return err
	}
	if err := <-done; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
