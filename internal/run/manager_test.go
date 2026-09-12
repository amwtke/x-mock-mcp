package run

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
	"xmock.local/x-mock-mcp/pluginapi"
)

func TestBackgroundJobAndFailures(t *testing.T) {
	root := t.TempDir()
	m, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name    string
		argv    []string
		timeout int64
		want    string
	}{{"ok", []string{"/bin/sh", "-c", "printf ready"}, 3000, "succeeded"}, {"exit", []string{"/bin/sh", "-c", "exit 7"}, 3000, "failed"}, {"timeout", []string{"/bin/sh", "-c", "sleep 30"}, 40, "timed_out"}, {"output", []string{"/bin/sh", "-c", "yes x | head -c 5000000"}, 3000, "failed"}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := time.Now()
			r, err := m.Start(c.name, "env", JobSpec{Name: c.name, Argv: c.argv, WorkingDirectory: ".", TimeoutMS: c.timeout}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if time.Since(before) > time.Second {
				t.Fatal("start blocked")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err = m.Wait(ctx, r.ID); err != nil {
				t.Fatal(err)
			}
			s, err := m.Status(r.ID)
			if err != nil || s.State != c.want {
				t.Fatal(s, err)
			}
		})
	}
}
func TestStopAndVerification(t *testing.T) {
	root := t.TempDir()
	m, _ := New(root)
	marker := filepath.Join(root, "survived")
	r, err := m.Start("stop", "env", JobSpec{Name: "stop", Argv: []string{"/bin/sh", "-c", "(sleep 1; touch survived) & wait"}, WorkingDirectory: ".", TimeoutMS: 5000}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.Stop(r.ID); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	m.Wait(ctx, r.ID)
	if _, err = os.Stat(marker); err == nil {
		t.Fatal("child survived")
	}
	r, err = m.Start("verify", "env", JobSpec{Name: "verify", Argv: []string{"/bin/sh", "-c", "exit 0"}, WorkingDirectory: ".", TimeoutMS: 3000}, nil, func(context.Context, Status) error {
		return pluginapi.Fail("EXPECTATION_FAILED", "missing application write")
	})
	if err != nil {
		t.Fatal(err)
	}
	m.Wait(ctx, r.ID)
	s, _ := m.Status(r.ID)
	if s.State != "failed" {
		t.Fatal("exit zero bypassed verification", s)
	}
}

func TestInfrastructureAbortIsFailedNotUserCancellation(t *testing.T) {
	m, _ := New(t.TempDir())
	r, err := m.Start("abort", "env", JobSpec{Name: "wait", Argv: []string{"/bin/sh", "-c", "sleep 30"}, WorkingDirectory: ".", TimeoutMS: 5000}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m.Abort(r.ID, pluginapi.Fail("PLUGIN_EXITED", "right plugin crashed"))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err = m.Wait(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	status, _ := m.Status(r.ID)
	if status.State != "failed" || status.Error.Code != "PLUGIN_EXITED" {
		t.Fatal(status)
	}
}
