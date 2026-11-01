// Command lifelog-web is the browser of lifelog: the front door of the HTML interface. Run it as `lifelog serve
// --token-file F --with web`, which gives it the address and the token of the server, or as `lifelog web` on a life.db
// that exists. See README.md.
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
	"strings"
	"sync"
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

// The exit status is the one of app.Main: 78 for a refusal of the environment (configError), 1 for any other error.
func main() {
	log.SetFlags(log.LstdFlags)
	log.SetPrefix("lifelog-web: ")
	app.Main("web", func(ctx context.Context) error {
		return run(ctx, os.Getenv, func(addr string) { log.Printf("the browser is on http://%s", addr) })
	})
}

// configError is a refusal of the environment. Another run would refuse the same, so the owner acts first
// (app.OwnerActs): `serve` does not start the program again.
func configError(msg string) error { return app.OwnerActs(errors.New(msg)) }

// run serves the browser until ctx ends. started is called with the address of the front door once it listens.
//
// The environment is LIFELOG_URL (the address of the core; required), LIFELOG_TOKEN (the owner's token; required: it is
// the login of the browser and the bearer token of the core), LIFELOG_WEB_LISTEN (a loopback address, 127.0.0.1:7780 by
// default), LIFELOG_WEB_ORIGINS (the origins that the guard of the writes trusts, separated by commas or spaces),
// and LIFELOG_LISTEN (the loopback address where the catalog of this program is served, which `lifelog serve --with web`
// sets; without it there is no catalog). The state folder is os.UserConfigDir()/lifelog-web; no variable names it.
//
// The program does not use app.API: it draws the answers of the core as they are, with a transport of its own (no proxy of
// the environment, no compression), and it keeps the token that it checks as the login of the browser.
//
// Two goroutines serve: the front door and the catalog. Each has one owner, which is this function. When either ends,
// for an error or for the end of ctx, the context of both ends, both drain, and this function returns when both are done.
func run(ctx context.Context, getenv func(string) string, started func(addr string)) error {
	base := getenv("LIFELOG_URL")
	if base == "" {
		// There is no default: a guess such as 127.0.0.1:7777 may be the server of another file (extensions/README.md).
		return configError("no LIFELOG_URL: run it as `lifelog serve --with web`, or as `lifelog web` on a life.db that exists")
	}
	token := getenv("LIFELOG_TOKEN")
	if token == "" {
		// The token is the login of the browser. A server that has a token (`serve --token-file`) hands it to its children.
		return configError("no LIFELOG_TOKEN: it is the login of the browser. `lifelog serve --token-file F --with web` gives it; for `lifelog web`, set it in the environment")
	}
	// The state folder is os.UserConfigDir()/lifelog-web. A unit with ProtectHome=read-only must list it in
	// ReadWritePaths, else the program cannot write its state (extensions/README.md, "Run by serve").
	state, err := app.StateDir("web")
	if err != nil {
		return configError("the state folder: " + err.Error())
	}
	browser, err := web.New(web.Options{CoreURL: base, Token: token, Origins: strings.FieldsFunc(getenv("LIFELOG_WEB_ORIGINS"), isSeparator), StateDir: state})
	var fault *fs.PathError
	switch {
	case errors.As(err, &fault): // a fault of the file system (the settings cannot be read now) may pass: serve starts web again
		return err
	case err != nil: // a value that the owner must change: a bad token, an origin, a settings file that is not JSON
		return configError(err.Error())
	}
	addr := getenv("LIFELOG_WEB_LISTEN")
	if addr == "" {
		addr = defaultListen
	}
	if err := checkLoopback(addr); err != nil {
		return configError("LIFELOG_WEB_LISTEN: " + err.Error())
	}
	catalog := getenv("LIFELOG_LISTEN") // app.Listen reads the same variable, and checks it again
	if catalog != "" {
		if err := checkLoopback(catalog); err != nil {
			return configError("LIFELOG_LISTEN: " + err.Error())
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
