//go:build !unix

package ratelimit

import "os"

// lockFile is a no-op on platforms without flock; the in-process mutex in
// Limiter still serializes callers within one process.
func lockFile(path string) (func(), error) { return func() {}, nil }

// ownedByUs cannot be checked without unix uids; assume yes.
func ownedByUs(fi os.FileInfo) bool { return true }
