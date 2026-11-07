package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// The registry is fixed in released builds. Tests can replace it at build time.
var standaloneRegistry = "https://registry.npmjs.org"

type updatePackage struct {
	Name    string   `json:"name"`
	Version string   `json:"version"`
	OS      []string `json:"os"`
	CPU     []string `json:"cpu"`
	Dist    struct {
		Tarball   string `json:"tarball"`
		Integrity string `json:"integrity"`
	} `json:"dist"`
}

func verifyUpdateIntegrity(data []byte, integrity string) error {
	if !strings.HasPrefix(integrity, "sha512-") {
		return errors.New("unsupported package integrity")
	}
	want, err := base64.StdEncoding.Strict().DecodeString(strings.TrimPrefix(integrity, "sha512-"))
	if err != nil || len(want) != sha512.Size {
		return errors.New("invalid package integrity")
	}
	sum := sha512.Sum512(data)
	if !bytes.Equal(sum[:], want) {
		return errors.New("package integrity mismatch")
	}
	return nil
}

func updateArchiveFile(data []byte, name string, limit int64) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("invalid package archive")
	}
	defer gz.Close()
	tr := tar.NewReader(io.LimitReader(gz, 128<<20))
	var result []byte
	found := false
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, errors.New("invalid package archive")
		}
		if h.Name != name {
			continue
		}
		if found || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA) || h.Size < 0 || h.Size > limit {
			return nil, errors.New("invalid package file")
		}
		found = true
		result, e = io.ReadAll(io.LimitReader(tr, limit+1))
		if e != nil || int64(len(result)) != h.Size {
			return nil, errors.New("invalid package file")
		}
	}
	if !found {
		return nil, errors.New("package file missing")
	}
	return result, nil
}

func updateDownload(client *http.Client, base *url.URL, location string, limit int64) ([]byte, error) {
	u, err := url.Parse(location)
	if err != nil || !u.IsAbs() || u.User != nil || u.Scheme != base.Scheme || !strings.EqualFold(u.Host, base.Host) {
		return nil, errors.New("untrusted package URL")
	}
	req, err := http.NewRequestWithContext(operationContext, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot download update: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update registry returned HTTP %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("update download exceeds size limit")
	}
	return data, nil
}

func updateMetadata(client *http.Client, base *url.URL, name, version string) (updatePackage, error) {
	var meta updatePackage
	location := strings.TrimRight(base.String(), "/") + "/" + url.PathEscape(name) + "/" + url.PathEscape(version)
	data, err := updateDownload(client, base, location, 1<<20)
	if err != nil {
		return meta, err
	}
	if err = json.Unmarshal(data, &meta); err != nil {
		return meta, errors.New("invalid update metadata")
	}
	if meta.Name != name || meta.Version == "" || (version != "latest" && meta.Version != version) {
		return meta, errors.New("update metadata does not match request")
	}
	return meta, nil
}

func validateUpdateArchiveMetadata(data []byte, expected updatePackage) error {
	b, err := updateArchiveFile(data, "package/package.json", 1<<20)
	if err != nil {
		return err
	}
	var actual updatePackage
	if json.Unmarshal(b, &actual) != nil || actual.Name != expected.Name || actual.Version != expected.Version {
		return errors.New("package contents do not match metadata")
	}
	return nil
}

