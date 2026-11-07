package preview

import (
	"os"
	"sync"
)

// SyncWriter is a terminal file whose writes never interleave. The program
// writes its frames to it and Kitty writes its images, from other goroutines.
// It has the methods of a term.File, which Bubble Tea needs to size and set up
// the terminal.
type SyncWriter struct {
	f  *os.File
	mu sync.Mutex
}

func NewSyncWriter(f *os.File) *SyncWriter { return &SyncWriter{f: f} }

func (s *SyncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.f.Write(p)
}

func (s *SyncWriter) Read(p []byte) (int, error) { return s.f.Read(p) }

// Close does nothing: the terminal outlives the program.
func (s *SyncWriter) Close() error { return nil }

func (s *SyncWriter) Fd() uintptr { return s.f.Fd() }
