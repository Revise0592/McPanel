// Package wake stands in for a sleeping server. It holds the server's port,
// answers the multiplayer list so the server still shows up, and reports
// when a player tries to join so the real server can be started.
//
// Java Edition uses TCP: the panel speaks just enough of the handshake,
// status and login protocol to show a description and turn the player away
// with a message. Bedrock Edition (and Geyser) use RakNet over UDP: the
// panel answers pings and treats a connection request as a join. Newer
// Geyser also takes NetherNet (WebRTC) joins, which start with HTTP
// signaling over TCP on the Bedrock port: the panel answers its status
// check and treats a join offer as a join.
package wake

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Info is what the multiplayer list shows while the server sleeps.
type Info struct {
	MOTD       string
	MaxPlayers int
	// Favicon is a "data:image/png;base64,..." URI, or empty.
	Favicon string
	// Version is the Minecraft version, shown by Bedrock clients.
	Version string
}

// Waker holds a sleeping server's ports. Close releases them.
type Waker struct {
	info   func() Info
	onJoin func(name string)

	mu      sync.Mutex
	closed  bool
	tcp     []net.Listener
	udp     []net.PacketConn
	http    []*http.Server
	wg      sync.WaitGroup
	guid    uint64
	started time.Time
}

// New returns a waker that calls info for the server list and onJoin when a
// player tries to join. The name is empty for Bedrock players,
// whose name isn't sent before the connection is set up.
func New(info func() Info, onJoin func(name string)) *Waker {
	return &Waker{info: info, onJoin: onJoin, guid: uint64(time.Now().UnixNano()), started: time.Now()}
}

// ListenJava holds a Java Edition TCP address, such as ":25565".
func (w *Waker) ListenJava(addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		l.Close()
		return errors.New("closed")
	}
	w.tcp = append(w.tcp, l)
	w.wg.Add(1)
	w.mu.Unlock()
	go func() {
		defer w.wg.Done()
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go w.serveJava(c)
		}
	}()
	return nil
}

// ListenBedrock holds a Bedrock Edition UDP address, such as ":19132".
func (w *Waker) ListenBedrock(addr string) error {
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		pc.Close()
		return errors.New("closed")
	}
	w.udp = append(w.udp, pc)
	w.wg.Add(1)
	w.mu.Unlock()
	go func() {
		defer w.wg.Done()
		buf := make([]byte, 2048)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if reply, join := w.bedrockPacket(buf[:n], localPort(pc.LocalAddr())); join {
				w.join("")
			} else if reply != nil {
				pc.WriteTo(reply, from)
			}
		}
	}()
	return nil
}

// ListenSignaling holds a NetherNet signaling TCP address, such as
// ":19132". Geyser serves this on the same port number as RakNet's UDP.
func (w *Waker) ListenSignaling(addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           http.HandlerFunc(w.serveSignaling),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		l.Close()
		return errors.New("closed")
	}
	w.http = append(w.http, srv)
	w.wg.Add(1)
	w.mu.Unlock()
	go func() {
		defer w.wg.Done()
		srv.Serve(l)
	}()
	return nil
}

// Close releases every port. When it returns, the ports can be reused.
func (w *Waker) Close() {
	w.mu.Lock()
	w.closed = true
	tcp, udp, hs := w.tcp, w.udp, w.http
	w.tcp, w.udp, w.http = nil, nil, nil
	w.mu.Unlock()
	for _, l := range tcp {
		l.Close()
	}
	for _, pc := range udp {
		pc.Close()
	}
	for _, srv := range hs {
		srv.Close()
	}
	w.wg.Wait()
}

func (w *Waker) join(name string) { go w.onJoin(name) }

func localPort(a net.Addr) int {
	if u, ok := a.(*net.UDPAddr); ok {
		return u.Port
	}
	return 0
}

// --- Java Edition ---

// Messages shown to Java players.
const (
	sleepingLine = "§7Asleep. Join to wake it up."
	wakingKick   = "§eThe server is starting up.\n\n§7Try joining again in a minute."
)

const maxPacket = 64 << 10

