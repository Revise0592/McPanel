package wake

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

func handshake(proto int32, next int32) []byte {
	b := appendVarInt(nil, proto)
	b = appendString(b, "localhost")
	b = binary.BigEndian.AppendUint16(b, 25565)
	return appendVarInt(b, next)
}

func packet(id int32, data []byte) []byte {
	var buf bytes.Buffer
	writePacket(&buf, id, data)
	return buf.Bytes()
}

func start(t *testing.T) (*Waker, string, chan string) {
	t.Helper()
	joined := make(chan string, 1)
	w := New(func() Info {
		return Info{MOTD: "Test world\nsecond line", MaxPlayers: 10, Version: "1.21.4"}
	}, func(name string) { joined <- name })
	if err := w.ListenJava("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	return w, w.tcp[0].Addr().String(), joined
}

func TestJavaStatus(t *testing.T) {
	_, addr, joined := start(t)
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	c.Write(packet(0, handshake(770, 1)))
	c.Write(packet(0, nil))
	r := bufio.NewReader(c)
	id, body, err := readPacket(r)
	if err != nil || id != 0 {
		t.Fatalf("status: id %d err %v", id, err)
	}
	s, err := readString(body, maxPacket)
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Version struct {
			Protocol int32 `json:"protocol"`
		} `json:"version"`
		Players struct {
			Max int `json:"max"`
		} `json:"players"`
		Description struct {
			Text string `json:"text"`
		} `json:"description"`
	}
	if err := json.Unmarshal([]byte(s), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Version.Protocol != 770 || resp.Players.Max != 10 || !strings.HasPrefix(resp.Description.Text, "Test world\n") {
		t.Fatalf("unexpected status %s", s)
	}
	c.Write(packet(1, []byte{1, 2, 3, 4, 5, 6, 7, 8}))
	id, body, err = readPacket(r)
	if err != nil || id != 1 || body.Len() != 8 {
		t.Fatalf("pong: id %d err %v", id, err)
	}
	select {
	case n := <-joined:
		t.Fatalf("a status ping woke the server (%q)", n)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestJavaLoginWakes(t *testing.T) {
	_, addr, joined := start(t)
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	c.Write(packet(0, handshake(770, 2)))
	c.Write(packet(0, appendString(nil, "Steve")))
	id, body, err := readPacket(bufio.NewReader(c))
	if err != nil || id != 0 {
		t.Fatalf("disconnect: id %d err %v", id, err)
	}
	if s, _ := readString(body, maxPacket); !strings.Contains(s, "starting up") {
		t.Fatalf("unexpected kick message %q", s)
	}
	select {
	case n := <-joined:
		if n != "Steve" {
			t.Fatalf("joined as %q", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("login didn't wake the server")
	}
}

func TestCloseFreesPort(t *testing.T) {
	w, addr, _ := start(t)
	w.Close()
	l, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("port still held after Close: %v", err)
	}
	l.Close()
}

func TestBedrock(t *testing.T) {
	joined := make(chan string, 1)
	w := New(func() Info { return Info{MOTD: "Island; home", MaxPlayers: 5, Version: "1.21.50"} }, func(n string) { joined <- n })
	if err := w.ListenBedrock("127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	c, err := net.Dial("udp", w.udp[0].LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	ping := []byte{rakPing, 0, 0, 0, 0, 0, 0, 0, 42}
	ping = append(ping, rakMagic...)
	ping = append(ping, make([]byte, 8)...)
	c.Write(ping)
	buf := make([]byte, 1500)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	p := buf[:n]
	if p[0] != rakPong || p[8] != 42 || !bytes.Equal(p[17:33], rakMagic) {
		t.Fatalf("bad pong % x", p)
	}
	id := string(p[35:])
	if !strings.HasPrefix(id, "MCPE;Island, home;") || strings.Count(id, ";") != 13 {
		t.Fatalf("bad server id %q", id)
	}
	open := append([]byte{rakOpenRequest1}, rakMagic...)
	c.Write(append(open, 11))
	select {
	case <-joined:
	case <-time.After(2 * time.Second):
		t.Fatal("connection request didn't wake the server")
	}
}
