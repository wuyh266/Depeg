package monitor

import (
	"testing"
)

func TestEvaluateExactDecimalBoundaries(t *testing.T) {
	for _, test := range []struct{ price, anchor, want, status string }{
		{"1", "1", "0.000000", "stable"},
		{"0.995", "1", "-50.000000", "warning"},
		{"1.01", "1", "100.000000", "critical"},
		{"0.990000000000000001", "1", "-100.000000", "warning"},
		{"0.994999999999999999", "1", "-50.000000", "warning"},
		{"0.995000000000000001", "1", "-50.000000", "stable"},
		{"1.09", "1.1", "-90.909091", "warning"},
		{"0.000000000000000002", "0.000000000000000001", "10000.000000", "critical"},
	} {
		t.Run(test.price+"/"+test.anchor, func(t *testing.T) {
			d, status, err := Evaluate(test.price, test.anchor, 50, 100)
			if err != nil || d != test.want || status != test.status {
				t.Fatalf("got %s %s %v", d, status, err)
			}
		})
	}
	for _, bad := range []string{"", "NaN", "Inf", "-1", "0", "1e3", "0.0000000000000000001", "100000000000000000000"} {
		if _, _, err := Evaluate("1", bad, 50, 100); err == nil {
			t.Fatalf("invalid anchor accepted: %s", bad)
		}
	}
}
