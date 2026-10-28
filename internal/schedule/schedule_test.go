package schedule

import (
	"testing"
	"time"

	"github.com/Revise0592/McPanel/internal/store"
)

func TestNext(t *testing.T) {
	loc := time.UTC
	after := time.Date(2026, 10, 28, 6, 30, 0, 0, loc)
	cases := []struct {
		task store.Task
		want time.Time
	}{
		{store.Task{At: "05:00"}, time.Date(2026, 10, 29, 5, 0, 0, 0, loc)},
		{store.Task{At: "07:15"}, time.Date(2026, 10, 28, 7, 15, 0, 0, loc)},
		{store.Task{At: "06:30"}, time.Date(2026, 10, 29, 6, 30, 0, 0, loc)},
		{store.Task{EveryHours: 6}, time.Date(2026, 10, 28, 12, 30, 0, 0, loc)},
		{store.Task{At: "25:00"}, time.Time{}},
	}
	for _, c := range cases {
		if got := Next(c.task, after); !got.Equal(c.want) {
			t.Errorf("Next(%+v) = %v, want %v", c.task, got, c.want)
		}
	}
}

func TestParseAt(t *testing.T) {
	if h, m, ok := ParseAt("05:07"); !ok || h != 5 || m != 7 {
		t.Errorf("ParseAt(05:07) = %d, %d, %v", h, m, ok)
	}
	for _, bad := range []string{"", "5", "24:00", "12:60", "noon"} {
		if _, _, ok := ParseAt(bad); ok {
			t.Errorf("ParseAt(%q) accepted", bad)
		}
	}
}
