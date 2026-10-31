package mc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Revise0592/McPanel/internal/store"
)

// fakeBedrock is a stand-in server program: it says it has started and
// exits when told to stop. It doesn't open the game port, so the test can
// tell the panel's sleeping listener from the server.
const fakeBedrock = `#!/bin/sh
echo "Server started."
while read line; do
  [ "$line" = "stop" ] && exit 0
done
`

func freeUDPPort(t *testing.T) int {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	return pc.LocalAddr().(*net.UDPAddr).Port
}

func waitState(t *testing.T, i *Instance, want State) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if i.Status().State == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("state is %s, want %s", i.Status().State, want)
}

func TestSleepAndWake(t *testing.T) {
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("needs /bin/sh")
	}
	dir := t.TempDir()
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(dir, st)
	sv, err := st.AddServer(store.Server{Name: "Sleepy", Type: store.TypeBedrock, Executable: "fake.sh", Installed: true, SleepAfterMins: 5})
	if err != nil {
		t.Fatal(err)
	}
	inst, _ := m.Get(sv.ID)
	os.MkdirAll(inst.Dir(), 0o755)
	os.WriteFile(filepath.Join(inst.Dir(), "fake.sh"), []byte(fakeBedrock), 0o755)
	port := freeUDPPort(t)
	os.WriteFile(filepath.Join(inst.Dir(), "server.properties"),
		[]byte(fmt.Sprintf("server-name=Sleepy island\nserver-port=%d\nserver-ip=127.0.0.1\n", port)), 0o644)

	if err := inst.Start(); err != nil {
		t.Fatal(err)
	}
	waitState(t, inst, Running)

	// Not idle long enough yet.
	if d := inst.idleFor(time.Now()); d > time.Minute {
		t.Fatalf("idle for %s right after starting", d)
	}
	if d := inst.idleFor(time.Now().Add(6 * time.Minute)); d < 5*time.Minute {
		t.Fatalf("idle for only %s", d)
	}

	if err := inst.Sleep("Asleep: test"); err != nil {
		t.Fatal(err)
	}
	waitState(t, inst, Sleeping)
	if l := inst.Status().Label(); l != "Asleep" {
		t.Fatalf("label %q", l)
	}

	c, err := net.Dial("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	magic := []byte{0x00, 0xff, 0xff, 0x00, 0xfe, 0xfe, 0xfe, 0xfe, 0xfd, 0xfd, 0xfd, 0xfd, 0x12, 0x34, 0x56, 0x78}
	ping := append(append([]byte{0x01, 0, 0, 0, 0, 0, 0, 0, 1}, magic...), make([]byte, 8)...)
	c.Write(ping)
	buf := make([]byte, 1500)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("no answer while asleep: %v", err)
	}
	if !strings.Contains(string(buf[:n]), "MCPE;Sleepy island;") {
		t.Fatalf("pong doesn't name the server: %q", buf[:n])
	}

	c.Write(append(append([]byte{0x05}, magic...), 11))
	waitState(t, inst, Running)

	evs := m.Activity().List(sv.ID, 0)
	var texts []string
	for _, e := range evs {
		texts = append(texts, e.Text)
	}
	all := strings.Join(texts, " | ")
	for _, want := range []string{"Asleep: test", "Waking up: A Bedrock player is joining"} {
		if !strings.Contains(all, want) {
			t.Errorf("activity is missing %q: %s", want, all)
		}
	}
	if strings.Contains(all, "Stopped") {
		t.Errorf("going to sleep was recorded as a stop: %s", all)
	}

	// Stopping a sleeping server lets go of its port.
	inst.Sleep("Asleep again")
	waitState(t, inst, Sleeping)
	inst.Stop()
	waitState(t, inst, Stopped)
	pc, err := net.ListenPacket("udp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("port still held after stopping: %v", err)
	}
	pc.Close()
}
