//go:build unix

package ratelimit

import (
	"os"
	"syscall"
)

// lockFile takes (blocking; holders keep it only for a read-modify-write, and
// the kernel drops it on crash - but a stuck holder on NFS would hang callers)
// takes an exclusive advisory flock on path and returns the unlock func.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
