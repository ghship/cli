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
	server    *httptest.Server
	mu        sync.Mutex
	downloads map[string]int
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
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimPrefix(req.URL.Path, "/")
		if strings.HasPrefix(name, "tarballs/") {
			name = "@ghship/cli-" + strings.TrimSuffix(strings.TrimPrefix(name, "tarballs/"), ".tgz")
			item, ok := packages[name]
			if !ok {
				http.NotFound(w, req)
				return
			}
			r.mu.Lock()
			r.downloads[name]++
			r.mu.Unlock()
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(item.data)
			return
		}
		item, ok := packages[name]
		if !ok {
			http.NotFound(w, req)
			return
		}
		meta := map[string]any{}
		for k, v := range item.meta {
			meta[k] = v
		}
		version := meta["version"].(string)
		sum := sha512.Sum512(item.data)
		meta["dist"] = map[string]string{"tarball": r.server.URL + "/tarballs/" + strings.TrimPrefix(name, "@ghship/cli-") + ".tgz", "integrity": "sha512-" + base64.StdEncoding.EncodeToString(sum[:])}
		w.Header().Set("Content-Type", "application/json")
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