func (w *Waker) serveJava(c net.Conn) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(c)
	if b, err := r.Peek(1); err != nil || b[0] == 0xFE {
		// Legacy ping from clients before 1.7: not worth answering.
		return
	}
	id, body, err := readPacket(r)
	if err != nil || id != 0x00 {
		return
	}
	proto, err := readVarInt(body)
	if err != nil {
		return
	}
	if _, err := readString(body, 255*4); err != nil {
		return
	}
	var port uint16
	if err := binary.Read(body, binary.BigEndian, &port); err != nil {
		return
	}
	next, err := readVarInt(body)
	if err != nil {
		return
	}
	switch next {
	case 1:
		w.javaStatus(c, r, proto)
	case 2, 3:
		id, body, err := readPacket(r)
		if err != nil || id != 0x00 {
			return
		}
		name, err := readString(body, 16*4)
		if err != nil {
			return
		}
		msg, _ := json.Marshal(map[string]string{"text": wakingKick})
		writePacket(c, 0x00, appendString(nil, string(msg)))
		w.join(name)
	}
}

func (w *Waker) javaStatus(c net.Conn, r *bufio.Reader, proto int32) {
	if id, _, err := readPacket(r); err != nil || id != 0x00 {
		return
	}
	in := w.info()
	motd := strings.SplitN(in.MOTD, "\n", 2)[0]
	type version struct {
		Name     string `json:"name"`
		Protocol int32  `json:"protocol"`
	}
	type players struct {
		Max    int `json:"max"`
		Online int `json:"online"`
	}
	resp := struct {
		Version     version           `json:"version"`
		Players     players           `json:"players"`
		Description map[string]string `json:"description"`
		Favicon     string            `json:"favicon,omitempty"`
	}{
		// The client's own protocol, so the list doesn't call the server
		// incompatible; the real server decides that when it's awake.
		Version:     version{Name: "Asleep", Protocol: proto},
		Players:     players{Max: in.MaxPlayers},
		Description: map[string]string{"text": motd + "\n" + sleepingLine},
		Favicon:     in.Favicon,
	}
	b, _ := json.Marshal(resp)
	if err := writePacket(c, 0x00, appendString(nil, string(b))); err != nil {
		return
	}
	id, body, err := readPacket(r)
	if err != nil || id != 0x01 {
		return
	}
	payload, _ := io.ReadAll(body)
	writePacket(c, 0x01, payload)
}

func readVarInt(r io.ByteReader) (int32, error) {
	var v uint32
	for i := 0; i < 5; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		v |= uint32(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return int32(v), nil
		}
	}
	return 0, errors.New("varint too long")
}

func appendVarInt(b []byte, v int32) []byte {
	u := uint32(v)
	for u >= 0x80 {
		b = append(b, byte(u)|0x80)
		u >>= 7
	}
	return append(b, byte(u))
}

func appendString(b []byte, s string) []byte {
	return append(appendVarInt(b, int32(len(s))), s...)
}

func readString(r *bytes.Reader, max int) (string, error) {
	n, err := readVarInt(r)
	if err != nil {
		return "", err
	}
	if n < 0 || int(n) > max || int(n) > r.Len() {
		return "", errors.New("bad string")
	}
	b := make([]byte, n)
	io.ReadFull(r, b)
	return string(b), nil
}

