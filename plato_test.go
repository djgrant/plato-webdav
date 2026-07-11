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
