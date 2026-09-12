package app

import (
	"os"
	"path/filepath"
	"syscall"
	"xmock.local/x-mock-mcp/pluginapi"
)

type DaemonLease struct{ file *os.File }

func AcquireDaemon(root string) (*DaemonLease, error) {
	dir := filepath.Join(root, ".x-mock")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(dir, "daemon.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, pluginapi.Fail("DAEMON_EXISTS", "another daemon holds this project lock")
	}
	return &DaemonLease{file: file}, nil
}
func (l *DaemonLease) Close() error {
	syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	return l.file.Close()
}