func standaloneDirectUpdate(old manifest, dir string, args []string, out io.Writer) error {
	if old.Binary == nil {
		return errors.New("standalone installation required")
	}
	for i := 0; i < len(args); i++ {
		if args[i] == "--bin-dir" {
			if i+1 == len(args) {
				return usage("--bin-dir requires a path")
			}
			requested, err := filepath.Abs(args[i+1])
			if err != nil {
				return err
			}
			if requested != filepath.Dir(old.Binary.Path) {
				return errors.New("bin directory differs from manifest")
			}
			i++
		} else if args[i] == "--dir" {
			i++
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	installed, err := filepath.EvalSymlinks(old.Binary.Path)
	if err != nil {
		return err
	}
	sameExecutable := exe == installed
	if runtime.GOOS == "windows" {
		sameExecutable = strings.EqualFold(exe, installed)
	}
	if !sameExecutable {
		return errors.New("run update from the managed standalone executable")
	}
	base, err := url.Parse(standaloneRegistry)
	if err != nil || base.Host == "" || base.User != nil || (base.Scheme != "https" && base.Scheme != "http") {
		return errors.New("invalid update registry")
	}
	client := &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 || req.URL.Scheme != base.Scheme || !strings.EqualFold(req.URL.Host, base.Host) {
			return errors.New("untrusted update redirect")
		}
		return nil
	}}
	main, err := updateMetadata(client, base, "@ghship/cli", "latest")
	if err != nil {
		return err
	}
	if !newerOrEqual(main.Version, old.Version) {
		return errors.New("downgrade refused")
	}
	if main.Version == old.Version {
		asJSON := false
		for _, arg := range args {
			if arg == "--json" {
				asJSON = true
			}
		}
		return emitResult(out, asJSON, map[string]any{"action": "update", "version": old.Version, "binary": old.Binary.Path, "directory": dir})
	}
	mainTar, err := updateDownload(client, base, main.Dist.Tarball, 40<<20)
	if err != nil {
		return err
	}
	if err = verifyUpdateIntegrity(mainTar, main.Dist.Integrity); err != nil {
		return err
	}
	if err = validateUpdateArchiveMetadata(mainTar, main); err != nil {
		return err
	}
	checksBytes, err := updateArchiveFile(mainTar, "package/checksums.json", 1<<20)
	if err != nil {
		return err
	}
	var checks map[string]string
	if json.Unmarshal(checksBytes, &checks) != nil {
		return errors.New("invalid binary checksums")
	}
	osName, arch := runtime.GOOS, runtime.GOARCH
	if osName == "windows" {
		osName = "win32"
	}
	if arch == "amd64" {
		arch = "x64"
	}
	if (osName != "win32" && osName != "darwin" && osName != "linux") || (arch != "x64" && arch != "arm64") {
		return errors.New("unsupported update platform")
	}
	target := osName + "-" + arch
	p, err := updateMetadata(client, base, "@ghship/cli-"+target, main.Version)
	if err != nil {
		return err
	}
	has := func(items []string, value string) bool {
		for _, item := range items {
			if item == value {
				return true
			}
		}
		return false
	}
	if !has(p.OS, osName) || !has(p.CPU, arch) {
		return errors.New("update platform does not match")
	}
	platformTar, err := updateDownload(client, base, p.Dist.Tarball, 40<<20)
	if err != nil {
		return err
	}
	if err = verifyUpdateIntegrity(platformTar, p.Dist.Integrity); err != nil {
		return err
	}
	if err = validateUpdateArchiveMetadata(platformTar, p); err != nil {
		return err
	}
	name := "ghship"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary, err := updateArchiveFile(platformTar, "package/bin/"+name, 64<<20)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(binary)
	if checks[target] != hex.EncodeToString(sum[:]) {
		return errors.New("update binary checksum mismatch")
	}
	tmp, err := os.MkdirTemp("", "ghship-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	candidate := filepath.Join(tmp, name)
	if err = os.WriteFile(candidate, binary, 0700); err != nil {
		return err
	}
	versionOutput, err := exec.CommandContext(operationContext, candidate, "version").Output()
	if err != nil || strings.TrimSpace(string(versionOutput)) != main.Version {
		return errors.New("downloaded CLI version does not match package")
	}
	childArgs := append([]string{"update", "--standalone"}, args...)
	child := exec.CommandContext(operationContext, candidate, childArgs...)
	child.Stdin = os.Stdin
	var childOutput bytes.Buffer
	child.Stdout = &childOutput
	child.Stderr = os.Stderr
	if err = child.Run(); err != nil {
		_, _ = io.Copy(out, &childOutput)
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return cliError{exit.ExitCode(), errors.New("standalone update failed")}
		}
		return err
	}
	manifestBytes, err := os.ReadFile(filepath.Join(dir, ".ghship.json"))
	if err != nil {
		return errors.New("update completed but installed manifest cannot be verified")
	}
	var updated manifest
	if json.Unmarshal(manifestBytes, &updated) != nil || updated.Version != main.Version || updated.Binary == nil || updated.Binary.Path != old.Binary.Path {
		return errors.New("update completed but installed manifest does not match")
	}
	if err = validateManaged(dir, updated); err != nil {
		return fmt.Errorf("update completed but verification failed: %w", err)
	}
	_, err = io.Copy(out, &childOutput)
	return err
}
