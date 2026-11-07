package main

import (
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// registry serves only the exact local platform tarballs. It records every
// tarball request so the test can distinguish metadata from binary downloads.
type registry struct {
	server         *httptest.Server
	mu             sync.Mutex
	downloads      map[string]int
	registerMain   func(string, string) error
	swapPlatform   func(string, string) (func(), error)
	setMainFailure func(string) func()
}

func startRegistry(root, tmp string, npm func(...string) ([]byte, error)) (*registry, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	type item struct {
		meta map[string]any
		data []byte
	}
	packages := map[string]item{}
	for _, entry := range entries {
		if !entry.IsDir() {
			return nil, fmt.Errorf("unexpected platform package entry")
		}
		dir := filepath.Join(root, entry.Name())
		b, e := os.ReadFile(filepath.Join(dir, "package.json"))
		if e != nil {
			return nil, e
		}
		var meta map[string]any
		if e = json.Unmarshal(b, &meta); e != nil {
			return nil, e
		}
		name, ok := meta["name"].(string)
		if !ok || name != "@ghship/cli-"+entry.Name() {
			return nil, fmt.Errorf("invalid platform package")
		}
		packed, e := npm("pack", dir, "--json", "--pack-destination", tmp)
		if e != nil {
			return nil, e
		}
		record, e := parseNpmPack(packed, name)
		if e != nil {
			return nil, fmt.Errorf("invalid platform tarball")
		}
		data, e := os.ReadFile(filepath.Join(tmp, record.Filename))
		if e != nil {
			return nil, e
		}
		packages[name] = item{meta: meta, data: data}
	}
	r := &registry{downloads: map[string]int{}}
	mainFailure := ""
	r.setMainFailure = func(value string) func() {
		r.mu.Lock()
		previous := mainFailure
		mainFailure = value
		r.mu.Unlock()
		return func() { r.mu.Lock(); mainFailure = previous; r.mu.Unlock() }
	}
	r.swapPlatform = func(dir, archive string) (func(), error) {
		b, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err != nil {
			return nil, err
		}
		var meta map[string]any
		if err = json.Unmarshal(b, &meta); err != nil {
			return nil, err
		}
		name, ok := meta["name"].(string)
		if !ok || !strings.HasPrefix(name, "@ghship/cli-") {
			return nil, fmt.Errorf("invalid platform package")
		}
		data, err := os.ReadFile(archive)
		if err != nil {
			return nil, err
		}
		r.mu.Lock()
		old, ok := packages[name]
		if ok {
			packages[name] = item{meta: meta, data: data}
		}
		r.mu.Unlock()
		if !ok {
			return nil, fmt.Errorf("unknown platform package")
		}
		return func() { r.mu.Lock(); packages[name] = old; r.mu.Unlock() }, nil
	}
	r.registerMain = func(dir, archive string) error {
		metaBytes, err := os.ReadFile(filepath.Join(dir, "package.json"))
		if err != nil {
			return err
		}
		var meta map[string]any
		if err = json.Unmarshal(metaBytes, &meta); err != nil {
			return err
		}
		if meta["name"] != "@ghship/cli" {
			return fmt.Errorf("invalid main package")
		}
		data, err := os.ReadFile(archive)
		if err != nil {
			return err
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		packages["@ghship/cli"] = item{meta: meta, data: data}
		return nil
	}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimPrefix(req.URL.Path, "/")
		if strings.HasPrefix(name, "tarballs/") {
			target := strings.TrimSuffix(strings.TrimPrefix(name, "tarballs/"), ".tgz")
			name = "@ghship/cli-" + target
			if target == "main" {
				name = "@ghship/cli"
			}
			r.mu.Lock()
			item, ok := packages[name]
			if ok && name != "@ghship/cli" {
				r.downloads[name]++
			}
			r.mu.Unlock()
			if !ok {
				http.NotFound(w, req)
				return
			}
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(item.data)
			return
		}
		requestedVersion := ""
		if i := strings.LastIndex(name, "/"); i > 0 && strings.Contains(name[:i], "/") {
			requestedVersion, name = name[i+1:], name[:i]
		}
		r.mu.Lock()
		failure := mainFailure
		item, ok := packages[name]
		r.mu.Unlock()
		if name == "@ghship/cli" && requestedVersion == "latest" {
			if failure == "disconnect" {
				if h, ok := w.(http.Hijacker); ok {
					c, _, err := h.Hijack()
					if err == nil {
						_ = c.Close()
					}
					return
				}
			}
			if failure == "unavailable" {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			if failure == "redirect" {
				http.Redirect(w, req, "https://example.com/untrusted", http.StatusFound)
				return
			}
		}
		if !ok {
			http.NotFound(w, req)
			return
		}
		meta := map[string]any{}
		for k, v := range item.meta {
			meta[k] = v
		}
		version := meta["version"].(string)
		if requestedVersion != "" && requestedVersion != "latest" && requestedVersion != version {
			http.NotFound(w, req)
			return
		}
		sum := sha512.Sum512(item.data)
		target := strings.TrimPrefix(name, "@ghship/cli-")
		if name == "@ghship/cli" {
			target = "main"
		}
		meta["dist"] = map[string]string{"tarball": r.server.URL + "/tarballs/" + target + ".tgz", "integrity": "sha512-" + base64.StdEncoding.EncodeToString(sum[:])}
		w.Header().Set("Content-Type", "application/json")
		if requestedVersion != "" {
			_ = json.NewEncoder(w).Encode(meta)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"name": name, "dist-tags": map[string]string{"latest": version}, "versions": map[string]any{version: meta}})
	}))
	return r, nil
}

func (r *registry) assertDownloads(target string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	expected := "@ghship/cli-" + target
	if r.downloads[expected] == 0 {
		return fmt.Errorf("matching platform binary was not downloaded")
	}
	for name := range r.downloads {
		if name != expected {
			return fmt.Errorf("nonmatching platform binary downloaded: %s", name)
		}
	}
	return nil
}
