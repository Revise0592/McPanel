package web

import (
	"strings"
	"testing"
	"time"

	"github.com/Revise0592/McPanel/internal/mc"
)

func TestNiceCeil(t *testing.T) {
	for in, want := range map[float64]float64{0: 1, 3.2: 5, 12: 20, 22: 25, 100: 100, 0.7: 1} {
		if got := niceCeil(in); got != want {
			t.Errorf("niceCeil(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestFmtMem(t *testing.T) {
	for in, want := range map[float64]string{300: "300 MB", 1024: "1 GB", 1280: "1.25 GB", 2560: "2.5 GB"} {
		if got := fmtMem(in); got != want {
			t.Errorf("fmtMem(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestAverageAndGaps(t *testing.T) {
	base := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	var s []mc.Sample
	for i := range 10 {
		s = append(s, mc.Sample{At: base.Add(time.Duration(i) * time.Minute), CPU: float64(i), MemoryMB: 1024, TPS: 20})
	}
	// The server was off for half an hour.
	for i := range 5 {
		s = append(s, mc.Sample{At: base.Add(time.Duration(40+i) * time.Minute), CPU: 50, MemoryMB: 2048})
	}
	pts := average(s, 5*time.Minute)
	if len(pts) != 3 || pts[0].cpu != 2 || pts[0].mem != 1024 || pts[0].tps != 20 || pts[2].tps != -1 {
		t.Fatalf("average = %+v", pts)
	}
	r := chartRange{Span: time.Hour, Bucket: time.Minute}
	line, area := paths(average(s, time.Minute), func(p point) float64 { return p.cpu }, base, r, 100)
	if n := strings.Count(line, "M"); n != 2 {
		t.Errorf("line has %d segments, want 2: %s", n, line)
	}
	if n := strings.Count(area, "Z"); n != 2 {
		t.Errorf("area has %d shapes, want 2", n)
	}
	tline, _ := paths(average(s, time.Minute), func(p point) float64 { return p.tps }, base, r, 20)
	if strings.Count(tline, "M") != 1 {
		t.Errorf("TPS line should skip samples without TPS: %s", tline)
	}
}
