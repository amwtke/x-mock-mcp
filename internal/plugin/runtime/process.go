package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"syscall"
	"time"
	"xmock.local/x-mock-mcp/internal/binding"
	"xmock.local/x-mock-mcp/internal/plugin/catalog"
	"xmock.local/x-mock-mcp/internal/plugin/ipc"
	"xmock.local/x-mock-mcp/pluginapi"
)

type Factory struct{ catalog *catalog.Catalog }

func New(c *catalog.Catalog) *Factory { return &Factory{catalog: c} }

type Process struct {
	peer *ipc.Peer
	cmd  *exec.Cmd
	done chan struct{}
	once sync.Once
	desc pluginapi.Descriptor
	log  *tailLog
}
type tailLog struct {
	mu sync.Mutex
	b  []byte
}

func (t *tailLog) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > 16384 {
		t.b = append([]byte(nil), t.b[len(t.b)-16384:]...)
	}
	return len(p), nil
}
func (p *Process) Done() <-chan struct{} { return p.peer.Done() }
func (p *Process) PID() int              { return p.cmd.Process.Pid }
func (p *Process) Close() error {
	p.once.Do(func() {
		p.peer.Close()
		select {
		case <-p.done:
		case <-time.After(500 * time.Millisecond):
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
			<-p.done
		}
	})
	return nil
}
func launch(ctx context.Context, path string, m pluginapi.Manifest, release func(), handler ipc.Handler) (*Process, error) {
	var once sync.Once
	doneRelease := func() { once.Do(release) }
	cmd := exec.Command(path)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		doneRelease()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		doneRelease()
		return nil, err
	}
	p := &Process{cmd: cmd, done: make(chan struct{}), log: &tailLog{}}
	cmd.Stderr = p.log
	if err = cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		doneRelease()
		return nil, err
	}
	p.peer = ipc.NewPeer(stdout, stdin, handler)
	go func() { _ = cmd.Wait(); p.peer.Close(); doneRelease(); close(p.done) }()
	startup, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var desc pluginapi.Descriptor
	if err = p.peer.Call(startup, "describe", struct{}{}, &desc); err != nil {
		p.Close()
		return nil, pluginapi.Fail("PLUGIN_EXITED", "plugin describe failed: "+err.Error())
	}
	if err = desc.Validate(); err != nil {
		p.Close()
		return nil, err
	}
	if desc.Ref != m.Descriptor.Ref || desc.APIMajor != m.Descriptor.APIMajor || !reflect.DeepEqual(desc.Contracts, m.Descriptor.Contracts) || !reflect.DeepEqual(desc.Features, m.Descriptor.Features) {
		p.Close()
		return nil, pluginapi.Fail("PLUGIN_API_MISMATCH", "runtime descriptor differs from installed manifest")
	}
	p.desc = desc
	return p, nil
}
func (f *Factory) open(ctx context.Context, ref pluginapi.Reference, handler ipc.Handler) (*Process, error) {
	release, err := f.catalog.Acquire(ref)
	if err != nil {
		return nil, err
	}
	m, dir, err := f.catalog.Package(ref)
	if err != nil {
		release()
		return nil, err
	}
	for name, digest := range m.Files {
		file, err := os.Open(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			release()
			return nil, err
		}
		h := sha256.New()
		_, err = io.Copy(h, file)
		file.Close()
		if err != nil {
			release()
			return nil, err
		}
		if hex.EncodeToString(h.Sum(nil)) != digest {
			release()
			return nil, pluginapi.Fail("STATE_CONFLICT", "installed plugin file digest changed")
		}
	}
	return launch(ctx, filepath.Join(dir, filepath.FromSlash(m.Entrypoint)), m, release, handler)
}
func (f *Factory) NewLeft(ctx context.Context, ref pluginapi.Reference) (binding.ManagedLeft, error) {
	if ref.Role != pluginapi.Left {
		return nil, pluginapi.Invalid("expected left role")
	}
	l := &LeftProxy{}
	p, err := f.open(ctx, ref, func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
		if method != "dispatch" {
			return nil, pluginapi.Fail("UNSUPPORTED", "unknown left callback")
		}
		var req pluginapi.Request
		if err := pluginapi.Decode(raw, &req); err != nil {
			return nil, err
		}
		l.mu.Lock()
		dispatch := l.dispatch
		l.mu.Unlock()
		if dispatch == nil {
			return nil, pluginapi.Fail("STATE_CONFLICT", "left has not started")
		}
		return dispatch(ctx, req)
	})
	if err != nil {
		return nil, err
	}
	l.Process = p
	return l, nil
}
func (f *Factory) NewRight(ctx context.Context, ref pluginapi.Reference) (binding.ManagedRight, error) {
	if ref.Role != pluginapi.Right {
		return nil, pluginapi.Invalid("expected right role")
	}
	p, err := f.open(ctx, ref, nil)
	if err != nil {
		return nil, err
	}
	return &RightProxy{Process: p}, nil
}
func (f *Factory) Prepare(ctx context.Context, ref pluginapi.Reference, spec pluginapi.PreparationSpec) (pluginapi.PreparationReport, error) {
	var report pluginapi.PreparationReport
	right, err := f.NewRight(ctx, ref)
	if err != nil {
		return report, err
	}
	defer right.Close()
	return right.(*RightProxy).Prepare(ctx, spec)
}
