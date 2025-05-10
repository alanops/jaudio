package main

import (
	"math"
	"testing"
)

const floatTolerance = 1e-6

// TestAmplitudeToMeterFill tests the amplitudeToMeterFill function
func TestAmplitudeToMeterFill(t *testing.T) {
	tests := []struct {
		name    string
		val     float32
		minDB   float64
		maxDB   float64
		want    float32
		wantErr bool
	}{
		{"zero amplitude", 0.0, -70.0, 0.0, 0.0, false},
		{"max amplitude (0dB)", 1.0, -70.0, 0.0, 1.0, false},
		{"mid amplitude (-35dB)", float32(math.Pow(10, -35.0/20.0)), -70.0, 0.0, 0.5, false},
		{"low amplitude (-70dB)", float32(math.Pow(10, -70.0/20.0)), -70.0, 0.0, 0.0, false},
		{"below minDB", float32(math.Pow(10, -80.0/20.0)), -70.0, 0.0, 0.0, false},
		{"above maxDB (clipped)", 2.0, -70.0, 0.0, 1.0, false},
		{"custom range: mid", float32(math.Pow(10, -30.0/20.0)), -60.0, -20.0, 0.75, false}, // -30dB in -60 to -20 range
		{"custom range: min", float32(math.Pow(10, -60.0/20.0)), -60.0, -20.0, 0.0, false},
		{"custom range: max", float32(math.Pow(10, -20.0/20.0)), -60.0, -20.0, 1.0, false},
		{"very small amplitude", 0.000001, -70.0, 0.0, 0.0, false}, // Below 0.00001 threshold
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use the global meterMinDB and meterMaxDB for tests that don't specify custom ranges
			// This makes tests more concise if they use the application's defaults.
			// However, the original function signature takes minDB, maxDB, so we pass them.
			// For this test, we'll use the tt.minDB and tt.maxDB as defined in the test case.
			got := amplitudeToMeterFill(tt.val, tt.minDB, tt.maxDB)
			if math.Abs(float64(got-tt.want)) > floatTolerance {
				t.Errorf("amplitudeToMeterFill(%v, %v, %v) = %v, want %v", tt.val, tt.minDB, tt.maxDB, got, tt.want)
			}
		})
	}
}

// TestContainsInt tests the containsInt function
func TestContainsInt(t *testing.T) {
	tests := []struct {
		name string
		arr  []int
		v    int
		want bool
	}{
		{"empty slice", []int{}, 5, false},
		{"value present", []int{1, 2, 3, 4, 5}, 3, true},
		{"value not present", []int{1, 2, 4, 5}, 3, false},
		{"value at start", []int{3, 1, 2, 4, 5}, 3, true},
		{"value at end", []int{1, 2, 4, 5, 3}, 3, true},
		{"slice with one element, present", []int{3}, 3, true},
		{"slice with one element, not present", []int{1}, 3, false},
		{"slice with duplicates, present", []int{1, 2, 3, 3, 4}, 3, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsInt(tt.arr, tt.v); got != tt.want {
				t.Errorf("containsInt(%v, %v) = %v, want %v", tt.arr, tt.v, got, tt.want)
			}
		})
	}
}