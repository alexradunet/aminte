package view

import "testing"

// TestEachKindOfTheNewMenuHasItsOwnIcon: a reading is not drawn with the icon of a metric.
func TestEachKindOfTheNewMenuHasItsOwnIcon(t *testing.T) {
	seen := map[string]string{}
	for _, k := range newKinds {
		if other, ok := seen[k.Icon]; ok {
			t.Errorf("%s and %s share the icon %s", other, k.Title, k.Icon)
		}
		seen[k.Icon] = k.Title
	}
}
