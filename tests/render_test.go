package tests

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestMermaidRender renders every mermaid block of the docs with mermaid-cli, so a syntax error cannot ship.
// Opt-in: AMINTE_MERMAID=1. Needs node with `mmdc` on PATH (or MMDC=/path/to/mmdc) and a Chromium or Chrome
// (PUPPETEER_EXECUTABLE_PATH, or chromium / google-chrome on PATH); install once: npm i -g @mermaid-js/mermaid-cli.
// Nothing is written into the repository: the SVGs go to a temporary folder.
func TestMermaidRender(t *testing.T) {
	if os.Getenv("AMINTE_MERMAID") == "" {
		t.Skip("set AMINTE_MERMAID=1 to render the diagrams (needs mmdc and a Chromium)")
	}
	mmdc := os.Getenv("MMDC")
	if mmdc == "" {
		var err error
		if mmdc, err = exec.LookPath("mmdc"); err != nil {
			t.Fatal("mmdc not found (npm i -g @mermaid-js/mermaid-cli, or set MMDC)")
		}
	}
	cfg := map[string]any{"args": []string{"--no-sandbox", "--disable-gpu"}}
	chrome := os.Getenv("PUPPETEER_EXECUTABLE_PATH")
	for _, x := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if chrome == "" {
			chrome, _ = exec.LookPath(x)
		}
	}
	if chrome != "" {
		cfg["executablePath"] = chrome
	}
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "puppeteer.json")
	b, _ := json.Marshal(cfg)
	os.WriteFile(cfgPath, b, 0o644)
	blocks := mermaidBlock.FindAllStringSubmatch(realDocs().Text(), -1)
	if len(blocks) == 0 {
		t.Fatal("no mermaid block in the docs")
	}
	id := regexp.MustCompile(`^%% diagram: ([a-z-]+)`)
	for _, m := range blocks {
		name := "block"
		if x := id.FindStringSubmatch(m[1]); x != nil {
			name = x[1]
		}
		t.Run(name, func(t *testing.T) {
			src, out := filepath.Join(dir, name+".mmd"), filepath.Join(dir, name+".svg")
			os.WriteFile(src, []byte(m[1]+"\n"), 0o644)
			msg, err := exec.Command(mmdc, "-i", src, "-o", out, "-p", cfgPath).CombinedOutput()
			svg, _ := os.ReadFile(out)
			if err != nil || len(svg) <= 500 || strings.Contains(string(svg), "Syntax error") {
				t.Errorf("%s does not render: %v %s", name, err, clip(string(msg), 300))
			}
		})
	}
}
