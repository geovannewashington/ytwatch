package youtube

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geovannewashington/ytwatch/internal/locale"
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
    printf '%s\n' '{"id":"live12345","title":"Live coding right now","webpage_url":"https://www.youtube.com/watch?v=live12345","live_status":"is_live"}'
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
	Search(ctx, "Go interfaces", Filters{}, locale.Locale{}, func(event Event) {
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

func TestPlaylistSearchURL(t *testing.T) {
	got := playlistSearchURL("go tutorial & more")
	want := "https://www.youtube.com/results?search_query=go+tutorial+%26+more&sp=EgIQAw%253D%253D"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// The main query keeps YouTube's order, a playlist comes after a few results,
// and the live query adds only streams that are on air, after the rest.
func TestSearchRanksResultsAndKeepsOnlyLiveFromTheLiveQuery(t *testing.T) {
	binDir := t.TempDir()
	fake := `#!/bin/sh
args="$*"
case "$args" in
  *--dump-single-json*) exit 1 ;;
  *'ytsearch20:topic live'*)
    printf '%s\n' '{"id":"junk000001","title":"Unrelated video"}'
    printf '%s\n' '{"id":"stream00001","title":"A stream","live_status":"is_live"}'
    ;;
  *'ytsearch35:topic'*)
    printf '%s\n' '{"id":"first000001","title":"First"}'
    printf '%s\n' '{"id":"second00001","title":"Second"}'
    printf '%s\n' '{"id":"third000001","title":"Third"}'
    ;;
  *results?search_query=*)
    printf '%s\n' '{"id":"PLplaylist000000000000000000000001","title":"A playlist","_type":"url","url":"https://www.youtube.com/playlist?list=PLplaylist000000000000000000000001"}'
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
	rank := map[string]int{}
	Search(ctx, "topic", Filters{}, locale.Locale{}, func(event Event) {
		if event.Kind == EventResult {
			mu.Lock()
			rank[event.Video.ID] = event.Rank
			mu.Unlock()
		}
	})
	if _, found := rank["junk000001"]; found {
		t.Error("a video from the live query that is not live must be dropped")
	}
	order := []string{"first000001", "second00001", "third000001", "PLplaylist000000000000000000000001", "stream00001"}
	for i := 1; i < len(order); i++ {
		a, aok := rank[order[i-1]]
		b, bok := rank[order[i]]
		if !aok || !bok || a >= b {
			t.Fatalf("want %s (rank %d) above %s (rank %d); all ranks: %v", order[i-1], a, order[i], b, rank)
		}
	}
}

func TestSearchPassesTheLocaleToYtDlpAndTheShortsPage(t *testing.T) {
	binDir := t.TempDir()
	log := filepath.Join(binDir, "args.log")
	script := "#!/bin/sh\necho \"$*\" >> " + log + "\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "yt-dlp"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	old := fetchResultsPage
	defer func() { fetchResultsPage = old }()
	var pageURL, accept string
	fetchResultsPage = func(_ context.Context, u string, loc locale.Locale) ([]byte, error) {
		pageURL, accept = u, loc.AcceptLanguage()
		return nil, errors.New("offline")
	}

	Search(context.Background(), "topic", Filters{}, locale.Locale{Language: "pt-BR", Region: "BR"}, func(Event) {})

	data, _ := os.ReadFile(log)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected the three searches to run yt-dlp, got %q", lines)
	}
	for _, line := range lines {
		if !strings.Contains(line, "--extractor-args youtube:lang=pt-BR") || !strings.Contains(line, "--xff BR") {
			t.Errorf("yt-dlp was run without the locale: %s", line)
		}
	}
	if !strings.Contains(pageURL, "hl=pt-BR&gl=BR") || accept != "pt-BR,en;q=0.5" {
		t.Errorf("the Shorts page was asked for with %q and Accept-Language %q", pageURL, accept)
	}
}
