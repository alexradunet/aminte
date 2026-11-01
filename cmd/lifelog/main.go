// Lifelog is the writing application of a life.db (docs/guides/building-a-writer.md). It is one binary that serves
// the API and the CLI through one handler (internal/api). The MCP server is the extension lifelog-mcp.
//
// The table commands in this file is the one list of its commands: the usage, the dispatch and the check of the
// flags read it, and "lifelog help" prints it. "lifelog actions" lists the actions of the catalog, and "lifelog do"
// runs any one of them. README.md gives the usual command lines.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"lifelog/client"
	"lifelog/internal/api"
	"lifelog/internal/core"
	"lifelog/internal/db"
	"lifelog/siren"
)

const version = "0.1.0"

// opts are the flags of a command line and its words. A flag may stand anywhere on the line; a command takes the
// global flags and its own (command.flags), and refuses any other.
type opts struct {
	db, url, source, mood, day, snapshots, token string // token: $LIFELOG_TOKEN, of --url and of serve
	human, dbExplicit                            bool
	args                                         []string // the words of the line that are no flag
	given                                        []string // the flags the line named
	serve                                        serveFlags
	file                                         fileFlags
}

// serveFlags are the flags that only serve reads.
type serveFlags struct {
	addr      string
	tokenFile string
	with      []string // the names of the extensions that serve runs (--with)
	token     string   // set by prepareServe: the token of the server, "" for none (tokenOf)
	withPaths []string // set by prepareServe: the program of each --with, in the order of the line
}

// fileFlags are the flags that only file and cover read. --day, which capture reads too, is opts.day.
type fileFlags struct {
	title, text, preview, mime string
	dryRun                     bool
}

// globalFlags are the flags every command takes.
var globalFlags = []string{"--db", "--url", "--source", "--human"}

const globalUsage = `
Global flags, anywhere on the line:
  --db PATH      the life.db (default $LIFELOG_DB, else the life.db beside this program)
  --url URL      talk to a running "lifelog serve" instead of opening the file ($LIFELOG_TOKEN if it has a token)
  --source NAME  the writer recorded on new rows (default cli; lifelog_meta.source)
  --human        print a readable summary instead of JSON
`

// env is what a command runs with: the line, the client (nil for serve, which builds the handler of its listener),
// the store (nil over --url, when a running server is the one), and what the command reads from its process. main
// gives the process's own; a test gives buffers, its own variables and its own clock.
type env struct {
	o          opts
	cmd        *command  // the command of the line, set by runContext: its synopsis is the text of an error of use
	table      []command // the table commands, set by runContext, for the command that lists the shortcuts: a closure in the table cannot name it (an initialization cycle)
	c          *client.Client
	store      *core.Store
	stdin      io.ReadCloser
	stdout     io.Writer
	stderr     io.Writer
	getenv     func(string) string    // LIFELOG_DB and LIFELOG_TOKEN, read by parse
	executable func() (string, error) // the program, beside which life.db lives
	clock      func() time.Time       // the clock of the store
}

// command is one subcommand. The table commands is the one list of them: the usage, the dispatch, the flags a
// command takes, whether an interrupt reaches it and what each shortcut wraps all read it.
type command struct {
	name       string
	gap        bool                              // a blank line before its usage: the start of a group
	usage      string                            // its lines of the usage text; "" for an alias with none of its own
	flags      []string                          // the flags it takes, besides the global ones
	refuses    []string                          // the global flags it does not take: it would ignore them
	setup      bool                              // a setup command of the machine (init): no client, and the OS's interrupt
	wraps      string                            // the catalog action this shortcut runs; "" for a command that is no shortcut
	ownHandler bool                              // it builds the handler of its listener (serve): it gets the store and no client
	prepare    func(e env, o opts) (opts, error) // what it checks before anything opens, and the line it runs with: o, changed or not
	run        func(ctx context.Context, e env, args []string) error
}

// synopsis is the first line of the usage of c, without its indent and its text: the words that an error shows.
func (c command) synopsis() string {
	line, _, _ := strings.Cut(c.usage, "\n")
	line = strings.TrimSpace(line)
	if i := strings.Index(line, "  "); i >= 0 {
		line = line[:i]
	}
	return strings.TrimPrefix(line, "lifelog ")
}

