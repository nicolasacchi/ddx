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
	// O_CREATE's 0600 only applies to new files: tighten an existing one we own.
	if fi, serr := f.Stat(); serr == nil && ownedByUs(fi) && fi.Mode().Perm() != 0o600 {
		_ = f.Chmod(0o600)
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

// ownedByUs reports whether fi belongs to the current user.
func ownedByUs(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return !ok || int(st.Uid) == os.Getuid()
}
