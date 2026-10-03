//go:build !windows

package netcap

// NewBackend is only implemented on Windows (Npcap). Other platforms can still
// use the replay mode for development and tests.
func NewBackend() (Backend, error) { return nil, ErrNoBackend }
