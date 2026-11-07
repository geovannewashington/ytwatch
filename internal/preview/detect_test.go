package preview

import (
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

// timeout stands for the poll deadline: the data, then an error.
func timeout(data string) io.Reader {
	return io.MultiReader(strings.NewReader(data), iotest.ErrReader(errors.New("timeout")))
}

func TestParseKittyReply(t *testing.T) {
	for name, tc := range map[string]struct {
		reader io.Reader
		want   bool
	}{
		"ok then DA1":        {strings.NewReader("\x1b_Gi=31;OK\x1b\\\x1b[?62;4c"), true},
		"DA1 only":           {strings.NewReader("\x1b[?62;4c"), false},
		"error then DA1":     {strings.NewReader("\x1b_Gi=31;ENOTSUPPORTED:no\x1b\\\x1b[?62c"), false},
		"DA1 then ok":        {strings.NewReader("\x1b[?62c\x1b_Gi=31;OK\x1b\\"), false},
		"timeout":            {timeout(""), false},
		"ok then timeout":    {timeout("\x1b_Gi=31;OK\x1b\\"), true},
		"noise before reply": {strings.NewReader("xx\x1b[Ayy\x1b_Gi=31;OK\x1b\\\x1b[?1;2c"), true},
		"split in pieces":    {iotest.OneByteReader(strings.NewReader("\x1b_Gi=31;OK\x1b\\\x1b[?6c")), true},
		"other image id":     {strings.NewReader("\x1b_Gi=7;OK\x1b\\\x1b[?6c"), false},
		"closed input":       {strings.NewReader(""), false},
	} {
		if got := parseKittyReply(tc.reader); got != tc.want {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}

func TestParseKittyReplyReadsTheDA1ReplyToo(t *testing.T) {
	r := strings.NewReader("\x1b_Gi=31;OK\x1b\\\x1b[?62;4c")
	parseKittyReply(r)
	if r.Len() != 0 {
		t.Errorf("%d bytes of the reply were left for the program", r.Len())
	}
}