var commands = []command{
	{name: "init", setup: true, gap: true, refuses: []string{"--url", "--source"},
		usage: "  lifelog init [PATH]                 create a new life.db from docs/schema/schema.sql (default: beside this program)",
		run:   initDB},
	{name: "serve", ownHandler: true, refuses: []string{"--human"}, flags: []string{"--addr", "--snapshots", "--token-file", "--with"},
		usage: "  lifelog serve [--addr 127.0.0.1:7777] [--snapshots DIR] [--token-file PATH]\n" +
			"                                      the API: Siren JSON, on a loopback address; a token (--token-file PATH or\n" +
			"                                      $LIFELOG_TOKEN) turns on the bearer check\n" +
			"  lifelog serve ... --with NAME       also run the extension lifelog-NAME as a child, and start it again when it fails",
		prepare: prepareServe, run: serve},
	{name: "get", gap: true,
		usage: "  lifelog get PATH                    fetch a resource, e.g. /, /days/today, /pages/12 (or pages/12: Git Bash rewrites a leading /)",
		run: func(ctx context.Context, e env, args []string) error {
			if len(args) != 1 {
				return errors.New(e.cmd.synopsis())
			}
			return show(e)(e.c.Get(ctx, "/"+strings.TrimPrefix(args[0], "/"))) // "pages/1" too: some shells rewrite a leading /
		}},
	{name: "do",
		usage: "  lifelog do ACTION [field=value...]  run any action of the catalog (lifelog actions)",
		run: func(ctx context.Context, e env, args []string) error {
			if len(args) == 0 {
				return errors.New(e.cmd.synopsis())
			}
			vals := map[string]string{}
			for _, kv := range args[1:] {
				k, v, ok := strings.Cut(kv, "=")
				if !ok {
					return fmt.Errorf("%q is not field=value", kv)
				}
				if _, dup := vals[k]; dup {
					return fmt.Errorf("%s is given twice", k)
				}
				vals[k] = v
			}
			return doAction(ctx, e, args[0], vals)
		}},
	{name: "actions",
		usage: "  lifelog actions                     list the actions: route, CLI line, MCP tool, owner mark, shortcut, fields",
		run: func(ctx context.Context, e env, args []string) error {
			if len(args) != 0 {
				return errors.New(e.cmd.synopsis())
			}
			return listActions(ctx, e)
		}},
	{name: "capture", wraps: "capture", gap: true, flags: []string{"--mood", "--day"},
		usage: "  lifelog capture [TEXT...] [--mood N] [--day YYYY-MM-DD]\n" +
			"                                      add to a day's journal page",
		run: func(ctx context.Context, e env, args []string) error {
			// The day is the server's today unless --day names one: with --url, the server's clock is the one that counts.
			vals := map[string]string{"day": dayOrToday(e.o), "text": strings.Join(args, " ")}
			if e.o.mood != "" {
				vals["mood"] = e.o.mood
			}
			return doAction(ctx, e, e.cmd.wraps, vals)
		}},
	{name: "day", wraps: "open-day",
		usage: "  lifelog day [YYYY-MM-DD]            the day view (default today)",
		run: func(ctx context.Context, e env, args []string) error {
			if len(args) > 1 {
				return errors.New(e.cmd.synopsis())
			}
			vals := map[string]string{} // no day is today: open-day reads the clock of the API
			if len(args) == 1 {
				vals["day"] = args[0]
			}
			return doAction(ctx, e, e.cmd.wraps, vals)
		}},
	{name: "page", wraps: "find",
		usage: "  lifelog page TITLE                  a page by its title",
		run: func(ctx context.Context, e env, args []string) error {
			return doAction(ctx, e, e.cmd.wraps, map[string]string{"title": strings.Join(args, " ")})
		}},
	{name: "search", wraps: "search",
		usage: "  lifelog search QUERY...             full-text search",
		run: func(ctx context.Context, e env, args []string) error {
			return doAction(ctx, e, e.cmd.wraps, map[string]string{"q": strings.Join(args, " ")})
		}},
	{name: "rename", wraps: "rename",
		usage: "  lifelog rename PAGE-ID NEW-TITLE    rename a named object, retaining its id and old-name aliases",
		run: func(ctx context.Context, e env, args []string) error {
			if len(args) != 2 {
				return errors.New(e.cmd.synopsis())
			}
			return doAction(ctx, e, e.cmd.wraps, map[string]string{"id": args[0], "title": args[1]})
		}},
	{name: "file", wraps: "add-file", flags: []string{"--title", "--text", "--preview", "--mime", "--day", "--dry-run"},
		usage: "  lifelog file PATH [--title T] [--text FILE] [--preview PICTURE] [--mime TYPE] [--day D] [--dry-run]\n" +
			"                                      keep a file: the add-file action (lifelog actions says the rest)",
		run: func(ctx context.Context, e env, args []string) error { return keepFile(ctx, e, args) }},
	{name: "cover", wraps: "set-cover", flags: []string{"--title", "--preview"},
		usage: "  lifelog cover PAGE-ID [PHOTO] [--title T] [--preview PICTURE]\n" +
			"                                      set a page's cover: the set-cover action (lifelog actions says the rest)",
		run: func(ctx context.Context, e env, args []string) error { return setCover(ctx, e, args) }},
	{name: "snapshot", wraps: "snapshot", flags: []string{"--snapshots"},
		usage: "  lifelog snapshot [--snapshots DIR]  a dated copy of life.db (life-YYYY-MM-DD.db, beside it or in DIR),\n" +
			"                                      then its restore check (docs/cookbook/take-a-snapshot.md); with --url the\n" +
			"                                      server's folder (serve --snapshots), and --snapshots is refused",
		prepare: func(_ env, o opts) (opts, error) {
			if o.url != "" && o.snapshots != "" {
				return o, errors.New("snapshot over --url lands in the server's folder (serve --snapshots): drop --snapshots")
			}
			if o.url == "" && o.snapshots != "" {
				// Refused before the database is opened: a mistaken folder leaves life.db untouched.
				if _, err := db.CheckSnapshotDir(o.snapshots); err != nil {
					return o, err
				}
			}
			return o, nil
		},
		run: func(ctx context.Context, e env, args []string) error {
			if len(args) != 0 {
				return errors.New(e.cmd.synopsis())
			}
			return doAction(ctx, e, e.cmd.wraps, nil)
		}},
}

