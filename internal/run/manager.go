package run

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"
	"xmock.local/x-mock-mcp/internal/scenario"
	"xmock.local/x-mock-mcp/pluginapi"
)

type JobSpec struct {
	Name             string            `json:"name" yaml:"name"`
	Argv             []string          `json:"argv" yaml:"argv"`
	WorkingDirectory string            `json:"working_directory" yaml:"working_directory"`
	Env              map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	TimeoutMS        int64             `json:"timeout_ms" yaml:"timeout_ms"`
}
type Status struct {
	ID            string             `json:"run_id"`
	EnvironmentID string             `json:"environment_id"`
	Job           string             `json:"job"`
	State         string             `json:"state"`
	PID           int                `json:"pid"`
	ExitCode      int                `json:"exit_code"`
	StartedAt     string             `json:"started_at"`
	FinishedAt    string             `json:"finished_at,omitempty"`
	LogPath       string             `json:"log_path"`
	Error         *pluginapi.Failure `json:"error,omitempty"`
}
type execution struct {
	status         Status
	done           chan struct{}
	cancel         context.CancelFunc
	stopped        bool
	outputExceeded bool
	aborted        error
}
type Manager struct {
	mu        sync.Mutex
	root, dir string
	runs      map[string]*execution
}
type Finish func(context.Context, Status) error

