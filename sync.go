package main

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type Syncer struct {
	cfg     *Config
	client  *Client
	emit    *Emitter
	state   *State
	library string
	saveDir string
}

func (s *Syncer) hookRel(rel string) string {
	r, err := filepath.Rel(s.library, filepath.Join(s.saveDir, filepath.FromSlash(rel)))
	if err != nil {
		return rel
	}
	return filepath.ToSlash(r)
}

func kindOf(name string) string {
	return strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
}

// sanitizeSegment makes one path segment safe for the Kobo's FAT32 storage.
func sanitizeSegment(seg string) string {
	var b strings.Builder
	for _, r := range seg {
		if r < 0x20 || strings.ContainsRune(`\/:*?"<>|`, r) {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	out := strings.TrimRight(b.String(), ". ")
	if out == "" {
		out = "_"
	}
	for len(out) > 200 {
		out = out[:len(out)-1]
	}
	return strings.TrimRight(out, ". ")
}

func sanitizeRelPath(rel string) string {
	segs := strings.Split(rel, "/")
	for i, seg := range segs {
		segs[i] = sanitizeSegment(seg)
	}
	return strings.Join(segs, "/")
}

// mapRemote sanitizes remote paths and resolves collisions (after
// sanitization or case-folding, since FAT32 is case-insensitive) by
// suffixing a short hash of the original path.
func mapRemote(files []RemoteFile) map[string]RemoteFile {
	sort.Slice(files, func(i, j int) bool { return files[i].RelPath < files[j].RelPath })
	out := make(map[string]RemoteFile, len(files))
	seen := make(map[string]bool, len(files))
	for _, f := range files {
		rel := sanitizeRelPath(f.RelPath)
		folded := strings.ToLower(rel)
		if seen[folded] {
			sum := sha1.Sum([]byte(f.RelPath))
			ext := path.Ext(rel)
			rel = fmt.Sprintf("%s-%x%s", strings.TrimSuffix(rel, ext), sum[:2], ext)
			folded = strings.ToLower(rel)
		}
		seen[folded] = true
		out[rel] = f
	}
	return out
}

// scanLocal returns the set of allowed-kind files under saveDir, keyed by
// slash-separated path relative to saveDir. Dotfiles and partials are skipped.
func (s *Syncer) scanLocal() (map[string]bool, error) {
	local := map[string]bool{}
	err := filepath.WalkDir(s.saveDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			if d.IsDir() && p != s.saveDir {
				return filepath.SkipDir
			}
			// Clean up partials left over from an interrupted run.
			if !d.IsDir() && strings.HasSuffix(name, ".partial") {
				os.Remove(p)
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !s.cfg.kindAllowed(kindOf(name)) {
			return nil
		}
		rel, err := filepath.Rel(s.saveDir, p)
		if err != nil {
			return err
		}
		local[filepath.ToSlash(rel)] = true
		return nil
	})
	return local, err
}

func (s *Syncer) Run(ctx context.Context) error {
	s.emit.Notify("Listing WebDAV server…")
	listed, err := s.client.List(ctx, *s.cfg.Recursive)
	if err != nil {
		return fmt.Errorf("listing failed: %w", err)
	}
	var books []RemoteFile
	for _, f := range listed {
		if s.cfg.kindAllowed(kindOf(f.RelPath)) {
			books = append(books, f)
		}
	}
	remote := mapRemote(books)

	local, err := s.scanLocal()
	if err != nil {
		return fmt.Errorf("scanning %s: %w", s.saveDir, err)
	}

	var toDownload []string
	for rel, rf := range remote {
		prev, tracked := s.state.Files[rel]
		switch {
		case !local[rel]:
			toDownload = append(toDownload, rel)
		case !tracked:
			// Present on disk but untracked (e.g. state lost): adopt it
			// rather than re-downloading.
			s.state.Files[rel] = FileState{Href: rf.Href, ETag: rf.ETag, Modified: rf.Modified, Size: rf.Size}
		case changed(prev, rf):
			toDownload = append(toDownload, rel)
		}
	}
	sort.Strings(toDownload)

	var toRemove []string
	if *s.cfg.DeleteRemoved {
		for rel := range s.state.Files {
			if _, ok := remote[rel]; !ok && local[rel] {
				toRemove = append(toRemove, rel)
			}
		}
	}
	sort.Strings(toRemove)

	if len(toDownload) == 0 && len(toRemove) == 0 {
		s.emit.Notify("Library is up to date.")
		return s.state.save(s.saveDir)
	}
	s.emit.Notify(fmt.Sprintf("Syncing: %d new, %d removed.", len(toDownload), len(toRemove)))

	for _, rel := range toRemove {
		if ctx.Err() != nil {
			break
		}
		abs := filepath.Join(s.saveDir, filepath.FromSlash(rel))
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			s.emit.Notify(fmt.Sprintf("Couldn't remove %s: %v", rel, err))
			continue
		}
		delete(s.state.Files, rel)
		s.emit.RemoveDocument(s.hookRel(rel))
		pruneEmptyDirs(filepath.Dir(abs), s.saveDir)
	}
	s.state.save(s.saveDir)

	done := 0
	for i, rel := range toDownload {
		if ctx.Err() != nil {
			break
		}
		rf := remote[rel]
		if err := s.download(ctx, rel, rf); err != nil {
			if ctx.Err() != nil {
				break
			}
			s.emit.Notify(fmt.Sprintf("Failed: %s: %v", path.Base(rel), err))
			continue
		}
		done++
		s.emit.Notify(fmt.Sprintf("Downloaded %s (%d/%d)", path.Base(rel), i+1, len(toDownload)))
	}

	if err := s.state.save(s.saveDir); err != nil {
		return err
	}
	if ctx.Err() != nil {
		s.emit.Notify("Sync interrupted.")
	} else {
		s.emit.Notify(fmt.Sprintf("Sync complete: %d downloaded, %d removed.", done, len(toRemove)))
	}
	return nil
}

func changed(prev FileState, rf RemoteFile) bool {
	if prev.ETag != "" && rf.ETag != "" {
		return prev.ETag != rf.ETag
	}
	return prev.Modified != rf.Modified || prev.Size != rf.Size
}

func (s *Syncer) download(ctx context.Context, rel string, rf RemoteFile) error {
	abs := filepath.Join(s.saveDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return err
	}
	body, length, err := s.client.Get(ctx, rf.Href)
	if err != nil {
		return err
	}
	defer body.Close()

	tmp := filepath.Join(filepath.Dir(abs), "."+filepath.Base(abs)+".partial")
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && length > 0 && n != length {
		err = fmt.Errorf("short download: got %d of %d bytes", n, length)
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, abs); err != nil {
		os.Remove(tmp)
		return err
	}

	s.state.Files[rel] = FileState{Href: rf.Href, ETag: rf.ETag, Modified: rf.Modified, Size: n}
	s.state.save(s.saveDir)
	s.emit.AddDocument(DocInfo{
		Added: platoTimestamp(time.Now()),
		File:  FileInfo{Path: s.hookRel(rel), Kind: kindOf(rel), Size: n},
	})
	return nil
}

func pruneEmptyDirs(dir, stop string) {
	for dir != stop && strings.HasPrefix(dir, stop) {
		if os.Remove(dir) != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}
