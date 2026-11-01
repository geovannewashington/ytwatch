package youtube

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geovannewashington/ytwatch/internal/locale"
)

func TestBrowseListsPlaylistVideosInOrderAndSkipsRemovedOnes(t *testing.T) {
	binDir := t.TempDir()
	fake := `#!/bin/sh
args="$*"
case "$args" in
  *--dump-single-json*) exit 1 ;;
  *playlist?list=PLabc*)
    printf '%s\n' '{"id":"first000001","title":"First","duration":60}'
    printf '%s\n' '{"id":"gone0000001","title":"[Private video]"}'
    printf '%s\n' '{"id":"second00001","title":"Second","duration":90}'
    printf '%s\n' '{"id":"third000001","title":"[Deleted video]"}'
    printf '%s\n' '{"id":"fourth00001","title":"Fourth","duration":30}'
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "yt-dlp"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var mu sync.Mutex
	type row struct {
		id   string
		rank int
	}
	var rows []row
	var done []Event
	Browse(ctx, "https://www.youtube.com/playlist?list=PLabc", locale.Locale{}, func(e Event) {
		mu.Lock()
		defer mu.Unlock()
		switch e.Kind {
		case EventResult:
			rows = append(rows, row{e.Video.ID, e.Rank})
		case EventSearchDone, EventDetailsEnd:
			done = append(done, e)
		}
	})
	var ids []string
	for i, r := range rows {
		ids = append(ids, r.id)
		if i > 0 && r.rank <= rows[i-1].rank {
			t.Errorf("ranks must follow the playlist order: %v", rows)
		}
	}
	if got := strings.Join(ids, ","); got != "first000001,second00001,fourth00001" {
		t.Fatalf("videos = %s", got)
	}
	if len(done) != 2 ||
		done[0].Kind != EventSearchDone ||
		done[0].Err != nil ||
		done[0].Count != 3 ||
		done[1].Kind != EventDetailsEnd {
		t.Fatalf("end events = %+v", done)
	}
}

func TestBrowseReportsAnEmptyPlaylist(t *testing.T) {
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "yt-dlp"), []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var got Event
	Browse(context.Background(), "https://www.youtube.com/playlist?list=PLabc", locale.Locale{}, func(e Event) {
		if e.Kind == EventSearchDone {
			got = e
		}
	})
	if got.Err == nil || !strings.Contains(got.Err.Error(), "no videos") {
		t.Fatalf("event = %+v", got)
	}
}
