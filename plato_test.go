package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestEventGoldenLines(t *testing.T) {
	var buf bytes.Buffer
	e := NewEmitter(&buf)
	e.Notify("hello")
	e.AddDocument(DocInfo{
		Added: "2026-07-11 12:00:00",
		File:  FileInfo{Path: "WebDAV/foo.epub", Kind: "epub", Size: 42},
	})
	e.RemoveDocument("WebDAV/foo.epub")
	e.SetWifi(true)
	e.SetWifi(false)

	want := []string{
		`{"type":"notify","message":"hello"}`,
		`{"type":"addDocument","info":{"added":"2026-07-11 12:00:00","file":{"path":"WebDAV/foo.epub","kind":"epub","size":42}}}`,
		`{"type":"removeDocument","path":"WebDAV/foo.epub"}`,
		`{"type":"setWifi","enable":true}`,
		`{"type":"setWifi","enable":false}`,
	}
	got := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d:\n%s", len(got), len(want), buf.String())
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d:\n got %s\nwant %s", i, got[i], want[i])
		}
	}
}

func TestWatchStdinNetworkUp(t *testing.T) {
	input := strings.Join([]string{
		`not json`,
		`{"type":"search","results":[]}`,
		`{"type":"network","status":"down"}`,
		`{"type":"network","status":"up"}`,
	}, "\n")
	netUp := watchStdin(strings.NewReader(input))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if !waitForNetwork(ctx, netUp) {
		t.Fatal("expected network-up signal")
	}
}

func TestWaitForServer(t *testing.T) {
	ctx := context.Background()

	// Reachable immediately: no waiting.
	ok := waitForServer(ctx, func(context.Context) error { return nil }, nil, time.Second, time.Hour)
	if !ok {
		t.Fatal("immediately reachable server reported unreachable")
	}

	// Unreachable until a network-up event arrives.
	up := false
	ping := func(context.Context) error {
		if up {
			return nil
		}
		return context.DeadlineExceeded
	}
	netUp := make(chan struct{}, 1)
	go func() {
		up = true
		netUp <- struct{}{}
	}()
	if !waitForServer(ctx, ping, netUp, 2*time.Second, time.Hour) {
		t.Fatal("server should be reachable after network-up event")
	}

	// Never reachable: gives up at maxWait (polling fast).
	if waitForServer(ctx, func(context.Context) error { return context.DeadlineExceeded }, nil, 50*time.Millisecond, 10*time.Millisecond) {
		t.Fatal("unreachable server reported reachable")
	}
}

func TestWaitForNetworkCancelled(t *testing.T) {
	// A reader that never produces a network event: block until ctx cancels.
	netUp := watchStdin(strings.NewReader(""))
	// Drain the closed-channel case explicitly: empty input closes the channel,
	// which reports false.
	ctx := context.Background()
	if waitForNetwork(ctx, netUp) {
		t.Fatal("expected false when stdin closes without network-up")
	}
}
