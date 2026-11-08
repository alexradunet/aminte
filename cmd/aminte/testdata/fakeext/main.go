// Fakeext is the extension of the tests of cmd/aminte: it prints what aminte gave it, one item per line.
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
)

func main() {
	if dir := os.Getenv("FAKE_DIR"); dir != "" {
		runByServe(dir)
		return
	}
	args := os.Args[1:]
	fmt.Println("self=" + os.Args[0])
	fmt.Println("args=" + strings.Join(args, "|"))
	for _, v := range []string{"AMINTE_URL", "AMINTE_EXE", "AMINTE_DB"} {
		fmt.Println(v + "=" + os.Getenv(v))
	}
	if u := os.Getenv("AMINTE_URL"); u != "" {
		resp, err := http.Get(u + "/actions")
		if err != nil {
			fmt.Println("status=error")
		} else {
			resp.Body.Close()
			fmt.Println("status=" + strconv.Itoa(resp.StatusCode))
		}
	}
	if len(args) > 0 && strings.HasPrefix(args[0], "exit=") {
		n, _ := strconv.Atoi(strings.TrimPrefix(args[0], "exit="))
		os.Exit(n)
	}
	if len(args) > 0 && args[0] == "wait" {
		// An extension that ends cleanly on an interrupt: it writes "ready" beside the file, then waits for the signal.
		interrupted := make(chan os.Signal, 1)
		signal.Notify(interrupted, os.Interrupt)
		if err := os.WriteFile(filepath.Join(filepath.Dir(os.Getenv("AMINTE_DB")), "ready"), nil, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		<-interrupted
		fmt.Println("interrupted=yes")
	}
}

// runByServe is the extension as `aminte serve --with` runs it. FAKE_DIR holds NAME.mode, what to do (NAME is the
// base name of the program), and NAME.log, one line for each start: the address, the token, the bytes of stdin and the
// status of GET / on that address. The modes: "exit=N" ends at once with N; "exit-once=N" does so on the first start
// only; "wait" waits for the interrupt and ends with 0; "capture" does so after it posted a capture to the server
// and ends with 0 when the answer is 200 (the server must still serve while the extension ends).
func runByServe(dir string) {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	modeBytes, err := os.ReadFile(filepath.Join(dir, name+".mode"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	mode := strings.TrimSpace(string(modeBytes))
	url, token := os.Getenv("AMINTE_URL"), os.Getenv("AMINTE_TOKEN")
	in, _ := io.ReadAll(os.Stdin)
	call := func(method, path, body string) int {
		req, err := http.NewRequest(method, url+path, strings.NewReader(body))
		if err != nil {
			return -1
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return -1
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	logPath := filepath.Join(dir, name+".log")
	prior, _ := os.ReadFile(logPath)
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt)
	fmt.Fprintf(f, "url=%s token=%s stdin=%d args=%d status=%d\n", url, token, len(in), len(os.Args)-1, call("GET", "/", ""))
	f.Close()
	switch n, _ := strconv.Atoi(mode[strings.Index(mode, "=")+1:]); {
	case strings.HasPrefix(mode, "exit="):
		os.Exit(n)
	case strings.HasPrefix(mode, "exit-once=") && len(prior) == 0:
		os.Exit(n)
	}
	<-interrupted
	if mode == "capture" && call("POST", "/days/today/capture", "text=written+while+the+child+ended") != 200 {
		os.Exit(1)
	}
}
