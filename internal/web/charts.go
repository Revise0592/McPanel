package web

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/Revise0592/McPanel/internal/mc"
)

// The usage charts are drawn on the server as SVG paths in a 1000×100 box
// that stretches to fit; labels are HTML around it.
const (
	chartW = 1000.0
	chartH = 100.0
)

type chartRange struct {
	Key, Label, Ago string
	Span            time.Duration
	Bucket          time.Duration // points are averaged over this long
	Row             time.Duration // and table rows over this long
}

var chartRanges = []chartRange{
	{"hour", "Last hour", "1 hour ago", time.Hour, mc.SampleEvery, 5 * time.Minute},
	{"day", "Last day", "24 hours ago", 24 * time.Hour, 10 * time.Minute, time.Hour},
}

type chartTick struct {
	Label string
	Top   float64 // percent from the top of the plot
}

type usageChart struct {
	Title string
	Now   string // the latest value
	Line  string // SVG path data
	Area  string
	Ticks []chartTick
}

type usageRow struct {
	Time          time.Time
	CPU, Mem, TPS string
}

type usageData struct {
	serverView
	Range   chartRange
	Ranges  []chartRange
	HasData bool
	Charts  []usageChart
	Rows    []usageRow
	// Points feeds the hover readout: [unix ms, cpu, memory MB, tps].
	Points     string
	Start, End int64 // unix ms
	BucketMS   int64
}

// point is an averaged sample; a value below zero means none.
type point struct {
	at            time.Time
	cpu, mem, tps float64
}

// average groups samples into buckets of d, oldest first.
func average(samples []mc.Sample, d time.Duration) []point {
	var out []point
	var cur point
	var n, nt int
	flush := func() {
		if n == 0 {
			return
		}
		cur.cpu /= float64(n)
		cur.mem /= float64(n)
		if nt > 0 {
			cur.tps /= float64(nt)
		} else {
			cur.tps = -1
		}
		out = append(out, cur)
	}
	for _, s := range samples {
		at := s.At.Truncate(d)
		if n > 0 && !at.Equal(cur.at) {
			flush()
			n, nt = 0, 0
		}
		if n == 0 {
			cur = point{at: at}
		}
		n++
		cur.cpu += s.CPU
		cur.mem += float64(s.MemoryMB)
		if s.TPS > 0 {
			cur.tps += s.TPS
			nt++
		}
	}
	flush()
	return out
}

// niceCeil rounds v up to 1, 2, 2.5 or 5 times a power of ten.
func niceCeil(v float64) float64 {
	if v <= 0 {
		return 1
	}
	p := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if v <= m*p {
			return m * p
		}
	}
	return 10 * p
}

// paths draws one series, breaking the line where samples are missing.
func paths(pts []point, val func(point) float64, start time.Time, r chartRange, top float64) (line, area string) {
	var l, a strings.Builder
	var segStart, lastX float64
	var last time.Time
	open := false
	closeArea := func() {
		if open {
			fmt.Fprintf(&a, "L%.1f,%.0fL%.1f,%.0fZ", lastX, chartH, segStart, chartH)
		}
	}
	for _, p := range pts {
		v := val(p)
		if v < 0 {
			continue
		}
		x := float64(p.at.Sub(start)) / float64(r.Span) * chartW
		y := chartH - min(v/top, 1)*chartH*0.96
		if open && p.at.Sub(last) > r.Bucket*5/2 {
			closeArea()
			open = false
		}
		if !open {
			fmt.Fprintf(&l, "M%.1f,%.1f", x, y)
			fmt.Fprintf(&a, "M%.1f,%.0fL%.1f,%.1f", x, chartH, x, y)
			segStart, open = x, true
		} else {
			fmt.Fprintf(&l, "L%.1f,%.1f", x, y)
			fmt.Fprintf(&a, "L%.1f,%.1f", x, y)
		}
		lastX, last = x, p.at
	}
	closeArea()
	return l.String(), a.String()
}

