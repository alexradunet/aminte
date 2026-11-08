package api_test

import (
	"testing"

	"aminte/internal/core"
	"aminte/siren"
)

// siren holds the codes for the clients, which must not import core; core holds the codes it sends. The two copies of
// a code must be the same string.
func TestSirenCodesEqualTheCodesOfCore(t *testing.T) {
	for name, pair := range map[string][2]string{
		"stale version":    {siren.CodeStaleVersion, core.CodeStaleVersion},
		"snapshot refused": {siren.CodeSnapshotRefused, core.CodeSnapshotRefused},
		"busy":             {siren.CodeBusy, core.CodeBusy},
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s: siren has %q, core has %q", name, pair[0], pair[1])
		}
	}
}
