package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestSyncer(t *testing.T, dav *fakeDAV) (*Syncer, *bytes.Buffer) {
	t.Helper()
	c, _ := newTestClient(t, dav, Config{})
	library := t.TempDir()
	saveDir := filepath.Join(library, "WebDAV")
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	cfg, _ := c, dav
	_ = cfg
	conf := &Config{ServerURL: "unused"}
	tr, dl := true, true
	conf.Recursive, conf.DeleteRemoved = &tr, &dl
	conf.AllowedKinds = defaultKinds
	return &Syncer{
		cfg:     conf,
		client:  c,
		emit:    NewEmitter(&out),
		state:   loadState(saveDir),
		library: library,
		saveDir: saveDir,
	}, &out
}

func eventTypes(t *testing.T, out *bytes.Buffer) []string {
	t.Helper()
	var types []string
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var ev struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("invalid JSON line: %s", line)
		}
		types = append(types, ev.Type)
	}
	return types
}

func TestSyncDownloadsAndIsIdempotent(t *testing.T) {
	dav := &fakeDAV{files: map[string][]byte{
		"One.epub":     []byte("book one"),
		"Sub/Two.pdf":  []byte("book two!"),
		"ignored.docx": []byte("not a book"),
	}}
	s, out := newTestSyncer(t, dav)

	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"One.epub", "Sub/Two.pdf"} {
		if _, err := os.Stat(filepath.Join(s.saveDir, filepath.FromSlash(p))); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(s.saveDir, "ignored.docx")); err == nil {
		t.Error("docx should not have been downloaded")
	}
	var adds int
	for _, ty := range eventTypes(t, out) {
		if ty == "addDocument" {
			adds++
		}
	}
	if adds != 2 {
		t.Fatalf("got %d addDocument events, want 2\n%s", adds, out.String())
	}
	// addDocument paths must be library-relative.
	if !strings.Contains(out.String(), `"path":"WebDAV/One.epub"`) {
		t.Errorf("expected library-relative path in events:\n%s", out.String())
	}

	// Second run: nothing to do, no add/remove events.
	out.Reset()
	s.state = loadState(s.saveDir)
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, ty := range eventTypes(t, out) {
		if ty == "addDocument" || ty == "removeDocument" {
			t.Fatalf("second run emitted %s:\n%s", ty, out.String())
		}
	}
}

func TestSyncRemovesDeletedButKeepsSideloaded(t *testing.T) {
	dav := &fakeDAV{files: map[string][]byte{
		"Keep.epub": []byte("keep"),
		"Gone.epub": []byte("gone"),
	}}
	s, out := newTestSyncer(t, dav)
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	// User sideloads a book; server deletes Gone.epub.
	sideload := filepath.Join(s.saveDir, "Mine.epub")
	os.WriteFile(sideload, []byte("mine"), 0644)
	delete(dav.files, "Gone.epub")

	out.Reset()
	s.state = loadState(s.saveDir)
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.saveDir, "Gone.epub")); err == nil {
		t.Error("Gone.epub should have been removed")
	}
	if _, err := os.Stat(sideload); err != nil {
		t.Error("sideloaded Mine.epub must not be removed")
	}
	if !strings.Contains(out.String(), `"type":"removeDocument","path":"WebDAV/Gone.epub"`) {
		t.Errorf("missing removeDocument event:\n%s", out.String())
	}
}

func TestSyncRedownloadsChangedFile(t *testing.T) {
	dav := &fakeDAV{files: map[string][]byte{"Book.epub": []byte("v1")}}
	s, _ := newTestSyncer(t, dav)
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	dav.files["Book.epub"] = []byte("version two") // size change → new etag in fakeDAV
	s.state = loadState(s.saveDir)
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(s.saveDir, "Book.epub"))
	if string(data) != "version two" {
		t.Fatalf("got %q, want updated content", data)
	}
}

func TestSyncCancelledBetweenFiles(t *testing.T) {
	dav := &fakeDAV{files: map[string][]byte{"A.epub": []byte("a")}}
	s, out := newTestSyncer(t, dav)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// A context error is fine (main treats it as a clean exit); anything
	// else, or a download happening, is not.
	if err := s.Run(ctx); err != nil && ctx.Err() == nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "addDocument") {
		t.Fatalf("cancelled sync should not download:\n%s", out.String())
	}
}

func TestSyncSkipsUnavailableFiles(t *testing.T) {
	files := map[string][]byte{}
	broken := map[string]bool{}
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"} {
		files[n+".epub"] = []byte(n)
		if n != "a" {
			broken[n+".epub"] = true
		}
	}
	dav := &fakeDAV{files: files, broken: broken}
	s, out := newTestSyncer(t, dav)
	if err := s.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Unavailable files are detected up front and skipped: no failure
	// notifications, no per-file spam — just listing, plan, progress,
	// and a summary mentioning the skips.
	notifies, adds := 0, 0
	for _, ty := range eventTypes(t, out) {
		switch ty {
		case "notify":
			notifies++
		case "addDocument":
			adds++
		}
	}
	if adds != 1 {
		t.Fatalf("got %d addDocument events, want 1:\n%s", adds, out.String())
	}
	if notifies > 4 {
		t.Fatalf("got %d notify events, want few:\n%s", notifies, out.String())
	}
	if strings.Contains(out.String(), "failed") {
		t.Fatalf("skips must not be reported as failures:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "11 not yet available on server") {
		t.Fatalf("missing skip note in summary:\n%s", out.String())
	}
}

func TestSanitizeSegment(t *testing.T) {
	cases := map[string]string{
		`a:b*c?.epub`:  "a_b_c_.epub",
		"trailing... ": "trailing",
		"ok name.pdf":  "ok name.pdf",
	}
	for in, want := range cases {
		if got := sanitizeSegment(in); got != want {
			t.Errorf("sanitizeSegment(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMapRemoteCaseCollision(t *testing.T) {
	files := []RemoteFile{
		{RelPath: "Book.epub"},
		{RelPath: "book.epub"},
	}
	m := mapRemote(files)
	if len(m) != 2 {
		t.Fatalf("expected 2 distinct entries, got %v", m)
	}
	folded := map[string]bool{}
	for k := range m {
		lk := strings.ToLower(k)
		if folded[lk] {
			t.Fatalf("case-insensitive collision survived: %v", m)
		}
		folded[lk] = true
	}
}

func TestPartialCleanup(t *testing.T) {
	dav := &fakeDAV{files: map[string][]byte{}}
	s, _ := newTestSyncer(t, dav)
	stale := filepath.Join(s.saveDir, ".old.epub.partial")
	os.WriteFile(stale, []byte("junk"), 0644)
	if _, err := s.scanLocal(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("stale partial should have been deleted")
	}
}
