package player

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeMpv listens on a socket and answers each request like mpv: the events
// first, then the value of the property that was asked. A property that is not
// in values answers "property unavailable".
func fakeMpv(t *testing.T, events []string, values map[string]string) string {
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

			go func() {
				defer conn.Close()
				for _, event := range events {
					fmt.Fprintln(conn, event)
				}

				scanner := bufio.NewScanner(conn)
				for scanner.Scan() {
					var request struct {
						Command   []string `json:"command"`
						RequestID int      `json:"request_id"`
					}
					if json.Unmarshal(scanner.Bytes(), &request) != nil || len(request.Command) < 2 {
						continue
					}

					if value, ok := values[request.Command[1]]; ok {
						fmt.Fprintf(conn, `{"data":%s,"request_id":%d,"error":"success"}`+"\n", value, request.RequestID)
					} else {
						fmt.Fprintf(conn, `{"request_id":%d,"error":"property unavailable"}`+"\n", request.RequestID)
					}
				}
			}()
		}
	}()

	return socket
}

func TestPlaybackStateReadsTimePauseAndSpeed(t *testing.T) {
	socket := fakeMpv(t,
		[]string{`{"event":"playback-restart"}`, `{"event":"property-change","name":"pause","data":false}`},
		map[string]string{"time-pos": "125.5", "pause": "false", "core-idle": "false", "speed": "1.5"},
	)

	got, err := PlaybackState(socket)
	if err != nil || got != (State{Time: 125500 * time.Millisecond, Paused: false, Speed: 1.5}) {
		t.Fatalf("PlaybackState = %+v, %v", got, err)
	}
	if time, err := PlaybackTime(socket); err != nil || time != 125500*1000000 {
		t.Errorf("PlaybackTime = %v, %v", time, err)
	}
}

func TestPlaybackStateSeesAPauseAndABufferingMpv(t *testing.T) {
	for name, values := range map[string]map[string]string{
		"paused":    {"time-pos": "10", "pause": "true", "core-idle": "true", "speed": "1"},
		"buffering": {"time-pos": "10", "pause": "false", "core-idle": "true", "speed": "1"},
	} {
		got, err := PlaybackState(fakeMpv(t, nil, values))
		if err != nil || !got.Paused || got.Time != 10*time.Second {
			t.Errorf("%s: %+v, %v", name, got, err)
		}
	}
}

func TestPlaybackStateKeepsDefaultsForAPropertyMpvDoesNotAnswer(t *testing.T) {
	got, err := PlaybackState(fakeMpv(t, nil, map[string]string{"time-pos": "3"}))
	if err != nil || got.Paused || got.Speed != 1 || got.Time != 3*time.Second {
		t.Errorf("only the time is needed: %+v, %v", got, err)
	}

	for _, speed := range []string{"0", "-2", "1000", "null"} {
		got, _ := PlaybackState(fakeMpv(t, nil, map[string]string{"time-pos": "3", "speed": speed}))
		if got.Speed != 1 {
			t.Errorf("speed %s must fall back to 1, got %v", speed, got.Speed)
		}
	}
}

func TestPlaybackTimeReportsAPositionThatIsNotThereYet(t *testing.T) {
	socket := fakeMpv(t, nil, map[string]string{"pause": "false"}) // no time-pos yet

	if _, err := PlaybackTime(socket); !errors.Is(err, ErrNoPlaybackTime) {
		t.Fatalf("err = %v, want ErrNoPlaybackTime", err)
	}
}

func TestPlaybackTimeFailsWhenMpvIsGone(t *testing.T) {
	if _, err := PlaybackTime(filepath.Join(t.TempDir(), "nothing.sock")); err == nil {
		t.Fatal("a missing socket must be an error")
	}
}

func TestPlaybackStateIgnoresRepliesToOtherRequests(t *testing.T) {
	socket := fakeMpv(t,
		[]string{`{"data":999,"request_id":7,"error":"success"}`, `not json`, `{"data":999,"request_id":0,"error":"success"}`},
		map[string]string{"time-pos": "3"},
	)

	got, err := PlaybackState(socket)
	if err != nil || got.Time != 3*time.Second {
		t.Fatalf("PlaybackState = %+v, %v", got, err)
	}
}
