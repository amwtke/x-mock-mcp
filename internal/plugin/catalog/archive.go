package catalog

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"xmock.local/x-mock-mcp/pluginapi"
)

func (c *Catalog) extract(ctx context.Context, p, digest string) (m pluginapi.Manifest, temp string, err error) {
	fail := func(message string) (pluginapi.Manifest, string, error) {
		return m, "", pluginapi.Invalid("%s", message)
	}
	f, err := os.Open(p)
	if err != nil {
		return m, "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return m, "", err
	}
	if !st.Mode().IsRegular() || st.Size() > 64<<20 {
		return fail("archive must be a regular file of at most 64 MiB")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return m, "", err
	}
	if hex.EncodeToString(h.Sum(nil)) != digest {
		return fail("archive SHA-256 mismatch")
	}
	z, err := zip.NewReader(f, st.Size())
	if err != nil {
		return m, "", err
	}
	if len(z.File) > 1024 {
		return fail("too many archive entries")
	}
	names := map[string]bool{}
	var mf *zip.File
	var total uint64
	for _, entry := range z.File {
		if err = ctx.Err(); err != nil {
			return m, "", err
		}
		name := strings.TrimSuffix(entry.Name, "/")
		if !pluginapi.SafeRelative(name) || names[name] {
			return fail("unsafe or duplicate archive path")
		}
		names[name] = true
		if entry.Mode()&os.ModeSymlink != 0 || (!entry.FileInfo().IsDir() && !entry.Mode().IsRegular()) {
			return fail("archive links and special files are forbidden")
		}
		if entry.UncompressedSize64 > 256<<20 || total > 256<<20-entry.UncompressedSize64 {
			return fail("expanded archive too large")
		}
		total += entry.UncompressedSize64
		if name == "manifest.json" {
			if entry.UncompressedSize64 > 1<<20 || entry.FileInfo().IsDir() {
				return fail("invalid manifest entry")
			}
			mf = entry
		}
	}
	if mf == nil {
		return fail("manifest.json missing")
	}
	r, err := mf.Open()
	if err != nil {
		return m, "", err
	}
	raw, err := io.ReadAll(io.LimitReader(r, (1<<20)+1))
	r.Close()
	if err != nil {
		return m, "", err
	}
	if err = pluginapi.Decode(raw, &m); err != nil {
		return m, "", err
	}
	if err = m.Validate(); err != nil {
		return m, "", err
	}
	if m.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		return fail("plugin platform does not match host")
	}
	if len(m.Files) == 0 {
		return fail("empty package")
	}
	for name := range m.Files {
		if !names[name] {
			return fail("declared file missing")
		}
	}
	temp, err = os.MkdirTemp(filepath.Join(c.root, "plugins"), ".install-")
	if err != nil {
		return m, "", err
	}
	ok := false
	stagingDir := temp
	defer func() {
		if !ok {
			os.RemoveAll(stagingDir)
		}
	}()
	for _, entry := range z.File {
		if err = ctx.Err(); err != nil {
			return m, "", err
		}
		if entry.FileInfo().IsDir() {
			continue
		}
		want, listed := m.Files[entry.Name]
		if entry.Name != "manifest.json" && !listed {
			return fail("unlisted file in package")
		}
		dest := filepath.Join(temp, filepath.FromSlash(entry.Name))
		if err = os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return m, "", err
		}
		mode := os.FileMode(0600)
		if entry.Name == m.Entrypoint {
			if entry.Mode().Perm()&0111 == 0 {
				return fail("entrypoint is not executable")
			}
			mode = 0700
		}
		out, e := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if e != nil {
			return m, "", e
		}
		in, e := entry.Open()
		if e != nil {
			out.Close()
			return m, "", e
		}
		h = sha256.New()
		n, e := io.Copy(io.MultiWriter(out, h), io.LimitReader(in, int64(entry.UncompressedSize64)+1))
		in.Close()
		closeErr := out.Close()
		if e != nil {
			return m, "", e
		}
		if closeErr != nil {
			return m, "", closeErr
		}
		if n != int64(entry.UncompressedSize64) {
			return fail("expanded size mismatch")
		}
		if listed && hex.EncodeToString(h.Sum(nil)) != want {
			return fail("file digest mismatch")
		}
	}
	ok = true
	return m, temp, nil
}
