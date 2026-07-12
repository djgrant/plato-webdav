package main

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path"
	"sort"
	"strings"
	"testing"
)

// fakeDAV is an in-memory WebDAV server supporting PROPFIND (Depth 1) and GET.
type fakeDAV struct {
	prefix string            // URL path prefix of the DAV root, e.g. "/dav"
	files  map[string][]byte // slash paths relative to root, e.g. "Sub Dir/A Book.epub"
	broken map[string]bool   // paths whose GET returns 500 (e.g. dataless cloud files)
	user   string
	pass   string
}

func (f *fakeDAV) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.user != "" {
			u, p, ok := r.BasicAuth()
			if !ok || u != f.user || p != f.pass {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		rel := strings.Trim(strings.TrimPrefix(r.URL.Path, f.prefix), "/")
		switch r.Method {
		case "PROPFIND":
			f.propfind(w, rel)
		case http.MethodGet:
			if f.broken[rel] {
				http.Error(w, "Resource deadlock avoided", http.StatusInternalServerError)
				return
			}
			data, ok := f.files[rel]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Length", fmt.Sprint(len(data)))
			w.Write(data)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
}

func (f *fakeDAV) propfind(w http.ResponseWriter, rel string) {
	// Collect direct children of rel plus the collection itself.
	type entry struct {
		rel   string
		isDir bool
		size  int
	}
	entries := []entry{{rel: rel, isDir: true}}
	seenDirs := map[string]bool{}
	for p, data := range f.files {
		dir := path.Dir(p)
		if dir == "." {
			dir = ""
		}
		if dir == rel {
			entries = append(entries, entry{rel: p, size: len(data)})
		} else if strings.HasPrefix(dir, rel) {
			// Direct child collection of rel.
			rest := strings.Trim(strings.TrimPrefix(dir, rel), "/")
			child := strings.Split(rest, "/")[0]
			childRel := path.Join(rel, child)
			if child != "" && !seenDirs[childRel] {
				seenDirs[childRel] = true
				entries = append(entries, entry{rel: childRel, isDir: true})
			}
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })

	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">`)
	for _, e := range entries {
		// Percent-encode each segment like real servers do.
		href := f.prefix
		for _, seg := range strings.Split(e.rel, "/") {
			if seg != "" {
				href += "/" + url.PathEscape(seg)
			}
		}
		var buf strings.Builder
		xml.EscapeText(&buf, []byte(href))
		b.WriteString(`<d:response><d:href>` + buf.String())
		if e.isDir {
			b.WriteString(`/`)
		}
		b.WriteString(`</d:href><d:propstat><d:status>HTTP/1.1 200 OK</d:status><d:prop>`)
		if e.isDir {
			b.WriteString(`<d:resourcetype><d:collection/></d:resourcetype>`)
		} else {
			b.WriteString(`<d:resourcetype/>`)
			fmt.Fprintf(&b, `<d:getcontentlength>%d</d:getcontentlength>`, e.size)
			fmt.Fprintf(&b, `<d:getetag>"etag-%s-%d"</d:getetag>`, e.rel, e.size)
			b.WriteString(`<d:getlastmodified>Fri, 11 Jul 2026 10:00:00 GMT</d:getlastmodified>`)
		}
		b.WriteString(`</d:prop></d:propstat></d:response>`)
	}
	b.WriteString(`</d:multistatus>`)
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusMultiStatus)
	w.Write([]byte(b.String()))
}

func newTestClient(t *testing.T, dav *fakeDAV, cfg Config) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(dav.handler())
	t.Cleanup(srv.Close)
	cfg.ServerURL = srv.URL + dav.prefix
	if cfg.TimeoutSeconds == 0 {
		cfg.TimeoutSeconds = 5
	}
	c, err := NewClient(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c, srv
}

func TestListRecursiveWithEncodedNames(t *testing.T) {
	dav := &fakeDAV{
		prefix: "/remote.php/dav",
		files: map[string][]byte{
			"A Book.epub":         []byte("aaaa"),
			"Sub Dir/Amélie.pdf": []byte("bb"),
			"Sub Dir/Deep/x.cbz":  []byte("c"),
			"notes.docx":          []byte("ignored later by sync, listed here"),
		},
	}
	c, _ := newTestClient(t, dav, Config{})
	files, err := c.List(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int64{}
	for _, f := range files {
		if f.IsDir {
			t.Errorf("List returned a directory: %s", f.RelPath)
		}
		got[f.RelPath] = f.Size
	}
	want := map[string]int64{
		"A Book.epub":         4,
		"Sub Dir/Amélie.pdf": 2,
		"Sub Dir/Deep/x.cbz":  1,
		"notes.docx":          34,
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: size %d, want %d", k, got[k], v)
		}
	}
}

func TestListNonRecursive(t *testing.T) {
	dav := &fakeDAV{files: map[string][]byte{
		"top.epub":     []byte("xx"),
		"Sub/deep.pdf": []byte("yy"),
	}}
	c, _ := newTestClient(t, dav, Config{})
	files, err := c.List(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].RelPath != "top.epub" {
		t.Fatalf("got %+v, want just top.epub", files)
	}
}

func TestBasicAuth(t *testing.T) {
	dav := &fakeDAV{files: map[string][]byte{"a.epub": []byte("x")}, user: "u", pass: "p"}

	c, _ := newTestClient(t, dav, Config{Username: "u", Password: "p"})
	if _, err := c.List(context.Background(), true); err != nil {
		t.Fatalf("valid creds: %v", err)
	}

	bad, _ := newTestClient(t, dav, Config{Username: "u", Password: "wrong"})
	if _, err := bad.List(context.Background(), true); err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("expected credentials error, got %v", err)
	}
}

func TestPathPrefixConfig(t *testing.T) {
	dav := &fakeDAV{prefix: "/dav/Books", files: map[string][]byte{"b.epub": []byte("zz")}}
	srv := httptest.NewServer(dav.handler())
	defer srv.Close()
	cfg := &Config{ServerURL: srv.URL + "/dav", Path: "Books", TimeoutSeconds: 5}
	c, err := NewClient(cfg)
	if err != nil {
		t.Fatal(err)
	}
	files, err := c.List(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].RelPath != "b.epub" {
		t.Fatalf("got %+v", files)
	}
}
