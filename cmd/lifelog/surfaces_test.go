package main

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"lifelog/siren"
)

// actionsOfTheCatalog runs `lifelog actions` on a fresh file and gives the catalog in JSON and the text of
// `lifelog actions --human`.
func actionsOfTheCatalog(t *testing.T) (catalog []siren.Action, human string) {
	t.Helper()
	live := filepath.Join(t.TempDir(), "life.db")
	e, _, _ := testEnv(t)
	if err := runArgs(t.Context(), e, []string{"init", live}); err != nil {
		t.Fatal(err)
	}
	e, stdout, _ := testEnv(t)
	if err := runArgs(t.Context(), e, []string{"actions", "--db", live}); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(stdout.Bytes(), &catalog); err != nil {
		t.Fatalf("lifelog actions did not write the catalog as JSON: %v", err)
	}
	e, stdout, _ = testEnv(t)
	if err := runArgs(t.Context(), e, []string{"actions", "--db", live, "--human"}); err != nil {
		t.Fatal(err)
	}
	return catalog, stdout.String()
}

// blocksOf splits the text of `lifelog actions --human` into one block for each action: the lines from the line
// that starts at the margin to the next one. The key is the name of the action.
func blocksOf(t *testing.T, text string) map[string][]string {
	t.Helper()
	blocks := map[string][]string{}
	var name string
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if !strings.HasPrefix(line, " ") {
			name = strings.Fields(line)[0]
			if _, twice := blocks[name]; twice {
				t.Fatalf("the action %s is printed twice", name)
			}
		}
		blocks[name] = append(blocks[name], line)
	}
	return blocks
}

// TestActionsHumanShowsTheSurfacesOfEachAction: `lifelog actions --human` prints, for each action of the catalog, the
// route, the CLI line, the MCP tool, the owner mark and the shortcut (README, "Every action on every surface"). The
// rows below are written by hand; the loop holds every action to the rule of the README.
func TestActionsHumanShowsTheSurfacesOfEachAction(t *testing.T) {
	catalog, text := actionsOfTheCatalog(t)
	blocks := blocksOf(t, text)
	if len(blocks) != len(catalog) {
		t.Errorf("the text has %d actions, the catalog %d", len(blocks), len(catalog))
	}

	for _, tc := range []struct {
		action string
		has    []string // lines, in this order, among the lines of the block
		hasNot []string // line starts that the block does not hold
	}{
		{"capture",
			[]string{"capture        POST /days/{day}/capture", "  cli: do capture", "  shortcut: capture", "  mcp: capture"},
			[]string{"  owner:"}},
		{"open-day",
			[]string{"open-day       GET /days/go", "  cli: do open-day", "  shortcut: day", "  mcp: open_day"},
			[]string{"  owner:"}},
		{"create-page",
			[]string{"create-page    POST /pages", "  cli: do create-page", "  mcp: create_page"},
			[]string{"  owner:", "  shortcut:"}},
		{"snapshot",
			[]string{"snapshot       POST /snapshots", "  cli: do snapshot", "  shortcut: snapshot", "  mcp: none", "  owner: yes"},
			nil},
		{"settings",
			[]string{"settings       POST /settings", "  cli: do settings", "  mcp: none", "  owner: yes"},
			[]string{"  shortcut:"}},
	} {
		t.Run(tc.action, func(t *testing.T) {
			block, ok := blocks[tc.action]
			if !ok {
				t.Fatalf("the text has no action %s", tc.action)
			}
			from := 0
			for _, line := range tc.has {
				i := slices.Index(block[from:], line)
				if i < 0 {
					t.Errorf("the block has no line %q after line %d:\n%s", line, from, strings.Join(block, "\n"))
					continue
				}
				from += i + 1
			}
			for _, start := range tc.hasNot {
				for _, line := range block {
					if strings.HasPrefix(line, start) {
						t.Errorf("the block holds the line %q, and must hold none that starts %q", line, start)
					}
				}
			}
		})
	}

	for _, a := range catalog {
		block := blocks[a.Name]
		mcp := "  mcp: " + strings.ReplaceAll(a.Name, "-", "_")
		if a.Owner {
			mcp = "  mcp: none"
		}
		var shortcuts []string
		for _, c := range commands {
			if c.wraps == a.Name {
				shortcuts = append(shortcuts, c.name)
			}
		}
		want := []string{"  cli: do " + a.Name, mcp}
		if len(shortcuts) > 0 {
			want = append(want, "  shortcut: "+strings.Join(shortcuts, ", "))
		}
		if a.Owner {
			want = append(want, "  owner: yes")
		}
		if first := strings.Fields(block[0]); len(first) != 3 || first[1] != a.Method || first[2] != a.Href {
			t.Errorf("%s: the first line is %q, want the name, then %s %s", a.Name, block[0], a.Method, a.Href)
		}
		for _, line := range want {
			if !slices.Contains(block, line) {
				t.Errorf("%s: the block has no line %q", a.Name, line)
			}
		}
		if !a.Owner && slices.Contains(block, "  owner: yes") {
			t.Errorf("%s: it is not an owner action, and the block marks it", a.Name)
		}
	}
}

// TestEachShortcutNamesAnActionOfTheCatalog: a shortcut of the CLI wraps an action of the catalog, never replaces or
// extends it (AGENTS.md, "Every capability on every surface"). The table of commands says which one it wraps.
func TestEachShortcutNamesAnActionOfTheCatalog(t *testing.T) {
	catalog, _ := actionsOfTheCatalog(t)
	names := map[string]bool{}
	for _, a := range catalog {
		names[a.Name] = true
	}
	shortcuts := 0
	for _, c := range commands {
		if c.wraps == "" {
			continue
		}
		shortcuts++
		if !names[c.wraps] {
			t.Errorf("the command %s wraps %q, which is no action of the catalog", c.name, c.wraps)
		}
	}
	if shortcuts < 2 {
		t.Errorf("the table of commands has %d shortcuts: the check reads nothing", shortcuts)
	}
}
