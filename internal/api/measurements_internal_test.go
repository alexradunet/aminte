package api

import (
	"reflect"
	"testing"

	"aminte/internal/core"
)

// TestMetricsGroupByTheirCategories: a metric is listed under each category it is filed in, in the order of the
// categories (by path, so a category comes before the categories under it), and the metrics filed nowhere come last,
// as Not filed. A category is listed when it or a category under it holds a metric: Home holds none itself, and
// Home/Garden holds one. A category that holds nothing (Health/Empty, Travel) is left out, and "Healthy" is not under
// "Health".
func TestMetricsGroupByTheirCategories(t *testing.T) {
	weight := core.Metric{ID: 1, Name: "weight", Categories: []string{"Health"}}
	ferritin := core.Metric{ID: 2, Name: "ferritin", Categories: []string{"Health/Blood"}}
	run := core.Metric{ID: 3, Name: "run", Categories: []string{"Health", "Sport"}}
	walk := core.Metric{ID: 4, Name: "walk"}
	rain := core.Metric{ID: 5, Name: "rain", Categories: []string{"Home/Garden"}}
	juice := core.Metric{ID: 6, Name: "juice", Categories: []string{"Healthy"}}
	cs := []core.Category{{ID: 10, Title: "Health", Path: "Health"}, {ID: 11, Title: "Blood", Path: "Health/Blood"},
		{ID: 12, Title: "Empty", Path: "Health/Empty"}, {ID: 13, Title: "Healthy", Path: "Healthy"}, {ID: 14, Title: "Home", Path: "Home"},
		{ID: 15, Title: "Garden", Path: "Home/Garden"}, {ID: 16, Title: "Sport", Path: "Sport"}, {ID: 17, Title: "Travel", Path: "Travel"}}
	for _, c := range []struct {
		name string
		ms   []core.Metric
		cs   []core.Category
		want []metricGroup
	}{
		{"no metric", nil, cs, nil},
		{"none filed", []core.Metric{walk}, nil, []metricGroup{{Title: "Not filed", Metrics: []core.Metric{walk}}}},
		{"every case", []core.Metric{weight, ferritin, run, walk, rain, juice}, cs, []metricGroup{
			{Title: "Health", Path: "Health", Page: 10, Depth: 0, Metrics: []core.Metric{weight, run}},
			{Title: "Blood", Path: "Health/Blood", Page: 11, Depth: 1, Metrics: []core.Metric{ferritin}},
			{Title: "Healthy", Path: "Healthy", Page: 13, Depth: 0, Metrics: []core.Metric{juice}},
			{Title: "Home", Path: "Home", Page: 14, Depth: 0, Metrics: []core.Metric{}},
			{Title: "Garden", Path: "Home/Garden", Page: 15, Depth: 1, Metrics: []core.Metric{rain}},
			{Title: "Sport", Path: "Sport", Page: 16, Depth: 0, Metrics: []core.Metric{run}},
			{Title: "Not filed", Metrics: []core.Metric{walk}},
		}},
	} {
		if got := groupMetrics(c.ms, c.cs); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s:\n %+v\nwant\n %+v", c.name, got, c.want)
		}
	}
}
