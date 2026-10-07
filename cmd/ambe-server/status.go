package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"time"
)

// latencyCap is the top of the latency histogram in microseconds: one frame
// period.  Slower requests are counted in the last bucket.
const latencyCap = 20000

// latency records request times since start-up in 1 µs buckets, so that
// percentiles are exact below latencyCap.
type latency struct {
	n    uint64
	max  time.Duration
	hist [latencyCap + 1]uint32
}

func (l *latency) add(d time.Duration) {
	l.n++
	if d > l.max {
		l.max = d
	}
	us := d.Microseconds()
	if us > latencyCap {
		us = latencyCap
	}
	l.hist[us]++
}

// percentile returns the p-th percentile in microseconds (nearest rank), 0
// with no samples and latencyCap for anything at or above it.
func (l *latency) percentile(p uint64) int64 {
	if l.n == 0 {
		return 0
	}
	rank := (p*l.n + 99) / 100 // ceil(p·n/100)
	if rank < 1 {
		rank = 1
	}
	var seen uint64
	for us, c := range l.hist {
		seen += uint64(c)
		if seen >= rank {
			return int64(us)
		}
	}
	return latencyCap
}

// status is the JSON status file.  Times are Unix seconds; last_encode and
// last_decode are null until the first request of that kind.
type status struct {
	Version      string   `json:"version"`
	Started      float64  `json:"started"`
	Updated      float64  `json:"updated"`
	Encoded      uint64   `json:"encoded"`
	Decoded      uint64   `json:"decoded"`
	Decoded72    uint64   `json:"decoded72"`
	Ignored      uint64   `json:"ignored"`
	ClientsSeen  uint64   `json:"clients_seen"`
	ClientStates int      `json:"client_states"`
	LastEncode   *float64 `json:"last_encode"`
	LastDecode   *float64 `json:"last_decode"`
	EncodeUsMax  int64    `json:"encode_us_max"`
	EncodeUsP99  int64    `json:"encode_us_p99"`
	Lookahead    int      `json:"lookahead"`
	State        string   `json:"state"`
	Tones        bool     `json:"tones"`
	Denoise      bool     `json:"denoise"`
	Resets       uint64   `json:"resets"`
}

func unixSeconds(t time.Time) float64 { return float64(t.UnixMilli()) / 1000 }

func optionalUnix(t time.Time) *float64 {
	if t.IsZero() {
		return nil
	}
	v := unixSeconds(t)
	return &v
}

// status snapshots the server's counters at time now.
func (s *server) status(now time.Time, version string) status {
	st := status{
		Version:      version,
		Started:      unixSeconds(s.started),
		Updated:      unixSeconds(now),
		Encoded:      s.stats.encoded,
		Decoded:      s.stats.decoded,
		Decoded72:    s.stats.decoded72,
		Ignored:      s.stats.ignored,
		ClientsSeen:  s.stats.clients,
		ClientStates: len(s.clients),
		LastEncode:   optionalUnix(s.stats.lastEncode),
		LastDecode:   optionalUnix(s.stats.lastDecode),
		EncodeUsMax:  s.encodeUs.max.Microseconds(),
		EncodeUsP99:  s.encodeUs.percentile(99),
		Lookahead:    s.cfg.encoder.Lookahead,
		State:        "shared",
		Tones:        s.cfg.encoder.Tones,
		Denoise:      s.cfg.encoder.Denoise,
		Resets:       s.stats.resets,
	}
	if s.cfg.perClient {
		st.State = "client"
	}
	return st
}

// writeStatus writes st to path atomically: to a temporary file in the same
// directory, renamed over path, so readers never see a partial file.
func writeStatus(path string, st status) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(append(data, '\n'))
	if err == nil {
		err = f.Chmod(0o644)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// statusWriter writes status snapshots on its own goroutine, so file system
// delays never hold up requests.  A snapshot that arrives while the previous
// one is still being written is dropped; the next interval brings a newer one.
type statusWriter struct {
	path string
	ch   chan status
	done chan struct{}
}

func startStatusWriter(path string) *statusWriter {
	w := &statusWriter{path: path, ch: make(chan status, 1), done: make(chan struct{})}
	go func() {
		defer close(w.done)
		failing := false
		for st := range w.ch {
			err := writeStatus(w.path, st)
			if err != nil && !failing {
				log.Printf("ambe-server: status file: %v", err)
			} else if err == nil && failing {
				log.Printf("ambe-server: status file: writing again")
			}
			failing = err != nil
		}
	}()
	return w
}

func (w *statusWriter) submit(st status) {
	select {
	case w.ch <- st:
	default:
	}
}

// close waits for pending writes to finish.
func (w *statusWriter) close() {
	close(w.ch)
	<-w.done
}

var pseudoVersion = regexp.MustCompile(`^v\d+\.\d+\.\d+-(?:.*\.)?\d{14}-([0-9a-f]{12})$`)

// buildVersion identifies the build: the short commit hash (with "-dirty"
// for uncommitted changes) when built from a checkout, the commit hash of a
// pseudo-version (go install ...@main), or the module version of a release.
func buildVersion() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, dirty := "", false
	for _, kv := range bi.Settings {
		switch kv.Key {
		case "vcs.revision":
			rev = kv.Value
		case "vcs.modified":
			dirty = kv.Value == "true"
		}
	}
	return versionOf(bi.Main.Version, rev, dirty)
}

func versionOf(module, rev string, dirty bool) string {
	if rev != "" {
		if len(rev) > 7 {
			rev = rev[:7]
		}
		if dirty {
			rev += "-dirty"
		}
		return rev
	}
	if m := pseudoVersion.FindStringSubmatch(module); m != nil {
		return m[1][:7]
	}
	if module == "" || module == "(devel)" {
		return "unknown"
	}
	return module
}
