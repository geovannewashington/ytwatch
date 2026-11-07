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
	// come before the reply. maxIPCLine and maxIPCLines bound what is read.
	maxIPCLine  = 64 << 10
	maxIPCLines = 50
)

// ErrNoPlaybackTime is returned by PlaybackTime while mpv has no position yet,
// for example while it is still opening the stream.
var ErrNoPlaybackTime = errors.New("mpv has no playback time yet")

// PlaybackTime asks the mpv that listens on socket where it is in the video.
func PlaybackTime(socket string) (time.Duration, error) {
	conn, err := net.DialTimeout("unix", socket, ipcTimeout)
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(ipcTimeout))

	const requestID = 1
	request := fmt.Sprintf(`{"command":["get_property","time-pos"],"request_id":%d}`+"\n", requestID)
	if _, err := conn.Write([]byte(request)); err != nil {
		return 0, err
	}

	reader := bufio.NewReaderSize(conn, maxIPCLine)
	for range maxIPCLines {
		line, err := reader.ReadSlice('\n')
		if err != nil {
			return 0, err
		}

		var reply struct {
			Error     string   `json:"error"`
			Data      *float64 `json:"data"`
			RequestID *int     `json:"request_id"`
		}
		if json.Unmarshal(line, &reply) != nil || reply.RequestID == nil || *reply.RequestID != requestID {
			continue // an event, not the reply
		}

		if reply.Error != "success" || reply.Data == nil {
			return 0, ErrNoPlaybackTime
		}

		return time.Duration(*reply.Data * float64(time.Second)), nil
	}

	return 0, errors.New("no reply from mpv")
}
