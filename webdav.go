package main

import (
	"context"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// RemoteFile is one entry from a WebDAV listing.
type RemoteFile struct {
	Href     string // decoded URL path on the server
	RelPath  string // path relative to the sync root, forward slashes
	Size     int64
	ETag     string
	Modified string // raw getlastmodified value; only used for change comparison
	IsDir    bool
}

type Client struct {
	http     *http.Client
	base     *url.URL // sync root collection, path ends with "/"
	user     string
	pass     string
	maxDepth int
}

func NewClient(cfg *Config) (*Client, error) {
	base, err := url.Parse(strings.TrimSpace(cfg.ServerURL))
	if err != nil {
		return nil, fmt.Errorf("server-url: %w", err)
	}
	if base.Scheme != "http" && base.Scheme != "https" {
		return nil, fmt.Errorf("server-url: unsupported scheme %q", base.Scheme)
	}
	if cfg.Path != "" {
		base.Path = path.Join(base.Path, cfg.Path)
	}
	if !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
	}
	transport := &http.Transport{
		ResponseHeaderTimeout: time.Duration(cfg.TimeoutSeconds) * time.Second,
	}
	if cfg.InsecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Client{
		// No overall client timeout: downloads of large books may
		// legitimately take longer; ctx and header timeouts still apply.
		http:     &http.Client{Transport: transport},
		base:     base,
		user:     cfg.Username,
		pass:     cfg.Password,
		maxDepth: 10,
	}, nil
}

const propfindBody = `<?xml version="1.0" encoding="utf-8"?>
<d:propfind xmlns:d="DAV:">
  <d:prop>
    <d:resourcetype/>
    <d:getcontentlength/>
    <d:getetag/>
    <d:getlastmodified/>
  </d:prop>
</d:propfind>`

type multistatus struct {
	Responses []davResponse `xml:"response"`
}

type davResponse struct {
	Href      string        `xml:"href"`
	Propstats []davPropstat `xml:"propstat"`
}

type davPropstat struct {
	Status string  `xml:"status"`
	Prop   davProp `xml:"prop"`
}

type davProp struct {
	ContentLength int64        `xml:"getcontentlength"`
	ETag          string       `xml:"getetag"`
	LastModified  string       `xml:"getlastmodified"`
	ResourceType  resourceType `xml:"resourcetype"`
}

type resourceType struct {
	Collection *struct{} `xml:"collection"`
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	if c.user != "" {
		req.SetBasicAuth(c.user, c.pass)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		return nil, fmt.Errorf("server rejected credentials (401)")
	}
	return resp, nil
}

// List walks the sync root with Depth: 1 PROPFINDs and returns all files
// (collections are recursed into when recursive is true).
func (c *Client) List(ctx context.Context, recursive bool) ([]RemoteFile, error) {
	var files []RemoteFile
	var walk func(colPath string, depth int) error
	walk = func(colPath string, depth int) error {
		entries, err := c.propfind(ctx, colPath)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.IsDir {
				if recursive && depth < c.maxDepth && e.RelPath != "" {
					if err := walk(e.Href, depth+1); err != nil {
						return err
					}
				}
				continue
			}
			files = append(files, e)
		}
		return nil
	}
	if err := walk(c.base.Path, 0); err != nil {
		return nil, err
	}
	return files, nil
}

// propfind lists one collection (Depth: 1). colPath is a decoded URL path.
func (c *Client) propfind(ctx context.Context, colPath string) ([]RemoteFile, error) {
	if !strings.HasSuffix(colPath, "/") {
		colPath += "/"
	}
	u := *c.base
	u.Path = colPath
	u.RawPath = ""
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", u.String(), strings.NewReader(propfindBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", `application/xml; charset="utf-8"`)
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMultiStatus {
		return nil, fmt.Errorf("PROPFIND %s: %s", u.Redacted(), resp.Status)
	}
	var ms multistatus
	if err := xml.NewDecoder(resp.Body).Decode(&ms); err != nil {
		return nil, fmt.Errorf("PROPFIND %s: bad XML: %w", u.Redacted(), err)
	}

	var out []RemoteFile
	for _, r := range ms.Responses {
		hrefPath, err := hrefToPath(r.Href)
		if err != nil {
			continue
		}
		// Skip the collection's own entry.
		if strings.TrimSuffix(hrefPath, "/") == strings.TrimSuffix(colPath, "/") {
			continue
		}
		prop, ok := okProp(r.Propstats)
		if !ok {
			continue
		}
		rel := strings.TrimPrefix(strings.TrimSuffix(hrefPath, "/"), strings.TrimSuffix(c.base.Path, "/"))
		rel = strings.TrimPrefix(rel, "/")
		out = append(out, RemoteFile{
			Href:     hrefPath,
			RelPath:  rel,
			Size:     prop.ContentLength,
			ETag:     prop.ETag,
			Modified: prop.LastModified,
			IsDir:    prop.ResourceType.Collection != nil,
		})
	}
	return out, nil
}

// hrefToPath normalizes a WebDAV href (absolute path or full URL, possibly
// percent-encoded) into a decoded URL path.
func hrefToPath(href string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil {
		return "", err
	}
	return u.Path, nil
}

func okProp(propstats []davPropstat) (davProp, bool) {
	for _, ps := range propstats {
		if strings.Contains(ps.Status, "200") {
			return ps.Prop, true
		}
	}
	return davProp{}, false
}

// Ping checks whether the server is reachable and answering WebDAV requests.
func (c *Client) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", c.base.String(), strings.NewReader(propfindBody))
	if err != nil {
		return err
	}
	req.Header.Set("Depth", "0")
	req.Header.Set("Content-Type", `application/xml; charset="utf-8"`)
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode != http.StatusMultiStatus {
		return fmt.Errorf("PROPFIND %s: %s", c.base.Redacted(), resp.Status)
	}
	return nil
}

// Available probes whether the file's content can actually be served, using
// a one-byte ranged GET. Servers backed by cloud storage (e.g. WsgiDAV over
// iCloud Drive) list placeholder files whose content reads fail with 5xx;
// HEAD can't detect this because the body is never opened.
func (c *Client) Available(ctx context.Context, hrefPath string) (bool, error) {
	u := *c.base
	u.Path = hrefPath
	u.RawPath = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := c.do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent:
		return true, nil
	case resp.StatusCode >= 500:
		return false, nil
	default:
		return false, fmt.Errorf("GET %s: %s", u.Redacted(), resp.Status)
	}
}

// Get streams the file at the given decoded server path.
func (c *Client) Get(ctx context.Context, hrefPath string) (io.ReadCloser, int64, error) {
	u := *c.base
	u.Path = hrefPath
	u.RawPath = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, 0, fmt.Errorf("GET %s: %s", u.Redacted(), resp.Status)
	}
	return resp.Body, resp.ContentLength, nil
}
