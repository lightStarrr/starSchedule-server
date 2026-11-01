package aiproxy

import "testing"

func TestClampReasoningEffort(t *testing.T) {
	all := []string{"minimal", "low", "medium", "high"}

	cases := []struct {
		name    string
		level   string
		efforts []string
		want    string
	}{
		{name: "empty level", level: "", efforts: all, want: ""},
		{name: "model without efforts", level: "high", efforts: nil, want: ""},
		{name: "in list keeps value", level: "medium", efforts: all, want: "medium"},
		{name: "unknown level drops", level: "ultra", efforts: all, want: ""},
		{name: "clamp down one step", level: "high", efforts: []string{"low", "medium"}, want: "medium"},
		{name: "clamp up one step", level: "minimal", efforts: []string{"low", "high"}, want: "low"},
		// high 距离 medium=1、low=2 → medium；medium 距离 low=1、high=1 → 取较低档 low
		{name: "clamp to nearest", level: "high", efforts: []string{"low", "medium"}, want: "medium"},
		{name: "tie prefers lower", level: "medium", efforts: []string{"low", "high"}, want: "low"},
		{name: "ignores unknown candidates", level: "high", efforts: []string{"bogus", "medium"}, want: "medium"},
		{name: "single candidate", level: "minimal", efforts: []string{"high"}, want: "high"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClampReasoningEffort(tc.level, tc.efforts); got != tc.want {
				t.Fatalf("ClampReasoningEffort(%q, %v) = %q, want %q", tc.level, tc.efforts, got, tc.want)
			}
		})
	}
}
