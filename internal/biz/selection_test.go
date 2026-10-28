package biz

import (
	"testing"
)

func TestSelectionUsesTierThresholdAndFloorsAverage(t *testing.T) {
	for _, test := range []struct {
		name     string
		tier     string
		first    int
		second   int
		selected bool
		average  int
	}{
		{name: "official meets threshold", tier: "official", first: 60, second: 60, selected: true, average: 60},
		{name: "official misses threshold", tier: "official", first: 59, second: 60, selected: false, average: 59},
		{name: "media meets threshold", tier: "media", first: 76, second: 76, selected: true, average: 76},
		{name: "media misses threshold", tier: "media", first: 75, second: 76, selected: false, average: 75},
		{name: "average floors odd sum", tier: "official", first: 61, second: 60, selected: true, average: 60},
	} {
		t.Run(test.name, func(t *testing.T) {
			selected, average := Selection(test.tier, test.first, test.second, Thresholds{Official: 60, Media: 76})
			if selected != test.selected || average != test.average {
				t.Fatalf("Selection(%q, %d, %d) = (%v, %d), want (%v, %d)", test.tier, test.first, test.second, selected, average, test.selected, test.average)
			}
		})
	}
}

func TestSelectionUsesConfiguredThresholds(t *testing.T) {
	for _, test := range []struct {
		name, tier    string
		first, second int
		thresholds    Thresholds
		selected      bool
	}{
		{"custom official", "official", 40, 40, Thresholds{Official: 40}, true},
		{"custom media boundary", "media", 59, 61, Thresholds{Media: 60}, true},
		{"below custom media", "media", 59, 60, Thresholds{Media: 60}, false},
		{"zero threshold", "official", 0, 0, Thresholds{}, true},
		{"hundred threshold", "media", 100, 100, Thresholds{Media: 100}, true},
		{"unknown tier", "unknown", 100, 100, Thresholds{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			selected, _ := Selection(test.tier, test.first, test.second, test.thresholds)
			if selected != test.selected {
				t.Fatalf("selected=%v, want %v", selected, test.selected)
			}
		})
	}
}