func New(root string) (*Manager, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, ".x-mock", "runs")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Manager{root: root, dir: dir, runs: map[string]*execution{}}, nil
}
func (m *Manager) Start(id, env string, job JobSpec, extra map[string]string, finish Finish) (out Status, err error) {
	if !scenario.ValidID(id) || env == "" || len(job.Argv) == 0 || job.Name == "" || job.TimeoutMS < 1 || job.TimeoutMS > 3600000 {
		return out, pluginapi.Invalid("invalid named job or timeout")
	}
	relative := job.WorkingDirectory
	if relative == "" {
		relative = "."
	}
	if relative != "." && !pluginapi.SafeRelative(relative) {
		return out, pluginapi.Invalid("job working directory must be inside project")
	}
	dir, err := filepath.EvalSymlinks(filepath.Join(m.root, relative))
	if err != nil {
		return out, err
	}
	rel, err := filepath.Rel(m.root, dir)
	if err != nil || (rel != "." && !pluginapi.SafeRelative(rel)) {
		return out, pluginapi.Invalid("job directory escapes project")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(job.TimeoutMS)*time.Millisecond)
	logPath := filepath.Join(m.dir, id+".log")
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		cancel()
		return out, err
	}
	cmd := exec.CommandContext(ctx, job.Argv[0], job.Argv[1:]...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 2 * time.Second
	envs := map[string]string{}
	for _, key := range []string{"PATH", "HOME", "JAVA_HOME", "LANG", "LC_ALL", "TMPDIR", "PLAYWRIGHT_BROWSERS_PATH"} {
		if value, ok := os.LookupEnv(key); ok {
			envs[key] = value
		}
	}
	for k, v := range job.Env {
		envs[k] = v
	}
	for k, v := range extra {
		envs[k] = v
	}
	for k, v := range envs {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	e := &execution{done: make(chan struct{}), cancel: cancel}
	bounded := &limitedWriter{file: file, remaining: 4 << 20, onExceeded: func() { m.mu.Lock(); e.outputExceeded = true; m.mu.Unlock(); cancel() }}
	cmd.Stdout = bounded
	cmd.Stderr = bounded
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		go func() {
			select {
			case <-e.done:
			case <-time.After(time.Second):
				syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		}()
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	m.mu.Lock()
	if _, ok := m.runs[id]; ok {
		m.mu.Unlock()
		cancel()
		file.Close()
		os.Remove(logPath)
		return out, pluginapi.Fail("STATE_CONFLICT", "run ID already exists")
	}
	if err = cmd.Start(); err != nil {
		m.mu.Unlock()
		cancel()
		file.Close()
		os.Remove(logPath)
		return out, err
	}
	out = Status{ID: id, EnvironmentID: env, Job: job.Name, State: "running", PID: cmd.Process.Pid, ExitCode: -1, StartedAt: time.Now().UTC().Format(time.RFC3339Nano), LogPath: logPath}
	e.status = out
	m.runs[id] = e
	m.mu.Unlock()
	go func() {
		waitErr := cmd.Wait()
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		file.Close()
		m.mu.Lock()
		status := e.status
		status.ExitCode = cmd.ProcessState.ExitCode()
		status.State = "succeeded"
		if waitErr != nil {
			status.State = "failed"
			status.Error = &pluginapi.Failure{Code: "JOB_FAILED", Message: waitErr.Error()}
		}
		if e.stopped {
			status.State = "cancelled"
			status.Error = &pluginapi.Failure{Code: "CANCELLED", Message: "run stopped"}
		} else if ctx.Err() == context.DeadlineExceeded {
			status.State = "timed_out"
			status.Error = &pluginapi.Failure{Code: "DEADLINE_EXCEEDED", Message: "job timed out"}
		}
		if e.outputExceeded {
			status.State = "failed"
			status.Error = &pluginapi.Failure{Code: "OUTPUT_FULL", Message: "job output exceeded 4 MiB"}
		}
		if e.aborted != nil {
			status.State = "failed"
			status.Error = &pluginapi.Failure{Code: pluginapi.Code(e.aborted), Message: e.aborted.Error()}
		}
		m.mu.Unlock()
		if finish != nil {
			checkCtx, checkCancel := context.WithTimeout(context.Background(), 5*time.Second)
			checkErr := finish(checkCtx, status)
			checkCancel()
			if checkErr != nil && status.State == "succeeded" {
				status.State = "failed"
				status.Error = &pluginapi.Failure{Code: pluginapi.Code(checkErr), Message: checkErr.Error()}
			}
		}
		cancel()
		status.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
		m.mu.Lock()
		e.status = status
		raw, _ := json.MarshalIndent(status, "", "  ")
		os.WriteFile(filepath.Join(m.dir, id+".json"), raw, 0600)
		close(e.done)
		m.mu.Unlock()
	}()
	return out, nil
}
func (m *Manager) Status(id string) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.runs[id]; e != nil {
		return e.status, nil
	}
	return Status{}, pluginapi.Fail("NOT_FOUND", "run does not exist in this daemon instance")
}
func (m *Manager) Wait(ctx context.Context, id string) error {
	m.mu.Lock()
	e := m.runs[id]
	m.mu.Unlock()
	if e == nil {
		return pluginapi.Fail("NOT_FOUND", "run missing")
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-e.done:
		return nil
	}
}
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.runs[id]
	if e == nil {
		return pluginapi.Fail("NOT_FOUND", "run missing")
	}
	select {
	case <-e.done:
		return nil
	default:
	}
	e.stopped = true
	e.cancel()
	return nil
}
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	ids := []string{}
	for id := range m.runs {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Stop(id)
	}
	for _, id := range ids {
		if err := m.Wait(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

type limitedWriter struct {
	mu         sync.Mutex
	file       io.Writer
	remaining  int
	onExceeded func()
	exceeded   bool
}

func (w *limitedWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	original := len(data)
	if len(data) > w.remaining {
		data = data[:w.remaining]
		if !w.exceeded {
			w.exceeded = true
			w.onExceeded()
		}
	}
	n, err := w.file.Write(data)
	w.remaining -= n
	if err != nil {
		return n, err
	}
	if w.exceeded {
		return original, fmt.Errorf("job output limit exceeded")
	}
	return n, nil
}

func (m *Manager) Abort(id string, err error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.runs[id]
	if e == nil {
		return pluginapi.Fail("NOT_FOUND", "run missing")
	}
	select {
	case <-e.done:
		return nil
	default:
	}
	e.aborted = err
	e.cancel()
	return nil
}
