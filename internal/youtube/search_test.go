package youtube

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geovannewashington/ytwatch/internal/media"
)

func TestSearchIncludesLiveOrientedResults(t *testing.T) {
	binDir := t.TempDir()
	fake := `#!/bin/sh
args="$*"
if echo "$args" | grep -q -- '--dump-single-json'; then
  case "$args" in
    *v=live12345*) printf '%s\n' '{"id":"live12345","title":"Live coding right now","webpage_url":"https://www.youtube.com/watch?v=live12345","live_status":"is_live","is_live":true,"concurrent_view_count":42,"description":"Current live stream"}' ;;
    *v=video12345*) printf '%s\n' '{"id":"video12345","title":"Go interfaces recorded","webpage_url":"https://www.youtube.com/watch?v=video12345","live_status":"was_live","was_live":true,"duration":900,"description":"Recorded stream"}' ;;
  esac
  exit 0
fi
case "$args" in
  *'ytsearch20:Go interfaces live'*)
    printf '%s\n' '{"id":"live12345","title":"Live coding right now","webpage_url":"https://www.youtube.com/watch?v=live12345"}'
    printf '%s\n' '{"id":"video12345","title":"Go interfaces recorded","webpage_url":"https://www.youtube.com/watch?v=video12345"}'
    ;;
  *'ytsearch35:Go interfaces'*)
    printf '%s\n' '{"id":"video12345","title":"Go interfaces recorded","webpage_url":"https://www.youtube.com/watch?v=video12345"}'
    ;;
esac
`
	fakePath := filepath.Join(binDir, "yt-dlp")
	if err := os.WriteFile(fakePath, []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var mu sync.Mutex
	var events []Event
	Search(ctx, "Go interfaces", func(event Event) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	})

	mu.Lock()
	defer mu.Unlock()
	results := make(map[string]media.Video)
	for _, event := range events {
		if event.Kind == EventResult || event.Kind == EventDetails {
			results[event.Video.ID] = event.Video
		}
	}
	if len(results) != 2 {
		t.Fatalf("got %d unique results, want two: %#v", len(results), results)
	}
	live, ok := results["live12345"]
	if !ok {
		t.Fatal("live-oriented query did not return the current livestream")
	}
	if !live.IsCurrentlyLive() {
		t.Fatalf("live result wasn't classified as live: status=%q is_live=%v", live.LiveStatus, live.IsLive)
	}
	if !strings.Contains(live.Title, "Live") {
		t.Fatalf("unexpected live result title: %q", live.Title)
	}
}
