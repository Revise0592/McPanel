package mc

import (
	"os"
	"testing"
	"time"
)

func TestReadProc(t *testing.T) {
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skip("no /proc here")
	}
	_, mem, ok := readProc(os.Getpid())
	if !ok || mem <= 0 {
		t.Fatalf("readProc = %d MB, %v", mem, ok)
	}
}

func TestTakeTPS(t *testing.T) {
	i := &Instance{}
	line := "[12:00:00 INFO]: TPS from last 1m, 5m, 15m: *20.0, 19.5, 18.2"
	if i.takeTPSLocked(line) {
		t.Fatal("took a TPS line nobody asked for")
	}
	i.tpsAsked = time.Now()
	if i.takeTPSLocked("[12:00:00 INFO]: Steve joined the game") {
		t.Fatal("took an unrelated line")
	}
	if !i.takeTPSLocked(line) || i.tps != 20 {
		t.Fatalf("tps = %v", i.tps)
	}
	if i.takeTPSLocked(line) {
		t.Fatal("took a second answer to one question")
	}
}
