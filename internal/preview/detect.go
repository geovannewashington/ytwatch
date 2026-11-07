package preview

import (
	"bytes"
	"io"
	"os"
	"time"

	"github.com/charmbracelet/x/term"
	"golang.org/x/sys/unix"
)

const (
	kittyQuery   = "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"
	kittyOK      = "\x1b_Gi=31;OK\x1b\\"
	queryTimeout = 300 * time.Millisecond
)

// QueryKitty asks the terminal on in and out whether it answers the kitty
// graphics protocol. Call it before Bubble Tea starts reading the input. The
// query is followed by a device attributes request, which every terminal
// answers: a graphics answer before it means support.
func QueryKitty(in, out *os.File, tmux bool) bool {
	if !term.IsTerminal(in.Fd()) || !term.IsTerminal(out.Fd()) {
		return false
	}

	state, err := term.MakeRaw(in.Fd())
	if err != nil {
		return false
	}
	defer term.Restore(in.Fd(), state)

	query := kittyQuery
	if tmux {
		query = "\x1bPtmux;" + string(bytes.ReplaceAll([]byte(kittyQuery), []byte("\x1b"), []byte("\x1b\x1b"))) + "\x1b\\"
	}
	if _, err := out.WriteString(query + "\x1b[c"); err != nil {
		return false
	}

	return parseKittyReply(pollReader{fd: int(in.Fd()), deadline: time.Now().Add(queryTimeout)})
}

// parseKittyReply reads until the device attributes reply or an error (such as
// the timeout) and reports whether a graphics OK came before it. It reads on
// after the OK so the device attributes reply does not reach the program.
func parseKittyReply(r io.Reader) bool {
	var buf []byte
	chunk := make([]byte, 256)

	for {
		n, err := r.Read(chunk)
		buf = append(buf, chunk[:n]...)

		ok := bytes.Index(buf, []byte(kittyOK))
		if start := bytes.Index(buf, []byte("\x1b[?")); start >= 0 {
			if end := bytes.IndexByte(buf[start:], 'c'); end >= 0 {
				return ok >= 0 && ok < start
			}
		}
		if err != nil {
			return ok >= 0
		}
	}
}

// pollReader reads a file descriptor until a deadline, without ever blocking
// past it.
type pollReader struct {
	fd       int
	deadline time.Time
}

func (p pollReader) Read(b []byte) (int, error) {
	for {
		left := time.Until(p.deadline)
		if left <= 0 {
			return 0, os.ErrDeadlineExceeded
		}

		fds := []unix.PollFd{{Fd: int32(p.fd), Events: unix.POLLIN}}
		switch n, err := unix.Poll(fds, int(left.Milliseconds())+1); {
		case err == unix.EINTR:
			continue
		case err != nil:
			return 0, err
		case n == 0:
			return 0, os.ErrDeadlineExceeded
		}

		n, err := unix.Read(p.fd, b)
		if err == unix.EINTR || err == unix.EAGAIN {
			continue // a signal or a spurious wakeup, not the end of the reply
		}
		if err == nil && n == 0 {
			return 0, io.EOF
		}

		return max(n, 0), err
	}
}
