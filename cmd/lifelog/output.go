package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"lifelog/client/tools"
	"lifelog/siren"
)

// listActions prints the catalog. With --human it prints, for each action, the route, the CLI line (do NAME), the
// shortcuts of the program that wrap it, the MCP tool (the name with _ for -, none for an owner action: the rule of
// client/tools) and the owner mark. This listing is the one place that shows the surfaces of an action: no page keeps
// a copy (README, "Every action on every surface").
func listActions(ctx context.Context, e env) error {
	actions, err := e.c.Catalog(ctx)
	if err != nil {
		return err
	}
	if !e.o.human {
		return siren.WriteJSON(e.stdout, actions, "  ")
	}
	out := &errWriter{w: e.stdout}
	for _, a := range actions {
		var fs []string
		for _, f := range a.Fields {
			s := f.Name
			if !f.Required {
				s = "[" + s + "]"
			}
			if f.Placeholder != "" {
				s += ` ("` + f.Placeholder + `")`
			}
			fs = append(fs, s)
		}
		fmt.Fprintf(out, "%-14s %s %s\n  %s\n  cli: do %s\n", a.Name, a.Method, a.Href, a.Description, a.Name)
		var shortcuts []string
		for _, c := range e.table {
			if c.wraps == a.Name {
				shortcuts = append(shortcuts, c.name)
			}
		}
		if len(shortcuts) > 0 {
			fmt.Fprintf(out, "  shortcut: %s\n", strings.Join(shortcuts, ", "))
		}
		if a.Owner {
			fmt.Fprint(out, "  mcp: none\n  owner: yes\n")
		} else {
			fmt.Fprintf(out, "  mcp: %s\n", tools.Name(a.Name))
		}
		fmt.Fprintf(out, "  fields: %s\n", strings.Join(fs, " "))
	}
	return out.err
}

// show prints an entity (an error entity too: an agent reads the message and the links) and returns the error.
func show(e env) func(*siren.Entity, error) error {
	return func(ent *siren.Entity, err error) error {
		if ent == nil {
			return err
		}
		var perr error
		if e.o.human {
			perr = human(e.stdout, ent)
		} else {
			perr = siren.WriteJSON(e.stdout, ent, "  ")
		}
		if perr != nil {
			return perr
		}
		return err
	}
}

// errWriter writes to w until a write fails. It keeps the error of that write, and after it a write does nothing.
type errWriter struct {
	w   io.Writer
	err error
}

func (o *errWriter) Write(p []byte) (int, error) {
	if o.err != nil {
		return 0, o.err
	}
	n, err := o.w.Write(p)
	o.err = err
	return n, err
}

// human writes an entity as a readable summary: its title and class, the result of a write, its properties, then
// the embedded links, the links and the actions. It returns the error of the first write that fails, or of a value
// that JSON cannot write.
func human(w io.Writer, e *siren.Entity) error {
	out := &errWriter{w: w}
	fmt.Fprintf(out, "# %s  (%s)\n", e.Title, strings.Join(e.Class, " "))
	if e.Result != nil {
		b, err := json.Marshal(e.Result)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "result: %s\n", b)
	}
	if p, ok := e.Properties.(map[string]any); ok {
		keys := make([]string, 0, len(p))
		for k := range p {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			switch v := p[k].(type) {
			case string:
				if strings.Contains(v, "\n") {
					fmt.Fprintf(out, "%s:\n%s\n", k, indent(v))
				} else if v != "" {
					fmt.Fprintf(out, "%s: %s\n", k, v)
				}
			default:
				b, err := json.Marshal(v)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "%s: %s\n", k, b)
			}
		}
	}
	for _, l := range e.Entities {
		fmt.Fprintf(out, "  → %-12s %-28s %s\n", strings.Join(l.Rel, " "), l.Title, l.Href)
	}
	for _, l := range e.Links {
		fmt.Fprintf(out, "  ↗ %-12s %s\n", strings.Join(l.Rel, " "), l.Href)
	}
	for _, a := range e.Actions {
		fmt.Fprintf(out, "  ! %-14s %s %s\n", a.Name, a.Method, a.Href)
	}
	return out.err
}

func indent(s string) string { return "    " + strings.ReplaceAll(s, "\n", "\n    ") }
