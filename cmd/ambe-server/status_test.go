package main

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestLatencyPercentile(t *testing.T) {
	var l latency
	if l.percentile(99) != 0 || l.max != 0 {
		t.Fatal("empty histogram")
	}
	for us := 1; us <= 100; us++ {
		l.add(time.Duration(us) * time.Microsecond)
	}
	if p := l.percentile(99); p != 99 {
		t.Errorf("p99 of 1..100 µs = %d", p)
	}
	if p := l.percentile(100); p != 100 {
		t.Errorf("p100 of 1..100 µs = %d", p)
	}
	if l.max != 100*time.Microsecond {
		t.Errorf("max %v", l.max)
	}
	for i := 0; i < 200; i++ {
		l.add(30 * time.Millisecond)
	}
	if p := l.percentile(99); p != latencyCap {
		t.Errorf("p99 beyond the cap = %d", p)
	}
	if l.max != 30*time.Millisecond {
		t.Errorf("max beyond the cap %v", l.max)
	}
}

// statusKeys are the status file's fields, as the bridge's monitoring reads them.
var statusKeys = []string{
	"client_states", "clients_seen", "decoded", "decoded72", "encode_us_max", "encode_us_p99",
	"encoded", "ignored", "last_decode", "last_encode", "lookahead", "resets", "started",
	"state", "updated", "version",
}

func TestStatusSnapshot(t *testing.T) {
	cfg := testConfig()
	cfg.resetGap = 200 * time.Millisecond
	s, advance := clocked(cfg)
	start := s.now()
	st := s.status(start, "abc1234")
	if st.LastEncode != nil || st.LastDecode != nil || st.Encoded != 0 || st.EncodeUsP99 != 0 {
		t.Fatalf("fresh server: %+v", st)
	}

	pcm := pcmFrames(3, 150)
	wire := wireFrames(pcm)
	for _, f := range pcm {
		s.handle(f, "10.0.0.1:5000")
		advance(framePeriod)
	}
	s.handle(wire[0], "10.0.0.1:5000")
	advance(time.Second) // the next encode and decode start new transmissions
	s.handle(pcm[0], "10.0.0.2:5000")
	encodedAt := s.now()
	s.handle(make([]byte, 9), "10.0.0.1:5000")
	s.handle(make([]byte, 5), "10.0.0.1:5000")

	st = s.status(encodedAt.Add(time.Second), "abc1234")
	want := status{
		Version: "abc1234", Encoded: 4, Decoded: 1, Decoded72: 1, Ignored: 1,
		ClientsSeen: 2, ClientStates: 0, Lookahead: 2, State: "shared", Resets: 2,
	}
	got := st
	got.Started, got.Updated, got.LastEncode, got.LastDecode, got.EncodeUsMax, got.EncodeUsP99 = 0, 0, nil, nil, 0, 0
	if !reflect.DeepEqual(got, want) {
		t.Errorf("status %+v, want %+v", got, want)
	}
	if st.LastEncode == nil || *st.LastEncode != unixSeconds(encodedAt) {
		t.Errorf("last_encode %v, want %v", st.LastEncode, unixSeconds(encodedAt))
	}
	if st.LastDecode == nil || *st.LastDecode != unixSeconds(encodedAt) {
		t.Errorf("last_decode %v (the 9-byte decode counts)", st.LastDecode)
	}
	if st.Updated != unixSeconds(encodedAt.Add(time.Second)) {
		t.Errorf("updated %v", st.Updated)
	}
	if st.EncodeUsMax <= 0 || st.EncodeUsP99 > st.EncodeUsMax {
		t.Errorf("encode times: max %d µs, p99 %d µs", st.EncodeUsMax, st.EncodeUsP99)
	}

	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(data, &m)
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if !reflect.DeepEqual(keys, statusKeys) {
		t.Errorf("JSON keys %v", keys)
	}

	cfg.perClient = true
	pc := newServer(cfg)
	pc.handle(pcm[0], "a")
	pc.handle(pcm[0], "b")
	if st := pc.status(time.Now(), ""); st.State != "client" || st.ClientStates != 2 || st.ClientsSeen != 2 {
		t.Errorf("per-client status: state %q, %d states, %d seen", st.State, st.ClientStates, st.ClientsSeen)
	}
}

func TestWriteStatus(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stats.json")
	s := newServer(testConfig())
	s.handle(pcmFrames(1, 150)[0], "a")
	for i := 0; i < 2; i++ { // the second write replaces the first
		if err := writeStatus(path, s.status(time.Now(), "v")); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var st status
	if err := json.Unmarshal(data, &st); err != nil || st.Encoded != 1 || st.Version != "v" {
		t.Fatalf("read back %+v, %v", st, err)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o644 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temporary files left behind: %v", entries)
	}
	if err := writeStatus(filepath.Join(dir, "missing", "stats.json"), st); err == nil {
		t.Error("writing into a missing directory succeeded")
	}
}

// TestServeStatusWhileIdle checks that status snapshots keep coming with no
// traffic at all, and while requests are being answered.
func TestServeStatusWhileIdle(t *testing.T) {
	conn, err := listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ticks := make(chan time.Time, 100)
	cfg := loopConfig{statusInterval: 30 * time.Millisecond, status: func(now time.Time) { ticks <- now }}
	done := make(chan error, 1)
	go func() { done <- serve(conn, newServer(testConfig()), cfg) }()

	wait := func(n int) {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for i := 0; i < n; i++ {
			select {
			case <-ticks:
			case <-deadline:
				t.Fatalf("only %d of %d status snapshots", i, n)
			}
		}
	}
	wait(3) // idle

	cli, err := listenClient(conn)
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	stop := make(chan struct{})
	go func() {
		f := pcmFrames(1, 150)[0]
		for {
			select {
			case <-stop:
				return
			default:
			}
			cli.Write(f)
			time.Sleep(5 * time.Millisecond)
		}
	}()
	wait(3) // busy
	close(stop)

	conn.Close()
	if err := <-done; err != nil {
		t.Errorf("serve: %v", err)
	}
}

func listenClient(conn *net.UDPConn) (*net.UDPConn, error) {
	return net.DialUDP("udp", nil, conn.LocalAddr().(*net.UDPAddr))
}

func TestVersionOf(t *testing.T) {
	for _, c := range []struct {
		module, rev string
		dirty       bool
		want        string
	}{
		{"(devel)", "e0e33cca66a89e44a59b2795a00cf3e695ab215d", false, "e0e33cc"},
		{"(devel)", "e0e33cca66a89e44a59b2795a00cf3e695ab215d", true, "e0e33cc-dirty"},
		{"v0.0.0-20261007045101-e0e33cca66a8", "", false, "e0e33cc"},
		{"v1.2.4-0.20261007045101-e0e33cca66a8", "", false, "e0e33cc"},
		{"v1.2.3", "", false, "v1.2.3"},
		{"(devel)", "", false, "unknown"},
	} {
		if got := versionOf(c.module, c.rev, c.dirty); got != c.want {
			t.Errorf("versionOf(%q, %q, %v) = %q, want %q", c.module, c.rev, c.dirty, got, c.want)
		}
	}
}
