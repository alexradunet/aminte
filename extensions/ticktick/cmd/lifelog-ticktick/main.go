// Command lifelog-ticktick is the TickTick connected app of lifelog. Run it as `lifelog ticktick ...`, or under
// `lifelog serve --with ticktick`, which gives it its catalog address. See README.md.
package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"lifelog-ticktick/connector"
	"lifelog-ticktick/ticktick"
	"lifelog/app"
	"lifelog/siren"
)

const usage = `usage:
  lifelog ticktick login [--client-id ID] [--client-secret SECRET] [--port 8765]
  lifelog ticktick sync [--url URL]
  lifelog serve --with ticktick     (the program serves its catalog and polls)`

// usageError is a refusal of the line: the owner acts first (app.OwnerActs), so serve does not start the program again.
// It prints the usage on stderr. The error holds the message only, because app.Main prints it as the one last line
// "lifelog-ticktick: err", under the usage.
func usageError(stderr io.Writer, msg string) error {
	fmt.Fprintln(stderr, usage)
	return app.OwnerActs(errors.New(msg))
}

// The exit status is the one of app.Main: 78 for a usage error and for a login that is missing (connector.ErrLoginAgain),
// 1 for any other error (serve starts the program again).
func main() {
	log.SetFlags(log.LstdFlags)
	log.SetPrefix("lifelog-ticktick: ")
	app.Main(connector.Name, func(ctx context.Context) error {
		return run(ctx, os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr)
	})
}

func run(ctx context.Context, args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		if getenv("LIFELOG_LISTEN") == "" {
			return usageError(stderr, "no command, and no LIFELOG_LISTEN")
		}
		return serve(ctx, getenv)
	}
	rest := args[1:]
	switch args[0] {
	case "login":
		return login(ctx, rest, stdin, stdout, stderr)
	case "sync":
		return syncOnce(ctx, rest, getenv, stdout, stderr)
	}
	return usageError(stderr, fmt.Sprintf("unknown argument %q", args[0]))
}

// parse reads the flags of a command and refuses a word that is not a flag.
func parse(fs *flag.FlagSet, args []string, stderr io.Writer) error {
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return app.OwnerActs(err)
	}
	if fs.NArg() > 0 {
		return usageError(stderr, fmt.Sprintf("unexpected argument %q", fs.Arg(0)))
	}
	return nil
}

// connect makes the clients from the files of the state folder. It is ErrLoginAgain without a token.
func connect(base string) (*connector.Poller, string, connector.Config, error) {
	dir, err := connector.Dir()
	if err != nil {
		return nil, "", connector.Config{}, err
	}
	cfg, err := connector.LoadConfig(dir)
	if err != nil {
		return nil, "", cfg, err
	}
	token, err := connector.LoadToken(dir)
	if err != nil {
		return nil, "", cfg, err
	}
	ll, err := app.API("lifelog ticktick sync", base, siren.ImportPrefix+connector.Name)
	if err != nil {
		return nil, "", cfg, err
	}
	tt := &ticktick.Client{Base: cfg.API, Token: token}
	p, err := connector.NewPoller(tt, ll, dir)
	return p, dir, cfg, err
}

func serve(ctx context.Context, getenv func(string) string) error {
	p, _, cfg, err := connect(getenv("LIFELOG_URL"))
	if err != nil {
		return err
	}
	every, err := cfg.Every()
	if err != nil {
		return err
	}
	lists, err := p.TT.Projects(ctx)
	if err != nil {
		if se := new(ticktick.StatusError); errors.As(err, &se) && se.Status == http.StatusUnauthorized {
			return connector.ErrLoginAgain
		}
		return err
	}
	return connector.Serve(ctx, p, lists, every)
}

