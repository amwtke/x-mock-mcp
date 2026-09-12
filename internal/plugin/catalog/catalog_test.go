package catalog

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"xmock.local/x-mock-mcp/pluginapi"
)

func bundle(t *testing.T, role pluginapi.Role, modify func(*pluginapi.Manifest), extra string) (string, string) {
	t.Helper()
	data := []byte("#!/bin/sh\nexit 0\n")
	sum := sha256.Sum256(data)
	m := pluginapi.Manifest{Descriptor: pluginapi.Descriptor{Ref: pluginapi.Reference{ID: "echo", Version: "0.1.0", Role: role}, APIMajor: 1, Contracts: []pluginapi.Contract{{ID: "echo", Version: 1}}, ConfigSchema: json.RawMessage(`{"type":"object"}`)}, Platform: runtime.GOOS + "/" + runtime.GOARCH, Entrypoint: "bin/echo", Files: map[string]string{"bin/echo": hex.EncodeToString(sum[:])}}
	if modify != nil {
		modify(&m)
	}
	p := filepath.Join(t.TempDir(), "plugin.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	b, _ := json.Marshal(m)
	for name, content := range map[string][]byte{"manifest.json": b, "bin/echo": data} {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0755)
		w, err := z.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = w.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if extra != "" {
		w, err := z.Create(extra)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte("extra"))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	archive, _ := os.ReadFile(p)
	digest := sha256.Sum256(archive)
	return p, hex.EncodeToString(digest[:])
}

func TestInstallUninstallIndependentRoles(t *testing.T) {
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	refs := []pluginapi.Reference{}
	for _, role := range []pluginapi.Role{pluginapi.Left, pluginapi.Right} {
		path, sha := bundle(t, role, nil, "")
		ref, err := c.Install(ctx, path, sha)
		if err != nil {
			t.Fatal(err)
		}
		refs = append(refs, ref)
		if _, err = c.Install(ctx, path, sha); err != nil {
			t.Fatal("identical install must be idempotent", err)
		}
		if _, err = c.Acquire(ref); pluginapi.Code(err) != "PLUGIN_NOT_ENABLED" {
			t.Fatal(err)
		}
		if err = c.Enable(ref); err != nil {
			t.Fatal(err)
		}
	}
	release, err := c.Acquire(refs[0])
	if err != nil {
		t.Fatal(err)
	}
	if pluginapi.Code(c.Disable(refs[0])) != "PLUGIN_IN_USE" {
		t.Fatal("disabled active plugin")
	}
	if pluginapi.Code(c.Uninstall(refs[0])) != "PLUGIN_IN_USE" {
		t.Fatal("removed active plugin")
	}
	release()
	release()
	if err = c.Disable(refs[0]); err != nil {
		t.Fatal(err)
	}
	if err = c.Uninstall(refs[0]); err != nil {
		t.Fatal(err)
	}
	list := c.List()
	if len(list) != 1 || list[0].Ref.Role != pluginapi.Right || !list[0].Enabled {
		t.Fatalf("right plugin changed: %+v", list)
	}
}

func TestRejectUnsafePackages(t *testing.T) {
	cases := map[string]struct {
		modify func(*pluginapi.Manifest)
		extra  string
	}{
		"traversal":      {extra: "../escape"},
		"unlisted":       {extra: "unlisted"},
		"wrong-platform": {modify: func(m *pluginapi.Manifest) { m.Platform = "other/arch" }},
		"digest":         {modify: func(m *pluginapi.Manifest) { m.Files["bin/echo"] = string(make([]byte, 64)) }},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c, _ := Open(t.TempDir())
			p, d := bundle(t, pluginapi.Left, tc.modify, tc.extra)
			if _, err := c.Install(context.Background(), p, d); err == nil {
				t.Fatal("unsafe package accepted")
			}
			if len(c.List()) != 0 {
				t.Fatal("failed installation registered")
			}
			leftovers, err := filepath.Glob(filepath.Join(c.root, "plugins", ".install-*"))
			if err != nil || len(leftovers) != 0 {
				t.Fatalf("failed installation leaked temporary files: %v %v", leftovers, err)
			}
		})
	}
}

func TestPersistFailureRollsBackInstall(t *testing.T) {
	c, _ := Open(t.TempDir())
	// A directory at the index path makes the existing index unreadable.
	if err := os.Mkdir(filepath.Join(c.root, "catalog.json"), 0700); err != nil {
		t.Fatal(err)
	}
	p, d := bundle(t, pluginapi.Right, nil, "")
	if _, err := c.Install(context.Background(), p, d); err == nil {
		t.Fatal("installation unexpectedly succeeded")
	}
	entries, _ := os.ReadDir(filepath.Join(c.root, "plugins"))
	if len(entries) != 0 {
		t.Fatalf("failed install left package data: %+v", entries)
	}
}

func TestSameVersionDifferentArchiveRejected(t *testing.T) {
	c, _ := Open(t.TempDir())
	ctx := context.Background()
	p, d := bundle(t, pluginapi.Left, nil, "")
	ref, err := c.Install(ctx, p, d)
	if err != nil {
		t.Fatal(err)
	}
	p2, d2 := bundle(t, pluginapi.Left, func(m *pluginapi.Manifest) {
		m.Descriptor.ConfigSchema = json.RawMessage(`{"type":"object","title":"changed"}`)
	}, "")
	if _, err = c.Install(ctx, p2, d2); pluginapi.Code(err) != "STATE_CONFLICT" {
		t.Fatal("replacement of fixed version accepted", err)
	}
	if _, err = c.Install(ctx, p, string(make([]byte, 64))); err == nil {
		t.Fatal("invalid archive checksum accepted")
	}
	if _, _, err = c.Package(ref); err != nil {
		t.Fatal("old package lost", err)
	}
}

func TestArchiveRejectsSymlinkAndDuplicate(t *testing.T) {
	for _, kind := range []string{"symlink", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := Open(t.TempDir())
			p := filepath.Join(t.TempDir(), "unsafe.zip")
			f, _ := os.Create(p)
			z := zip.NewWriter(f)
			h := &zip.FileHeader{Name: "link"}
			if kind == "symlink" {
				h.SetMode(os.ModeSymlink | 0777)
			} else {
				h.SetMode(0600)
			}
			w, _ := z.CreateHeader(h)
			w.Write([]byte("../escape"))
			if kind == "duplicate" {
				w, _ = z.Create("link")
				w.Write([]byte("again"))
			}
			z.Close()
			f.Close()
			b, _ := os.ReadFile(p)
			s := sha256.Sum256(b)
			if _, err := c.Install(context.Background(), p, hex.EncodeToString(s[:])); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func TestConcurrentAcquireBlocksUninstall(t *testing.T) {
	c, _ := Open(t.TempDir())
	p, d := bundle(t, pluginapi.Right, nil, "")
	ref, err := c.Install(context.Background(), p, d)
	if err != nil {
		t.Fatal(err)
	}
	c.Enable(ref)
	release, err := c.Acquire(ref)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := c.Acquire(ref)
			if e != nil {
				t.Error(e)
				return
			}
			defer r()
			if pluginapi.Code(c.Uninstall(ref)) != "PLUGIN_IN_USE" {
				t.Error("uninstalled active reference")
			}
		}()
	}
	wg.Wait()
}
