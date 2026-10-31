package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"lifelog/internal/api"
	"lifelog/internal/db"
)

// tokenOf is the token of the server: the first line of the file --token-file names (one trailing line break is cut),
// else $LIFELOG_TOKEN. "" means that the server needs no token. A token must pass api.CheckToken, so a file that is
// empty is refused: the owner asked for a token and none is there. So is an empty path (--token-file=), whatever
// $LIFELOG_TOKEN holds: the owner named a token file, and no file is not a reason to drop the check. The checks are
// made before anything opens.
func tokenOf(o opts) (string, error) {
	token := o.token
	if o.serve.tokenFile == "" && slices.Contains(o.given, "--token-file") {
		return "", errors.New("--token-file needs a path")
	}
	if o.serve.tokenFile != "" {
		b, err := os.ReadFile(o.serve.tokenFile)
		if err != nil {
			return "", fmt.Errorf("token file: %w", err)
		}
		token = strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
	} else if token == "" {
		return "", nil
	}
	if err := api.CheckToken(token); err != nil {
		return "", fmt.Errorf("token: %w", err)
	}
	return token, nil
}

// serveHTTP owns shutdown until admitted requests finish or the drain deadline
// expires. The caller must not close their database while Shutdown is still running.
func serveHTTP(ctx context.Context, srv *http.Server, listener net.Listener, drainTimeout time.Duration) error {
	stopped := make(chan struct{})
	drained := make(chan struct{})
	var drainErr error
	go func() {
		defer close(drained)
		select {
		case <-ctx.Done():
		case <-stopped:
		}
		// An interrupt stops new work but lets admitted HTTP requests finish. This
		// separate, bounded lifetime preserves command context values and is joined below.
		drainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), drainTimeout)
		defer cancel()
		err := srv.Shutdown(drainCtx)
		if err != nil {
			// Expiry closes connections and cancels their request contexts instead of
			// leaving a blocked upload or reader alive after the server has returned.
			err = errors.Join(err, srv.Close())
		}
		drainErr = err
	}()
	err := srv.Serve(listener)
	close(stopped)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	<-drained
	return errors.Join(err, drainErr)
}

// prepareServe reads the token and the address before anything opens: a wrong token, a wrong address or a snapshot
// folder that is not one refuses the command with life.db untouched. It gives the line with the address checked and
// the token read.
func prepareServe(e env, o opts) (opts, error) {
	var err error
	if o.serve.token, err = tokenOf(o); err != nil {
		return o, err
	}
	if o.serve.addr, err = api.LoopbackAddress(o.serve.addr); err != nil {
		return o, err
	}
	if len(o.serve.with) > 0 {
		exe, err := program(e)
		if err != nil {
			return o, err
		}
		for i, name := range o.serve.with {
			if slices.Contains(o.serve.with[:i], name) { // two flags that name one thing twice are refused
				return o, fmt.Errorf("--with %s is given twice: one child runs an extension", name)
			}
			path, err := findExtension(exe, name)
			if err != nil {
				return o, fmt.Errorf("--with %s: %w", name, err)
			}
			o.serve.withPaths = append(o.serve.withPaths, path)
		}
	}
	if o.url != "" {
		return o, errors.New("serve opens the file itself: drop --url")
	}
	if o.snapshots != "" {
		if _, err := db.CheckSnapshotDir(o.snapshots); err != nil {
			return o, err
		}
	}
	return o, nil
}

// serve is lifelog serve: the API on a loopback socket until an interrupt drains it. serve builds the handler of its
// listener itself, from the outside in: NetworkAuthority (a request to a Host that is not this address is 403), then
// the bearer check when the server has a token (a request without it is 401), then the API. So no code in another
// function decides what guards the listener.
func serve(ctx context.Context, e env, args []string) error {
	if len(args) != 0 {
		return errors.New(e.cmd.synopsis())
	}
	o := e.o
	listener, err := net.Listen("tcp", o.serve.addr)
	if err != nil {
		return err
	}
	defer listener.Close()
	// The apps of --with: known before the listener opens, so a request to /with/NAME before the child is up is 503.
	apps := api.NewApps()
	for _, name := range o.serve.with {
		apps.Expect(name)
	}
	handler, note := http.Handler(api.NewWith(e.store, apps)), ""
	if o.serve.token != "" {
		if handler, err = api.RequireToken(o.serve.token, handler); err != nil {
			return err
		}
		note = " (token required)"
	}
	if handler, err = api.NetworkAuthority(listener.Addr(), handler); err != nil {
		return err
	}
	srv := &http.Server{Addr: listener.Addr().String(), Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	// The file's own user_version, not the constant of this program: the line shows which schema the process serves.
	var userVersion int64
	if err := e.store.DB.R.QueryRowContext(ctx, "PRAGMA user_version").Scan(&userVersion); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	fmt.Fprintf(e.stderr, "lifelog: serving %s on http://%s%s (version %s, user_version %d)\n", o.db, listener.Addr(), note, version, userVersion)
	// The children end before the server drains: an extension may still write while it ends (extensions/README.md).
	serveCtx, waitWith := startWith(ctx, e, listener.Addr(), apps)
	err = serveHTTP(serveCtx, srv, listener, 10*time.Second)
	waitWith()
	// After the drain, before the database closes: the log shows that the stop was clean, or why it was not.
	if err != nil {
		fmt.Fprintf(e.stderr, "lifelog: stopped serving %s with an error: %v\n", o.db, err)
	} else {
		fmt.Fprintf(e.stderr, "lifelog: stopped serving %s\n", o.db)
	}
	return err
}