// commandNamed is the command of a name, nil for none.
func commandNamed(name string) *command {
	for i := range commands {
		if commands[i].name == name {
			return &commands[i]
		}
	}
	return nil
}

// usageText is the usage, assembled from the table so that a command cannot be missing from it.
func usageText() string {
	var b strings.Builder
	b.WriteString("lifelog — the writer of a life.db\n")
	for _, c := range commands {
		if c.usage == "" {
			continue
		}
		if c.gap {
			b.WriteString("\n")
		}
		b.WriteString(c.usage)
		b.WriteString("\n")
	}
	b.WriteString("\n  lifelog NAME [ARGS...]              runs the extension lifelog-NAME beside this program or on PATH (extensions/README.md)\n")
	b.WriteString(globalUsage)
	return b.String()
}

// parse takes flags anywhere on the line (an old CLI ignored --db after the subcommand). A flag the command does not
// take is refused, never ignored. getenv gives LIFELOG_DB and LIFELOG_TOKEN.
func parse(argv []string, getenv func(string) string) (opts, error) {
	o := opts{db: getenv("LIFELOG_DB"), token: getenv("LIFELOG_TOKEN"), source: "cli"}
	o.serve.addr = "127.0.0.1:7777"
	vals := map[string]*string{"--db": &o.db, "--url": &o.url, "--source": &o.source, "--addr": &o.serve.addr,
		"--mood": &o.mood, "--day": &o.day,
		"--title": &o.file.title, "--text": &o.file.text, "--preview": &o.file.preview, "--mime": &o.file.mime,
		"--token-file": &o.serve.tokenFile, "--snapshots": &o.snapshots}
	bools := map[string]*bool{"--human": &o.human, "--dry-run": &o.file.dryRun}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if len(o.args) > 0 && o.args[0] != "help" && commandNamed(o.args[0]) == nil {
			// The extension mode (extensions/README.md). The words after the name belong to the extension, and its flags
			// may have any name. So lifelog reads only the two flags that choose the database, and passes the rest as it
			// is, in its order.
			if a == "--" { // from here on, even --db and --url are the extension's words, and so is the -- itself
				o.args = append(o.args, argv[i:]...)
				break
			}
			name, val, hasVal := strings.Cut(a, "=")
			if name != "--db" && name != "--url" {
				o.args = append(o.args, a)
				continue
			}
			if !hasVal {
				if i+1 >= len(argv) {
					return o, fmt.Errorf("%s needs a value", name)
				}
				i++
				val = argv[i]
			}
			o.given = append(o.given, name)
			if name == "--db" {
				o.db, o.dbExplicit = val, true
			} else {
				o.url = val
			}
			continue
		}
		if a == "--" {
			o.args = append(o.args, argv[i+1:]...)
			break
		}
		name, val, hasVal := strings.Cut(a, "=")
		if p, ok := bools[name]; ok {
			*p = true
			o.given = append(o.given, name)
			continue
		}
		p, ok := vals[name]
		if !ok && name != "--with" {
			if strings.HasPrefix(a, "--") {
				return o, fmt.Errorf("unknown flag %s", a)
			}
			o.args = append(o.args, a)
			continue
		}
		if !hasVal {
			if i+1 >= len(argv) {
				return o, fmt.Errorf("%s needs a value", name)
			}
			i++
			val = argv[i]
		}
		o.given = append(o.given, name)
		switch name {
		case "--with":
			o.serve.with = append(o.serve.with, val)
		case "--db":
			*p, o.dbExplicit = val, true
		default:
			*p = val
		}
	}
	if len(o.args) > 0 && o.args[0] != "help" {
		cmd := commandNamed(o.args[0])
		for _, f := range o.given {
			switch {
			case cmd == nil: // an extension: extension() refuses --source and --human with their reasons
				if !slices.Contains(globalFlags, f) {
					return o, fmt.Errorf("an extension takes only the global flags before its name, not %s", f)
				}
			case !slices.Contains(cmd.flags, f) && (!slices.Contains(globalFlags, f) || slices.Contains(cmd.refuses, f)):
				return o, fmt.Errorf("%s does not take %s", cmd.name, f)
			}
		}
		if o.dbExplicit && o.url != "" {
			return o, errors.New("--db and --url name two databases: give one")
		}
	}
	return o, nil
}