func syncOnce(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	base := fs.String("url", getenv("LIFELOG_URL"), "address of lifelog (default $LIFELOG_URL)")
	if err := parse(fs, args, stderr); err != nil {
		return err
	}
	p, _, _, err := connect(*base)
	if err != nil {
		return err
	}
	n, err := p.Poll(ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %d entries\n", n)
	return nil
}

func login(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	id := fs.String("client-id", "", "the client id of your app in the TickTick developer console")
	secret := fs.String("client-secret", "", "the client secret of your app")
	port := fs.Int("port", 8765, "the port of the redirect http://127.0.0.1:PORT/callback")
	if err := parse(fs, args, stderr); err != nil {
		return err
	}
	if *port < 1 || *port > 65535 {
		return usageError(stderr, "--port must be a port number")
	}
	in := bufio.NewReader(stdin)
	for _, f := range []struct {
		value *string
		ask   string
	}{{id, "client id: "}, {secret, "client secret: "}} {
		if *f.value != "" {
			continue
		}
		fmt.Fprint(stdout, f.ask)
		line, err := in.ReadString('\n')
		if err != nil && line == "" {
			return fmt.Errorf("read the answer: %w", err)
		}
		*f.value = strings.TrimSpace(line) // the secret is not printed back
	}
	dir, err := connector.Dir()
	if err != nil {
		return err
	}
	cfg, err := connector.LoadConfig(dir)
	if err != nil {
		return err
	}
	o := ticktick.OAuth{ClientID: *id, ClientSecret: *secret, AuthorizeURL: cfg.AuthorizeURL, TokenURL: cfg.TokenURL,
		RedirectURI: "http://127.0.0.1:" + strconv.Itoa(*port) + "/callback"}
	code, err := waitForCode(ctx, o, *port, in, stdout)
	if err != nil {
		return err
	}
	token, err := o.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("exchange the code: %w", err)
	}
	if err := connector.SaveToken(dir, token); err != nil {
		return err
	}
	if err := connector.WriteDefaultConfig(dir); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "logged in; now `lifelog ticktick sync --url http://127.0.0.1:7777`, or add --with ticktick to serve")
	return nil
}

// waitForCode prints the authorize link and waits for the redirect on the loopback, or for the address of the redirect
// pasted on the terminal: when this program runs on a server, the browser lands on the 127.0.0.1 of the owner's own
// machine, and the owner brings the address here. It checks state in both.
func waitForCode(ctx context.Context, o ticktick.OAuth, port int, in *bufio.Reader, stdout io.Writer) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	state := hex.EncodeToString(raw)
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		return "", fmt.Errorf("listen for the redirect: %w", err)
	}
	got := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state || q.Get("code") == "" {
			http.Error(w, "This is not the answer to the login that waits. Try again.", http.StatusBadRequest)
			return
		}
		fmt.Fprintln(w, "Logged in. You can close this page.")
		select {
		case got <- q.Get("code"):
		default:
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.Serve(ln) }()
	defer func() { // the owner of the goroutine: stop it and wait
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
		<-done
	}()
	pasted, pasteErr := make(chan string, 1), make(chan error, 1)
	go func() { // a read of the terminal cannot be stopped: this goroutine ends with the process, right after the login
		line, _ := in.ReadString('\n')
		if line = strings.TrimSpace(line); line == "" { // the end of the input, or an empty line: the redirect is the way
			return
		}
		code, err := pastedCode(line, state)
		if err != nil {
			pasteErr <- err
			return
		}
		pasted <- code
	}()
	fmt.Fprintf(stdout, "Open this address and agree:\n%s\n", o.AuthorizeLink(state))
	fmt.Fprintln(stdout, "If the browser is on another machine, paste the address it lands on here and press Enter.")
	select {
	case code := <-got:
		return code, nil
	case code := <-pasted:
		return code, nil
	case err := <-pasteErr:
		return "", err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// pastedCode is the code in a line the owner pasted: the address of the redirect (its state must be the one of this
// login), or the code alone.
func pastedCode(line, state string) (string, error) {
	if !strings.Contains(line, "code=") {
		if strings.ContainsAny(line, " \t?&=") {
			return "", errors.New("the pasted line is not the address of the redirect and not a code")
		}
		return line, nil
	}
	u, err := url.Parse(line)
	if err != nil {
		return "", fmt.Errorf("the pasted address: %w", err)
	}
	q := u.Query()
	if q.Get("state") != state {
		return "", errors.New("the pasted address is not the answer to this login: its state differs")
	}
	if q.Get("code") == "" {
		return "", errors.New("the pasted address has no code")
	}
	return q.Get("code"), nil
}
