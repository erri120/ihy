//go:build !linux

// Non-Linux fallback: no fadvise, warming is a no-op there.
package stream

func fadviseWarm(absPath string) {}