func main() {
	e := env{stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr, getenv: os.Getenv, executable: os.Executable, clock: time.Now}
	err := runArgs(context.Background(), e, os.Args[1:])
	var usage usageError
	var exit exitError
	switch {
	case errors.As(err, &exit): // an extension ran and said its own message
		os.Exit(exit.code)
	case errors.As(err, &usage):
		if usage.err != nil {
			fmt.Fprintln(e.stderr, usage.err)
		}
		fmt.Fprint(e.stderr, usageText())
		os.Exit(2)
	case err != nil:
		fmt.Fprintln(e.stderr, "lifelog:", err)
		os.Exit(1)
	}
}

// usageError is a line with no command, the line help, or a line that parse refuses: main writes err, if there is
// one, and the usage, and exits 2.
type usageError struct{ err error }

func (u usageError) Error() string {
	if u.err == nil {
		return "usage"
	}
	return u.err.Error()
}

// runArgs runs one command line with the process of e, as main does: the parse, then the command. A command that takes
// the interrupt through its context (commandConsumesContext) gets a context that an interrupt cancels. So does
// SIGTERM: it is the signal of `systemctl stop` and `restart`, and without it the drain of serve never runs.
func runArgs(ctx context.Context, e env, argv []string) error {
	o, err := parse(argv, e.getenv)
	if err != nil || len(o.args) == 0 || o.args[0] == "help" {
		return usageError{err}
	}
	if commandConsumesContext(o) {
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
	}
	e.o = o
	return runContext(ctx, e)
}