// readPacket reads one uncompressed packet: its ID and the rest of its body.
func readPacket(r *bufio.Reader) (int32, *bytes.Reader, error) {
	n, err := readVarInt(r)
	if err != nil {
		return 0, nil, err
	}
	if n <= 0 || n > maxPacket {
		return 0, nil, fmt.Errorf("bad packet length %d", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return 0, nil, err
	}
	body := bytes.NewReader(b)
	id, err := readVarInt(body)
	return id, body, err
}

func writePacket(w io.Writer, id int32, data []byte) error {
	inner := append(appendVarInt(nil, id), data...)
	_, err := w.Write(append(appendVarInt(nil, int32(len(inner))), inner...))
	return err
}

// --- Bedrock Edition (RakNet) ---

var rakMagic = []byte{0x00, 0xff, 0xff, 0x00, 0xfe, 0xfe, 0xfe, 0xfe, 0xfd, 0xfd, 0xfd, 0xfd, 0x12, 0x34, 0x56, 0x78}

const (
	rakPing         = 0x01
	rakPingOpen     = 0x02
	rakPong         = 0x1c
	rakOpenRequest1 = 0x05
)

// bedrockProtocol is sent in the server list answer. Clients show the
// server whatever it says; the real server checks versions once awake.
const bedrockProtocol = 0

// bedrockPacket answers a RakNet packet. join is true for a connection
// request, which means a player picked the server.
func (w *Waker) bedrockPacket(p []byte, port int) (reply []byte, join bool) {
	if len(p) == 0 {
		return nil, false
	}
	switch p[0] {
	case rakPing, rakPingOpen:
		if len(p) < 1+8+16 || !bytes.Equal(p[9:25], rakMagic) {
			return nil, false
		}
		in := w.info()
		motd := strings.ReplaceAll(strings.SplitN(in.MOTD, "\n", 2)[0], ";", ",")
		ver := in.Version
		if ver == "" || ver == "latest" || ver == "unknown" {
			ver = "1.0"
		}
		id := fmt.Sprintf("MCPE;%s;%d;%s;0;%d;%d;%s;Survival;1;%d;%d;0;",
			motd, bedrockProtocol, ver, max(in.MaxPlayers, 1), w.guid, "Asleep - join to wake it up", port, port+1)
		out := []byte{rakPong}
		out = append(out, p[1:9]...) // the client's time
		out = binary.BigEndian.AppendUint64(out, w.guid)
		out = append(out, rakMagic...)
		out = binary.BigEndian.AppendUint16(out, uint16(len(id)))
		return append(out, id...), false
	case rakOpenRequest1:
		if len(p) >= 17 && bytes.Equal(p[1:17], rakMagic) {
			return nil, true
		}
	}
	return nil, false
}

// netherNetStatus is the server list answer for NetherNet, as Geyser's
// built-in signaling writes it.
type netherNetStatus struct {
	DataVersion    int    `json:"dataVersion"`
	Name           string `json:"name"`
	Protocol       int    `json:"protocol"`
	Version        string `json:"version"`
	Level          string `json:"level"`
	Players        int    `json:"players"`
	MaxPlayers     int    `json:"maxPlayers"`
	GameType       int    `json:"gameType"`
	Editor         bool   `json:"editor"`
	Hardcore       bool   `json:"hardcore"`
	OnlineAuth     bool   `json:"onlineAuth"`
	SelfSignedAuth bool   `json:"selfSignedAuth"`
	Nonce          string `json:"nonce"`
	TransportLayer int    `json:"transportLayer"`
	Connection     int    `json:"connection"`
}

// serveSignaling answers NetherNet signaling. GET /v1/join is the status
// check behind the server list; POST /v1/join/<network id> carries a
// player's join offer, which wakes the server. The player is turned away
// as if the server were full, and can join once it's up.
func (w *Waker) serveSignaling(rw http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/v1/join":
		if r.Method != http.MethodGet {
			rw.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		in := w.info()
		// The client's own protocol and version, so the list doesn't call
		// the server outdated; the real server checks once awake.
		q := r.URL.Query()
		proto, _ := strconv.Atoi(q.Get("protocol"))
		ver := q.Get("version")
		if ver == "" {
			ver = in.Version
		}
		body, _ := json.Marshal(netherNetStatus{
			DataVersion: 7,
			Name:        strings.SplitN(in.MOTD, "\n", 2)[0],
			Protocol:    proto,
			Version:     ver,
			Level:       "Asleep - join to wake it up",
			MaxPlayers:  max(in.MaxPlayers, 1),
			OnlineAuth:  true,
			Nonce:       fmt.Sprintf("%016x", w.guid),
			// NetherNet, over LAN-style WebRTC signaling.
			TransportLayer: 2,
			Connection:     4,
		})
		rw.Header().Set("Content-Type", "application/json")
		rw.Write(body)
	case strings.HasPrefix(r.URL.Path, "/v1/join/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/join/")
		if id == "" || strings.Contains(id, "/") {
			rw.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Method != http.MethodPost {
			rw.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		io.Copy(io.Discard, http.MaxBytesReader(rw, r.Body, 64<<10))
		w.join("")
		rw.WriteHeader(http.StatusServiceUnavailable)
	default:
		rw.WriteHeader(http.StatusNotFound)
	}
}
