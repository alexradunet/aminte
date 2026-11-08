// Package tools offers the action catalog as the functions of a model: a tool for each action that is not the
// owner's, its JSON Schema made from the fields, and the arguments of a call flattened to form values. Two agents
// build their tool lists from it: the MCP server and a chat agent. The argument keep of Of is how an agent narrows
// the list further, for example by dropping every action with a file field.
package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"aminte/client"
	"aminte/siren"
)

// Tool is one function a model may call: an action of the catalog, or the read get.
type Tool struct {
	Name        string
	Description string
	Schema      map[string]any
	Action      *siren.Action // the action that the tool runs; nil for get
}

// Of is the tool list of the catalog: one tool for each action that is not the owner's, then get. An owner's
// action is never a tool, whatever keep says (AGENTS.md, "Every capability on every surface"). When keep is not nil,
// an action is a tool only if keep returns true for it. A tool name that two actions share is refused.
func Of(actions []siren.Action, keep func(siren.Action) bool) ([]Tool, error) {
	seen := map[string]string{"get": "built-in tool get"}
	var list []Tool
	for i := range actions {
		a := &actions[i]
		if a.Owner || (keep != nil && !keep(*a)) {
			continue
		}
		name := Name(a.Name)
		if prev, ok := seen[name]; ok {
			return nil, fmt.Errorf("tool name %q from action %q collides with %s", name, a.Name, prev)
		}
		seen[name] = fmt.Sprintf("action %q", a.Name)
		list = append(list, Tool{Name: name, Description: a.Description, Schema: inputSchema(a.Fields), Action: a})
	}
	return append(list,
		Tool{Name: "get", Description: "Follow a link: fetch the resource at an href from an earlier result (e.g. /pages/12).",
			Schema: map[string]any{"type": "object", "required": []string{"href"},
				"properties": map[string]any{"href": map[string]any{"type": "string"}}}},
	), nil
}

// Name is the tool name of an action: a hyphen is not allowed in every model's function names.
func Name(action string) string { return strings.ReplaceAll(action, "-", "_") }

// Run calls a tool through the client with its arguments, as Arguments gives them.
func Run(ctx context.Context, c *client.Client, t Tool, args map[string]string) (*siren.Entity, error) {
	if t.Action != nil {
		return c.Do(ctx, *t.Action, args)
	}
	if !strings.HasPrefix(args["href"], "/") {
		return nil, errors.New("href must be a path such as /pages/12")
	}
	return c.Get(ctx, args["href"])
}

// inputSchema is a JSON Schema for an action's fields: numbers as numbers, everything else a string. A date takes a
// local day or the word today (API.md, "The entity"), so its schema has no format date, which takes no word, and no
// pattern, which would repeat the schema's day CHECK. Its description adds the hint to the title, inside the title's own brackets when it ends
// with them. The title stays without the hint: it is also the label of the browser's date picker, in which a person
// cannot type the word.
func inputSchema(fields []siren.Field) map[string]any {
	props := map[string]any{}
	req := []string{}
	for _, f := range fields {
		p := map[string]any{"type": "string"}
		desc := f.Title
		switch f.Type {
		case "file": // the agent reads the file and sends it, as the CLI does (client.Do)
			desc = "The absolute path of a local file: " + desc
		case "number":
			p["type"] = "number"
		case "date":
			if strings.HasSuffix(desc, ")") {
				desc = strings.TrimSuffix(desc, ")") + "; YYYY-MM-DD or today)"
			} else {
				desc = strings.TrimSpace(desc + " (YYYY-MM-DD or today)")
			}
		}
		if f.Placeholder != "" { // an example or a prompt in words: JSON Schema "examples" would have to be a value
			desc = strings.TrimSpace(desc + " Placeholder: " + strings.TrimRight(f.Placeholder, ".") + ".")
		}
		if desc != "" {
			p["description"] = desc
		}
		if len(f.Options) > 0 {
			options := make([]any, 0, len(f.Options))
			for _, option := range f.Options {
				if f.Type == "number" { // the catalog's numbers parse: TestEveryNumericOptionIsANumber
					options = append(options, json.Number(option))
				} else {
					options = append(options, option)
				}
			}
			p["enum"] = options
		}
		props[f.Name] = p
		if f.Required {
			req = append(req, f.Name)
		}
	}
	return map[string]any{"type": "object", "properties": props, "required": req}
}

// Arguments flattens a model's JSON arguments to the form values the API takes: a number as its JSON text, which the
// API's own parsers read, as they read the CLI's and a browser's.
func Arguments(raw []byte) (map[string]string, error) {
	var args map[string]any
	if len(bytes.TrimSpace(raw)) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&args); err != nil {
			return nil, err
		}
	}
	out := map[string]string{}
	for k, v := range args {
		switch v := v.(type) {
		case nil:
		case string:
			out[k] = v
		case json.Number:
			out[k] = string(v)
		default:
			return nil, fmt.Errorf("unsupported argument %s", k)
		}
	}
	return out, nil
}
