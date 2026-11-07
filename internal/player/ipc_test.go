package player

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeMpv listens on a socket and answers each connection with the lines.
func fakeMpv(t *testing.T, lines ...string) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "ytwatch-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	socket := filepath.Join(dir, socketName)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}

			buf := make([]byte, 256)
			_, _ = conn.Read(buf) // the request
			for _, line := range lines {
				_, _ = conn.Write([]byte(line + "\n"))
			}
			conn.Close()
		}
	}()

	return socket
}

func TestPlaybackTimeSkipsEventsAndReadsTheReply(t *testing.T) {
	socket := fakeMpv(t,
		`{"event":"playback-restart"}`,
		`{"event":"property-change","name":"pause","data":false}`,
		`{"data":125.5,"request_id":1,"error":"success"}`,
	)

	got, err := PlaybackTime(socket)
	if err != nil || got != 125500*time.Millisecond {
		t.Fatalf("PlaybackTime = %v, %v", got, err)
	}
}

func TestPlaybackTimeReportsAPositionThatIsNotThereYet(t *testing.T) {
	socket := fakeMpv(t, `{"request_id":1,"error":"property unavailable"}`)

	if _, err := PlaybackTime(socket); !errors.Is(err, ErrNoPlaybackTime) {
		t.Fatalf("err = %v, want ErrNoPlaybackTime", err)
	}
}

func TestPlaybackTimeFailsWhenMpvIsGone(t *testing.T) {
	if _, err := PlaybackTime(filepath.Join(t.TempDir(), "nothing.sock")); err == nil {
		t.Fatal("a missing socket must be an error")
	}
}

func TestPlaybackTimeIgnoresRepliesToOtherRequests(t *testing.T) {
	socket := fakeMpv(t,
		`{"data":999,"request_id":7,"error":"success"}`,
		`not json`,
		`{"data":3,"request_id":1,"error":"success"}`,
	)

	got, err := PlaybackTime(socket)
	if err != nil || got != 3*time.Second {
		t.Fatalf("PlaybackTime = %v, %v", got, err)
	}
}
