package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"xmock.local/x-mock-mcp/pluginapi"
)

type Record struct {
	Ref             pluginapi.Reference `json:"ref"`
	Manifest        pluginapi.Manifest  `json:"manifest"`
	ArchiveSHA256   string              `json:"archive_sha256"`
	Enabled         bool                `json:"enabled"`
	ActiveInstances int                 `json:"active_instances"`
}
type Catalog struct {
	root string
	mu   sync.Mutex
}

func Open(projectRoot string) (*Catalog, error) {
	root, err := filepath.Abs(filepath.Join(projectRoot, ".x-mock"))
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(filepath.Join(root, "plugins"), 0700); err != nil {
		return nil, err
	}
	c := &Catalog{root: root}
	err = c.locked(false, func(records map[string]Record) error { return nil })
	return c, err
}
func (c *Catalog) locked(write bool, fn func(map[string]Record) error, rollback ...func()) (err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	f, err := os.OpenFile(filepath.Join(c.root, "catalog.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	defer func() {
		if err != nil {
			for _, undo := range rollback {
				undo()
			}
		}
	}()
	records := map[string]Record{}
	b, err := os.ReadFile(filepath.Join(c.root, "catalog.json"))
	if err == nil {
		if err = pluginapi.Decode(b, &records); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err = fn(records); err != nil {
		return err
	}
	if !write {
		return nil
	}
	b, err = json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(c.root, "catalog-*.tmp")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer os.Remove(name)
	if _, err = temp.Write(b); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, filepath.Join(c.root, "catalog.json"))
}
func (c *Catalog) packageDir(ref pluginapi.Reference) string {
	return filepath.Join(c.root, "plugins", string(ref.Role), ref.ID, ref.Version)
}
func record(records map[string]Record, ref pluginapi.Reference) (Record, error) {
	if err := ref.Validate(); err != nil {
		return Record{}, err
	}
	r, ok := records[ref.Key()]
	if !ok {
		return Record{}, pluginapi.Fail("PLUGIN_NOT_INSTALLED", "plugin version is not installed")
	}
	return r, nil
}
func (c *Catalog) Enable(ref pluginapi.Reference) error {
	return c.locked(true, func(rs map[string]Record) error {
		r, e := record(rs, ref)
		if e != nil {
			return e
		}
		r.Enabled = true
		rs[ref.Key()] = r
		return nil
	})
}
func (c *Catalog) Disable(ref pluginapi.Reference) error {
	return c.locked(true, func(rs map[string]Record) error {
		r, e := record(rs, ref)
		if e != nil {
			return e
		}
		if r.ActiveInstances > 0 {
			return pluginapi.Fail("PLUGIN_IN_USE", "destroy active bindings or finish preparation first")
		}
		r.Enabled = false
		rs[ref.Key()] = r
		return nil
	})
}
func (c *Catalog) Uninstall(ref pluginapi.Reference) error {
	var tomb string
	dest := c.packageDir(ref)
	err := c.locked(true, func(rs map[string]Record) error {
		r, e := record(rs, ref)
		if e != nil {
			return e
		}
		if r.ActiveInstances > 0 {
			return pluginapi.Fail("PLUGIN_IN_USE", "active references prevent uninstall")
		}
		if r.Enabled {
			return pluginapi.Fail("PLUGIN_IN_USE", "disable plugin before uninstall")
		}
		staging, createErr := os.MkdirTemp(filepath.Join(c.root, "plugins"), ".uninstall-")
		if createErr != nil {
			return createErr
		}
		tomb = staging
		if e = os.Rename(dest, filepath.Join(tomb, "package")); e != nil {
			return e
		}
		delete(rs, ref.Key())
		return nil
	}, func() {
		if tomb != "" {
			_ = os.Rename(filepath.Join(tomb, "package"), dest)
		}
	})
	if tomb != "" {
		_ = os.RemoveAll(tomb)
	}
	return err
}
func (c *Catalog) Acquire(ref pluginapi.Reference) (func(), error) {
	err := c.locked(true, func(rs map[string]Record) error {
		r, e := record(rs, ref)
		if e != nil {
			return e
		}
		if !r.Enabled {
			return pluginapi.Fail("PLUGIN_NOT_ENABLED", "enable plugin version first")
		}
		r.ActiveInstances++
		rs[ref.Key()] = r
		return nil
	})
	if err != nil {
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = c.locked(true, func(rs map[string]Record) error {
				r, e := record(rs, ref)
				if e != nil {
					return e
				}
				if r.ActiveInstances > 0 {
					r.ActiveInstances--
				}
				rs[ref.Key()] = r
				return nil
			})
		})
	}, nil
}
func (c *Catalog) List() []Record { rs, _ := c.Records(); return rs }
func (c *Catalog) Records() ([]Record, error) {
	var out []Record
	err := c.locked(false, func(rs map[string]Record) error {
		for _, r := range rs {
			out = append(out, r)
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Ref.Key() < out[j].Ref.Key() })
	return out, err
}
func (c *Catalog) Package(ref pluginapi.Reference) (pluginapi.Manifest, string, error) {
	var out Record
	err := c.locked(false, func(rs map[string]Record) error { var e error; out, e = record(rs, ref); return e })
	return out.Manifest, c.packageDir(ref), err
}

// RecoverReferences is only called after taking the daemon's exclusive project
// lock and ensuring the previous plugin pipes have closed.
func (c *Catalog) RecoverReferences() error {
	return c.locked(true, func(rs map[string]Record) error {
		for k, r := range rs {
			r.ActiveInstances = 0
			rs[k] = r
		}
		return nil
	})
}

func (c *Catalog) Install(ctx context.Context, archivePath, expectedSHA256 string) (pluginapi.Reference, error) {
	var ref pluginapi.Reference
	if !pluginapi.ValidDigest(expectedSHA256) {
		return ref, pluginapi.Invalid("archive sha256 required")
	}
	m, temp, err := c.extract(ctx, archivePath, expectedSHA256)
	if err != nil {
		return ref, err
	}
	defer os.RemoveAll(temp)
	ref = m.Descriptor.Ref
	created := false
	err = c.locked(true, func(rs map[string]Record) error {
		if r, ok := rs[ref.Key()]; ok {
			if r.ArchiveSHA256 != expectedSHA256 {
				return pluginapi.Fail("STATE_CONFLICT", "same version has a different package digest")
			}
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		dest := c.packageDir(ref)
		if _, err := os.Lstat(dest); err == nil {
			return pluginapi.Fail("STATE_CONFLICT", "unregistered package directory exists")
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return err
		}
		if err := os.Rename(temp, dest); err != nil {
			return err
		}
		created = true
		rs[ref.Key()] = Record{Ref: ref, Manifest: m, ArchiveSHA256: expectedSHA256}
		return nil
	}, func() {
		if created {
			_ = os.RemoveAll(c.packageDir(ref))
		}
	})
	return ref, err
}
