package services

import (
	"testing"
	"time"
)

func TestPeakAt(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	cases := []struct {
		name string
		t    time.Time
		want bool
	}{
		{"monday 08:59", time.Date(2026, 10, 26, 8, 59, 0, 0, loc), false},
		{"monday 09:00", time.Date(2026, 10, 26, 9, 0, 0, 0, loc), true},
		{"monday 11:59", time.Date(2026, 10, 26, 11, 59, 0, 0, loc), true},
		{"monday 12:00 lunch", time.Date(2026, 10, 26, 12, 0, 0, 0, loc), false},
		{"monday 13:59 lunch", time.Date(2026, 10, 26, 13, 59, 0, 0, loc), false},
		{"monday 14:00", time.Date(2026, 10, 26, 14, 0, 0, 0, loc), true},
		{"monday 17:59", time.Date(2026, 10, 26, 17, 59, 0, 0, loc), true},
		{"monday 18:00", time.Date(2026, 10, 26, 18, 0, 0, 0, loc), false},
		{"saturday 10:00", time.Date(2026, 10, 31, 10, 0, 0, 0, loc), false},
		{"sunday 15:00", time.Date(2026, 11, 1, 15, 0, 0, 0, loc), false},
	}
	for _, c := range cases {
		if got := peakAt(c.t, loc); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}
