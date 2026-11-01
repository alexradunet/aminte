// Command lifelog-gphotos is a local web page. The owner picks photos in Google Photos, and the page keeps one photo
// as the cover of each day that has none. It is a client of the lifelog HTTP API. See README.md.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"lifelog-googlephotos/google"
	"lifelog-googlephotos/lifelog"
	"lifelog-googlephotos/review"
	"lifelog/app"
)

func main() {
	app.Main("gphotos", func(ctx context.Context) error { return run(ctx, os.Args[1:]) })
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("lifelog-gphotos", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:7790", "address of the review page (a loopback address)")
	// The address is $LIFELOG_URL, which `lifelog gphotos` sets, or --url. There is no default: app.API refuses an
	// empty address.
	base := fs.String("url", os.Getenv("LIFELOG_URL"), "address of lifelog (default $LIFELOG_URL); $LIFELOG_TOKEN is sent as a bearer token when it is set")
	credentials := fs.String("credentials", "", "JSON of the Desktop OAuth client from Google Cloud (default: credentials.json beside the token file)")
	zoneName := fs.String("zone", "", "IANA zone that gives the day of a photo (default: the local zone)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	api, err := app.API(*base, lifelog.Source)
	if err != nil {
		return err
	}
	if _, err := review.LoopbackAddr(*addr); err != nil {
		return err
	}
	zone := time.Local
	if *zoneName != "" {
		var err error
		if zone, err = time.LoadLocation(*zoneName); err != nil {
			return fmt.Errorf("zone: %w", err)
		}
	}
	tokenPath, err := google.DefaultTokenPath()
	if err != nil {
		return err
	}
	if *credentials == "" {
		*credentials = filepath.Join(filepath.Dir(tokenPath), "credentials.json")
	}
	cfg, err := google.LoadCredentials(*credentials)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	auth := &google.Auth{Config: cfg, HTTP: client, TokenPath: tokenPath}
	srv := review.New(review.Config{
		Auth:    auth,
		Picker:  &google.Picker{Base: google.DefaultPickerBase, HTTP: client, Tokens: auth},
		Lifelog: lifelog.New(api),
		Zone:    zone,
	})
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	log.Printf("review page on http://%s", ln.Addr())
	errc := make(chan error, 1)
	go func() { errc <- httpSrv.Serve(ln) }() // ends when Shutdown runs below; its error is read from errc
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdown); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