// commandConsumesContext says whether an interrupt reaches the command through its context: every command that runs
// through the client or the server does; a setup command of the machine keeps the OS's interrupt.
func commandConsumesContext(o opts) bool {
	if len(o.args) == 0 {
		return false
	}
	cmd := commandNamed(o.args[0])
	if cmd == nil { // an extension takes the interrupt too, and passes it on
		return o.args[0] != "help"
	}
	return !cmd.setup
}

// runContext runs the command of the line e.o: its prepare, then the client (a running server over --url, or the
// handler in-process on the file dbPath names), then its run. An error of the Close of the file is joined
// to the error of the run. A failed PRAGMA optimize is not one: Close logs it.
func runContext(ctx context.Context, e env) (err error) {
	o := e.o
	cmd := commandNamed(o.args[0])
	if cmd == nil {
		return extension(ctx, e, o.args[0], o.args[1:])
	}
	e.cmd, e.table = cmd, commands
	args := o.args[1:]
	if cmd.prepare != nil {
		if o, err = cmd.prepare(e, o); err != nil {
			return err
		}
	}
	e.o = o
	if cmd.setup {
		return cmd.run(ctx, e, args)
	}
	if o.url != "" {
		e.c = client.Remote(o.url, o.source, o.token)
		return cmd.run(ctx, e, args)
	}
	path, err := dbPath(e)
	if err != nil {
		return err
	}
	d, err := db.Open(ctx, path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	e.o.db = path
	e.store = &core.Store{DB: d, SnapshotDir: o.snapshots, SettingsPath: core.SettingsPathFor(path), Clock: e.clock}
	if !cmd.ownHandler {
		e.c = client.InProcess(api.NewWith(e.store, nil), o.source)
	}
	return cmd.run(ctx, e, args)
}

// doAction runs a catalog action by name and shows its answer.
func doAction(ctx context.Context, e env, name string, vals map[string]string) error {
	return show(e)(do(ctx, e.c, name, vals))
}

func do(ctx context.Context, c *client.Client, name string, vals map[string]string) (*siren.Entity, error) {
	a, err := actionNamed(ctx, c, name)
	if err != nil {
		return nil, err
	}
	return c.Do(ctx, a, vals)
}

// actionNamed is the catalog's action of that name.
func actionNamed(ctx context.Context, c *client.Client, name string) (siren.Action, error) {
	actions, err := c.Catalog(ctx)
	if err != nil {
		return siren.Action{}, err
	}
	for _, a := range actions {
		if a.Name == name {
			return a, nil
		}
	}
	return siren.Action{}, fmt.Errorf("no action %q (lifelog actions)", name)
}

// dayOrToday is the day a shortcut sends: --day, else "today", which the API reads as the server's local day.
func dayOrToday(o opts) string {
	if o.day != "" {
		return o.day
	}
	return "today"
}

// initDB is `lifelog init [PATH]`: a new life.db at PATH, else where every other command looks for it. PATH with --db
// is a mistake, refused.
func initDB(ctx context.Context, e env, args []string) error {
	if len(args) > 1 {
		return errors.New("init [PATH]")
	}
	path := strings.Join(args, "")
	switch {
	case path != "" && e.o.dbExplicit:
		return errors.New("init takes PATH or --db, not both")
	case path == "":
		var err error
		if path, err = dbPath(e); err != nil {
			return err
		}
	}
	if err := db.Init(ctx, path); err != nil {
		return err
	}
	_, err := fmt.Fprintln(e.stdout, "created", path)
	return err
}

// dbPath is the life.db a command uses, in this order: --db, $LIFELOG_DB, the file beside this program
// (plan 090).
func dbPath(e env) (string, error) {
	o := e.o
	if o.dbExplicit || o.db != "" { // --db, else $LIFELOG_DB, which parse reads first
		return o.db, nil
	}
	exe, err := program(e)
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(exe), "life.db"), nil
}

// program is the path of the running program, with its symbolic links resolved. life.db lives in its folder when no
// --db or LIFELOG_DB names another, and the extensions find it there (plan 090).
func program(e env) (string, error) {
	exe, err := e.executable()
	if err != nil {
		return "", fmt.Errorf("the path of this program: %w", err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return exe, nil
}
