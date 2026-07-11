package main

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// End-to-end: real run() with argv, Settings.json in cwd, an offline start
// that waits for Plato's network-up event, and a fake WebDAV server.
func TestRunEndToEnd(t *testing.T) {
	dav := &fakeDAV{files: map[string][]byte{"Book.epub": []byte("hello")}}
	srv := httptest.NewServer(dav.handler())
	defer srv.Close()

	cwd := t.TempDir()
	os.WriteFile(filepath.Join(cwd, "Settings.json"),
		[]byte(`{"server-url": "`+srv.URL+`"}`), 0644)
	t.Chdir(cwd)

	library := t.TempDir()
	saveDir := filepath.Join(library, "WebDAV")

	stdin := strings.NewReader(`{"type":"network","status":"up"}` + "\n")
	var stdout bytes.Buffer
	code := run([]string{library, saveDir, "false", "false"}, stdin, &stdout)
	if code != 0 {
		t.Fatalf("exit code %d\n%s", code, stdout.String())
	}

	data, err := os.ReadFile(filepath.Join(saveDir, "Book.epub"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("book not synced: %v %q", err, data)
	}
	out := stdout.String()
	for _, want := range []string{
		`{"type":"setWifi","enable":true}`,  // wifi was off
		`{"type":"setWifi","enable":false}`, // restored at exit
		`"type":"addDocument"`,
		`"path":"WebDAV/Book.epub"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in output:\n%s", want, out)
		}
	}
}
