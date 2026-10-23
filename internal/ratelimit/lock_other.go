//go:build !unix

package ratelimit

// lockFile is a no-op on platforms without flock; the in-process mutex in
// Limiter still serializes callers within one process.
func lockFile(path string) (func(), error) { return func() {}, nil }
