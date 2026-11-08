// lifescale runs the opt-in synthetic workload described in tests/README.md.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"runtime/debug"
	"strings"

	"aminte/internal/scaletest"
)

// report is what lifescale prints: what scaletest.Run measured, and where and how it ran.
type report struct {
	scaletest.Result
	Environment map[string]string `json:"environment"`
	RootsPerSec float64           `json:"fixture_root_readings_per_second"`
	// Checks are what Run validated before it returned. A failed check is an error of Run, so a report lists all of them.
	Checks         []string `json:"checks"`
	Interpretation string   `json:"interpretation"`
}

func main() {
	var options scaletest.Options
	flag.StringVar(&options.Profile, "profile", "small", "small, lifetime (50 years), or stress")
	flag.Int64Var(&options.Seed, "seed", 2075, "synthetic generator seed")
	flag.IntVar(&options.Samples, "samples", 5, "warm samples per workflow (1-20); snapshot/restore run once")
	flag.BoolVar(&options.Previews, "previews", false, "generate unique realistic-sized JPEG previews; lifetime requires tens of GB")
	flag.StringVar(&options.Directory, "dir", "", "parent scratch directory (default: system temp)")
	flag.BoolVar(&options.Keep, "keep", false, "retain the generated scratch directory")
	flag.StringVar(&options.Storage, "storage", "", "hardware/filesystem description to record with results")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "lifescale accepts flags only")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	options.Progress = func(message string) { fmt.Fprintln(os.Stderr, message) }
	result, err := scaletest.Run(ctx, options)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	env := environment(options.Storage)
	env["sqlite"] = result.SQLite
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report{
		Result:      *result,
		Environment: env,
		RootsPerSec: float64(result.Manifest.Roots) / (float64(result.Build.Nanoseconds) / 1e9),
		Checks: []string{
			"all metric leaves: independent IDs, values, days, scope and ordering",
			"raw reading count, files and preview byte totals",
			"rare search and popular backlinks",
			"malformed batch rolls back its valid prefix",
			"four integrity groups",
			"snapshot restored into a distinct file and all known answers checked again",
		},
		Interpretation: "how to read this report: tests/README.md, section Synthetic scale runner",
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// environment describes where the run happened: the toolchain, the machine, the revision of the build.
func environment(storage string) map[string]string {
	out := map[string]string{"go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "logical_cpus": fmt.Sprint(runtime.NumCPU()), "gomaxprocs": fmt.Sprint(runtime.GOMAXPROCS(0)), "storage_description": storage, "cpu": "unavailable", "revision": "unavailable"}
	if storage == "" {
		out["storage_description"] = "unspecified; supply -storage for comparable runs"
	}
	if raw, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "model name") {
				_, name, _ := strings.Cut(line, ":")
				out["cpu"] = strings.TrimSpace(name)
				break
			}
		}
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				out["revision"] = setting.Value
				out["revision_source"] = "build info"
			case "vcs.modified":
				out["build_tree_dirty"] = setting.Value
			}
		}
		for _, d := range info.Deps {
			if d.Path == "modernc.org/sqlite" {
				out["sqlite_driver"] = d.Version
			}
		}
	}
	return out
}
