package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"
)

// FileInfo mirrors Plato's metadata::FileInfo (camelCase JSON).
type FileInfo struct {
	Path string `json:"path"` // relative to the library root
	Kind string `json:"kind"` // lowercase extension
	Size int64  `json:"size"`
}

// DocInfo mirrors the subset of Plato's metadata::Info we populate.
// Empty string fields are omitted so Plato fills them from the document.
type DocInfo struct {
	Title      string   `json:"title,omitempty"`
	Author     string   `json:"author,omitempty"`
	Year       string   `json:"year,omitempty"`
	Identifier string   `json:"identifier,omitempty"`
	Added      string   `json:"added"` // "%Y-%m-%d %H:%M:%S", naive local time
	File       FileInfo `json:"file"`
}

type event struct {
	Type    string   `json:"type"`
	Message string   `json:"message,omitempty"`
	Info    *DocInfo `json:"info,omitempty"`
	Path    string   `json:"path,omitempty"`
	Enable  *bool    `json:"enable,omitempty"`
}

// Emitter writes line-delimited JSON events for Plato to stdout.
type Emitter struct {
	mu  sync.Mutex
	enc *json.Encoder
}

func NewEmitter(w io.Writer) *Emitter {
	return &Emitter{enc: json.NewEncoder(w)}
}

func (e *Emitter) send(ev event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.enc.Encode(ev) // Encode appends the newline
}

func (e *Emitter) Notify(message string) {
	e.send(event{Type: "notify", Message: message})
}

func (e *Emitter) AddDocument(info DocInfo) {
	e.send(event{Type: "addDocument", Info: &info})
}

func (e *Emitter) RemoveDocument(path string) {
	e.send(event{Type: "removeDocument", Path: path})
}

func (e *Emitter) SetWifi(enable bool) {
	e.send(event{Type: "setWifi", Enable: &enable})
}

func platoTimestamp(t time.Time) string {
	return t.Format("2006-01-02 15:04:05")
}

// watchStdin reads line-delimited JSON events from Plato and signals on the
// returned channel when the network comes up. Unknown events are ignored.
// The channel is closed when stdin closes.
func watchStdin(r io.Reader) <-chan struct{} {
	netUp := make(chan struct{}, 1)
	go func() {
		defer close(netUp)
		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			var ev struct {
				Type   string `json:"type"`
				Status string `json:"status"`
			}
			if json.Unmarshal(scanner.Bytes(), &ev) != nil {
				continue
			}
			if ev.Type == "network" && ev.Status == "up" {
				select {
				case netUp <- struct{}{}:
				default:
				}
			}
		}
	}()
	return netUp
}

// waitForNetwork blocks until Plato reports the network is up, stdin closes,
// or ctx is cancelled. Returns true if the network came up.
func waitForNetwork(ctx context.Context, netUp <-chan struct{}) bool {
	select {
	case _, ok := <-netUp:
		return ok
	case <-ctx.Done():
		return false
	}
}

// waitForServer returns once the server answers a ping: Plato's online flag
// only flips on an observed network-up event, so it can be stale in both
// directions — the server itself is the source of truth. It re-probes on
// Plato's network-up events and every pollEvery, giving up after maxWait.
func waitForServer(ctx context.Context, ping func(context.Context) error, netUp <-chan struct{}, maxWait, pollEvery time.Duration) bool {
	if ping(ctx) == nil {
		return true
	}
	deadline := time.After(maxWait)
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline:
			return false
		case _, ok := <-netUp:
			if !ok {
				netUp = nil // stdin closed; keep polling on the ticker
				continue
			}
			if ping(ctx) == nil {
				return true
			}
		case <-tick.C:
			if ping(ctx) == nil {
				return true
			}
		}
	}
}
