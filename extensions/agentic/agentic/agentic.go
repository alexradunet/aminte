// Package agentic is `aminte agentic-init` (plan 090): it makes the folder of the executable
// ready for a coding agent opened in it. It writes AGENTS.md (and a CLAUDE.md that imports it), the skills in the two
// folders the frameworks read, and an MCP config for Claude Code, Codex, OpenCode and Pi, each starting aminte's
// own MCP server on the folder's life.db.
package agentic

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

//go:embed templates
var templates embed.FS

// Paths are what the texts and the MCP configs name: the folder, the executable and the database, all absolute,
// so an MCP server starts from any working directory.
type Paths struct {
	Dir, Exe, DB string
}

// The states of a file after Init.
const (
	Written   = "written"   // it did not exist
	Unchanged = "unchanged" // it already had this text
	Kept      = "kept"      // it has another text, kept; force replaces it
	Replaced  = "replaced"  // it had another text, replaced on force
)

// Result is one file and what Init did with it.
type Result struct {
	Path  string `json:"path"` // relative to the folder, with / separators
	State string `json:"state"`
}

// skillDirs are the folders the frameworks read skills from: .agents/skills (Codex, OpenCode, Pi) and
// .claude/skills (Claude Code, OpenCode).
var skillDirs = []string{".agents/skills", ".claude/skills"}

// Files returns every file Init writes, by its path relative to the folder.
func Files(p Paths) (map[string][]byte, error) {
	out := map[string][]byte{}
	render := func(name string) ([]byte, error) {
		src, err := templates.ReadFile("templates/" + name)
		if err != nil {
			return nil, err
		}
		t, err := template.New(name).Option("missingkey=error").Parse(string(src))
		if err != nil {
			return nil, err
		}
		var b bytes.Buffer
		if err := t.Execute(&b, p); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		b, err := render(name)
		if err != nil {
			return nil, err
		}
		out[name] = b
	}
	skills, err := fs.ReadDir(templates, "templates/skills")
	if err != nil {
		return nil, err
	}
	for _, s := range skills {
		b, err := render("skills/" + s.Name() + "/SKILL.md")
		if err != nil {
			return nil, err
		}
		for _, d := range skillDirs {
			out[path.Join(d, s.Name(), "SKILL.md")] = b
		}
	}
	server := func(agent string) []string { return []string{"mcp", "--agent", agent, "--db", p.DB} }
	type stdio struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	out[".mcp.json"] = jsonFile(map[string]any{"mcpServers": map[string]stdio{"aminte": {p.Exe, server("claude")}}})
	out[".pi/mcp.json"] = jsonFile(map[string]any{"mcpServers": map[string]stdio{"aminte": {p.Exe, server("pi")}}})
	out["opencode.json"] = jsonFile(map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"mcp": map[string]any{"aminte": map[string]any{
			"type": "local", "command": append([]string{p.Exe}, server("opencode")...), "enabled": true}},
	})
	args := make([]string, 0, 5)
	for _, a := range server("codex") {
		args = append(args, tomlString(a))
	}
	out[".codex/config.toml"] = []byte("[mcp_servers.aminte]\ncommand = " + tomlString(p.Exe) + "\nargs = [" + strings.Join(args, ", ") + "]\n")
	return out, nil
}

// jsonFile is v as indented JSON with no HTML escaping. v holds only strings, booleans and lists and maps of them,
// so the encoder cannot fail.
func jsonFile(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
	return b.Bytes()
}

// tomlString is s as a TOML basic string: a backslash, a quote and every control character escaped.
func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"':
			b.WriteString(`\"`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// Init writes the files of Files into p.Dir. A missing file is written; a file with the same text is left as it is;
// a file with another text is kept, unless force, which replaces it. Every write goes to a temporary file in the same
// folder first, then a rename, so a file is never half written. The results are in path order.
func Init(p Paths, force bool) ([]Result, error) {
	files, err := Files(p)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []Result
	for _, name := range names {
		state, err := put(filepath.Join(p.Dir, filepath.FromSlash(name)), files[name], force)
		if err != nil {
			return out, fmt.Errorf("%s: %w", name, err)
		}
		out = append(out, Result{Path: name, State: state})
	}
	return out, nil
}

func put(full string, text []byte, force bool) (string, error) {
	have, err := os.ReadFile(full)
	state := Written
	switch {
	case err == nil && bytes.Equal(have, text):
		return Unchanged, nil
	case err == nil && !force:
		return Kept, nil
	case err == nil:
		state = Replaced
	case !os.IsNotExist(err):
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(full), ".agentic-*")
	if err != nil {
		return "", err
	}
	_, werr := tmp.Write(text)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), full)
	}
	if werr != nil {
		os.Remove(tmp.Name())
		return "", werr
	}
	return state, nil
}
