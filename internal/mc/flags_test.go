package mc

import (
	"slices"
	"testing"

	"github.com/Revise0592/McPanel/internal/store"
)

func TestJavaArgs(t *testing.T) {
	plain := javaArgs(store.Server{MemoryMB: 4096, Executable: "server.jar", JavaArgs: "-Dfoo=1"})
	want := []string{"-Xmx4096M", "-Xms1024M", "-Dfoo=1", "-jar", "server.jar", "nogui"}
	if !slices.Equal(plain, want) {
		t.Errorf("plain = %v, want %v", plain, want)
	}

	tuned := javaArgs(store.Server{MemoryMB: 4096, Executable: "server.jar", TunedFlags: true, JavaArgs: "-Dfoo=1"})
	if tuned[0] != "-Xms4096M" || tuned[1] != "-Xmx4096M" {
		t.Errorf("tuned starts %v, want the heap fixed at 4096M", tuned[:2])
	}
	if !slices.Contains(tuned, "-XX:G1HeapRegionSize=8M") {
		t.Errorf("tuned lacks the small-heap region size: %v", tuned)
	}
	// The user's own options come after the flags so they can override them.
	if i, j := slices.Index(tuned, "-XX:+UseG1GC"), slices.Index(tuned, "-Dfoo=1"); i < 0 || j < i {
		t.Errorf("own options should follow the flags: %v", tuned)
	}
	if !slices.Equal(tuned[len(tuned)-3:], []string{"-jar", "server.jar", "nogui"}) {
		t.Errorf("tuned ends %v", tuned[len(tuned)-3:])
	}

	big := javaArgs(store.Server{MemoryMB: 16384, Executable: "server.jar", TunedFlags: true})
	if !slices.Contains(big, "-XX:G1HeapRegionSize=16M") {
		t.Errorf("16 GB server should get the large-heap flags: %v", big)
	}
}

func TestJavaArgsLoadersAndProxy(t *testing.T) {
	neo := javaArgs(store.Server{Type: store.TypeNeoForge, MemoryMB: 4096, Executable: "@libraries/net/neoforged/neoforge/21.1.77/unix_args.txt"})
	if !slices.Equal(neo[len(neo)-2:], []string{"@libraries/net/neoforged/neoforge/21.1.77/unix_args.txt", "nogui"}) || slices.Contains(neo, "-jar") {
		t.Errorf("neoforge = %v", neo)
	}
	proxy := javaArgs(store.Server{Type: store.TypeVelocity, Executable: "velocity.jar", TunedFlags: true})
	if proxy[0] != "-Xms512M" || !slices.Contains(proxy, "-XX:MaxInlineLevel=15") || slices.Contains(proxy, "nogui") || slices.Contains(proxy, "-XX:G1NewSizePercent=30") {
		t.Errorf("velocity = %v", proxy)
	}
	if !slices.Equal(proxy[len(proxy)-2:], []string{"-jar", "velocity.jar"}) {
		t.Errorf("velocity ends %v", proxy[len(proxy)-2:])
	}
}