func ticks(top float64, format func(float64) string) []chartTick {
	return []chartTick{
		{format(top), (1 - 0.96) * 100},
		{format(top / 2), (1 - 0.48) * 100},
		{format(0), 100},
	}
}

func fmtPct(v float64) string { return trimZero(fmt.Sprintf("%.1f", v)) + "%" }
func fmtTPS(v float64) string { return trimZero(fmt.Sprintf("%.1f", v)) }

// fmtMem shows memory in MB below 1 GB and in GB above.
func fmtMem(mb float64) string {
	if mb < 1024 {
		return fmt.Sprintf("%.0f MB", mb)
	}
	return trimZero(fmt.Sprintf("%.2f", mb/1024)) + " GB"
}

// trimZero drops trailing zeros after a decimal point: 1.50 → 1.5, 2.00 → 2.
func trimZero(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	return strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
}

func (a *App) buildUsage(v serverView, inst *mc.Instance, key string) usageData {
	r := chartRanges[0]
	for _, c := range chartRanges {
		if c.Key == key {
			r = c
		}
	}
	end := time.Now()
	start := end.Add(-r.Span)
	samples := inst.History(start)
	d := usageData{serverView: v, Range: r, Ranges: chartRanges, HasData: len(samples) > 1,
		Start: start.UnixMilli(), End: end.UnixMilli(), BucketMS: r.Bucket.Milliseconds()}
	if !d.HasData {
		return d
	}
	pts := average(samples, r.Bucket)
	var maxCPU, maxMem float64
	hasTPS := false
	for _, p := range pts {
		maxCPU, maxMem = max(maxCPU, p.cpu), max(maxMem, p.mem)
		hasTPS = hasTPS || p.tps >= 0
	}
	latest := samples[len(samples)-1]

	cpuTop := max(niceCeil(maxCPU), 10)
	c := usageChart{Title: "CPU", Now: fmtPct(math.Round(latest.CPU)), Ticks: ticks(cpuTop, fmtPct)}
	c.Line, c.Area = paths(pts, func(p point) float64 { return p.cpu }, start, r, cpuTop)
	d.Charts = append(d.Charts, c)

	memTop := niceCeil(max(maxMem, float64(v.MemoryMB), 64))
	c = usageChart{Title: "Memory", Now: fmtMem(float64(latest.MemoryMB)), Ticks: ticks(memTop, fmtMem)}
	c.Line, c.Area = paths(pts, func(p point) float64 { return p.mem }, start, r, memTop)
	d.Charts = append(d.Charts, c)

	if hasTPS {
		c = usageChart{Title: "TPS", Now: "-", Ticks: ticks(20, fmtTPS)}
		for _, s := range samples {
			if s.TPS > 0 {
				c.Now = fmtTPS(s.TPS)
			}
		}
		c.Line, c.Area = paths(pts, func(p point) float64 { return p.tps }, start, r, 20)
		d.Charts = append(d.Charts, c)
	}

	js := make([][4]any, len(pts))
	for i, p := range pts {
		var tps any
		if p.tps >= 0 {
			tps = math.Round(p.tps*10) / 10
		}
		js[i] = [4]any{p.at.UnixMilli(), math.Round(p.cpu*10) / 10, math.Round(p.mem), tps}
	}
	b, _ := json.Marshal(js)
	d.Points = string(b)

	rows := average(samples, r.Row)
	for i := len(rows) - 1; i >= 0; i-- {
		p := rows[i]
		row := usageRow{Time: p.at, CPU: fmtPct(math.Round(p.cpu)), Mem: fmtMem(p.mem), TPS: "-"}
		if p.tps >= 0 {
			row.TPS = fmtTPS(p.tps)
		}
		d.Rows = append(d.Rows, row)
	}
	return d
}

// usagePartial is the usage chart card, which refreshes itself.
func (a *App) usagePartial(w http.ResponseWriter, r *http.Request) {
	sv, inst, ok := a.load(w, r)
	if !ok {
		return
	}
	a.partial(w, "usage-card", a.buildUsage(a.view(r, sv), inst, r.FormValue("range")))
}
