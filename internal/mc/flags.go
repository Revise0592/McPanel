package mc

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/Revise0592/McPanel/internal/store"
)

// aikarFlags are the garbage collector settings recommended by Paper
// (https://docs.papermc.io/paper/aikars-flags), minus
// G1RSetUpdatingPauseIntervalMillis, which newer Java no longer accepts.
// Servers with more than 12 GB get the larger young generation the guide
// suggests for them.
func aikarFlags(memMB int) []string {
	newSize, maxNew, region, reserve, ihop := "30", "40", "8M", "20", "15"
	if memMB > 12*1024 {
		newSize, maxNew, region, reserve, ihop = "40", "50", "16M", "15", "20"
	}
	return []string{
		"-XX:+UseG1GC",
		"-XX:+ParallelRefProcEnabled",
		"-XX:MaxGCPauseMillis=200",
		"-XX:+UnlockExperimentalVMOptions",
		"-XX:+DisableExplicitGC",
		"-XX:+AlwaysPreTouch",
		"-XX:G1NewSizePercent=" + newSize,
		"-XX:G1MaxNewSizePercent=" + maxNew,
		"-XX:G1HeapRegionSize=" + region,
		"-XX:G1ReservePercent=" + reserve,
		"-XX:G1HeapWastePercent=5",
		"-XX:G1MixedGCCountTarget=4",
		"-XX:InitiatingHeapOccupancyPercent=" + ihop,
		"-XX:G1MixedGCLiveThresholdPercent=90",
		"-XX:SurvivorRatio=32",
		"-XX:+PerfDisableSharedMem",
		"-XX:MaxTenuringThreshold=1",
		"-Dusing.aikars.flags=https://mcflags.emc.gs",
		"-Daikars.new.flags=true",
	}
}

// velocityFlags are the settings Velocity's documentation recommends.
var velocityFlags = []string{
	"-XX:+UseG1GC",
	"-XX:G1HeapRegionSize=4M",
	"-XX:+UnlockExperimentalVMOptions",
	"-XX:+ParallelRefProcEnabled",
	"-XX:+AlwaysPreTouch",
	"-XX:MaxInlineLevel=15",
}

// DefaultMemoryMB is what a server gets when none is set.
func DefaultMemoryMB(typ string) int {
	if store.IsProxy(typ) {
		return 512
	}
	return 2048
}

// javaArgs builds the JVM command line for a Java server.
func javaArgs(def store.Server) []string {
	mem := def.MemoryMB
	if mem <= 0 {
		mem = DefaultMemoryMB(def.Type)
	}
	var args []string
	if def.TunedFlags && store.IsProxy(def.Type) {
		args = append([]string{fmt.Sprintf("-Xms%dM", mem), fmt.Sprintf("-Xmx%dM", mem)}, velocityFlags...)
	} else if def.TunedFlags {
		// The flags expect the whole heap to be reserved up front.
		args = append([]string{fmt.Sprintf("-Xms%dM", mem), fmt.Sprintf("-Xmx%dM", mem)}, aikarFlags(mem)...)
	} else {
		args = []string{fmt.Sprintf("-Xmx%dM", mem), fmt.Sprintf("-Xms%dM", min(mem, 1024))}
	}
	args = append(args, strings.Fields(def.JavaArgs)...)
	switch {
	case store.IsProxy(def.Type):
		return append(args, "-jar", def.Executable)
	case strings.HasPrefix(def.Executable, "@"):
		// NeoForge and newer Forge list their classpath in a file.
		return append(args, def.Executable, "nogui")
	}
	return append(args, "-jar", def.Executable, "nogui")
}

// HostMemoryMB is how much memory this machine, or the container McPanel
// runs in, has. It's 0 when that can't be read.
func HostMemoryMB() int {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	var kb int64
	for _, line := range strings.Split(string(b), "\n") {
		if rest, ok := strings.CutPrefix(line, "MemTotal:"); ok {
			kb, _ = strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(rest), " kB"), 10, 64)
			break
		}
	}
	total := kb / 1024
	// A container's limit is lower than what /proc/meminfo shows.
	if b, err := os.ReadFile("/sys/fs/cgroup/memory.max"); err == nil {
		if lim, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil && lim>>20 < total {
			total = lim >> 20
		}
	}
	return int(total)
}
