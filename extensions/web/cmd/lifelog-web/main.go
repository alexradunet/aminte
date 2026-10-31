// Command lifelog-web is the browser of lifelog: the front door of the HTML interface. Run it as `lifelog serve --with
// web`, which gives it the address of the server, or as `lifelog web` on a life.db that exists. See README.md.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
// The environment is LIFELOG_URL (the address of the core; required), LIFELOG_TOKEN (the bearer token of the core, when
// the core checks one) and LIFELOG_WEB_LISTEN (a loopback address, 127.0.0.1:7780 by default).
func run(ctx context.Context, getenv func(string) string, started func(addr string)) error {
	base := getenv("LIFELOG_URL")
	if base == "" {
		// There is no default: a guess such as 127.0.0.1:7777 may be the server of another file (extensions/README.md).
		return &configError{"no LIFELOG_URL: run it as `lifelog serve --with web`, or as `lifelog web` on a life.db that exists"}
	}
	browser, err := web.New(web.Options{CoreURL: base, Token: getenv("LIFELOG_TOKEN")})
	if err != nil {
		return &configError{err.Error()}
	}
	addr := getenv("LIFELOG_WEB_LISTEN")
	if addr == "" {
		addr = defaultListen
	}
	if err := checkLoopback(addr); err != nil {
		return &configError{"LIFELOG_WEB_LISTEN: " + err.Error()}
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	started(ln.Addr().String())
	// The next part of the extension serves the catalog of web on LIFELOG_LISTEN (app.Listen) beside this door, and
	// ends both when either ends.
	return serve(ctx, ln, browser)
}

// checkLoopback refuses an address that is not host:port with a loopback IP. The proxy of the owner reaches the
// browser through the loopback, and nothing else may: the door has no login of its own yet.
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
