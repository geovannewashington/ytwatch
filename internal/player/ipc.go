package player

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"
)

// socketName is the name of mpv's control socket inside its private directory.
const socketName = "mpv.sock"

const (
	ipcTimeout = 1500 * time.Millisecond

	// mpv sends events on the same socket as the replies, so a few lines can
	// come before the replies. maxIPCLine and maxIPCLines bound what is read.
	maxIPCLine  = 64 << 10
	maxIPCLines = 100
)

// ErrNoPlaybackTime is returned while mpv has no position yet, for example
// while it is still opening the stream.
var ErrNoPlaybackTime = errors.New("mpv has no playback time yet")

// State is where mpv is in the video, and whether that moves.
type State struct {
	Time time.Duration
	// Paused is true when the time does not move: the user paused, or mpv
	// waits for data.
	Paused bool
	// Speed is how many seconds of video play in a second.
	Speed float64
}

// ipcProperties are the properties of State, in the order they are asked.
var ipcProperties = []string{"time-pos", "pause", "core-idle", "speed"}

// PlaybackTime asks the mpv that listens on socket where it is in the video.
func PlaybackTime(socket string) (time.Duration, error) {
	state, err := PlaybackState(socket)

	return state.Time, err
}

// PlaybackState asks the mpv that listens on socket where it is in the video,
// and whether it is paused. The properties are asked together, on one
// connection, so that they describe the same moment.
func PlaybackState(socket string) (State, error) {
	conn, err := net.DialTimeout("unix", socket, ipcTimeout)
	if err != nil {
		return State{}, err
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(ipcTimeout))

	var request []byte
	for i, property := range ipcProperties {
		request = fmt.Appendf(request, `{"command":["get_property",%q],"request_id":%d}`+"\n", property, i+1)
	}
	if _, err := conn.Write(request); err != nil {
		return State{}, err
	}

	type reply struct {
		Error     string          `json:"error"`
		Data      json.RawMessage `json:"data"`
		RequestID *int            `json:"request_id"`
	}
	replies := map[int]reply{}

	reader := bufio.NewReaderSize(conn, maxIPCLine)
	for range maxIPCLines {
		if len(replies) == len(ipcProperties) {
			break
		}

		line, err := reader.ReadSlice('\n')
		if err != nil {
			return State{}, err
		}

		var r reply
		if json.Unmarshal(line, &r) != nil || r.RequestID == nil || *r.RequestID < 1 || *r.RequestID > len(ipcProperties) {
			continue // an event, not a reply
		}
		replies[*r.RequestID] = r
	}
	if len(replies) != len(ipcProperties) {
		return State{}, errors.New("no reply from mpv")
	}

	var seconds float64
	if r := replies[1]; r.Error != "success" || json.Unmarshal(r.Data, &seconds) != nil {
		return State{}, ErrNoPlaybackTime
	}

	state := State{Time: time.Duration(seconds * float64(time.Second)), Speed: 1}

	// A property that mpv does not answer keeps its default.
	var paused, idle bool
	if r := replies[2]; r.Error == "success" {
		_ = json.Unmarshal(r.Data, &paused)
	}
	if r := replies[3]; r.Error == "success" {
		_ = json.Unmarshal(r.Data, &idle)
	}
	state.Paused = paused || idle

	var speed float64
	if r := replies[4]; r.Error == "success" && json.Unmarshal(r.Data, &speed) == nil && speed > 0 && speed <= 100 {
		state.Speed = speed
	}

	return state, nil
}
