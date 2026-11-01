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
  # A batch holds several URLs, so every match is printed.
  case "$args" in *v=live12345*) printf '%s\n' '{"id":"live12345","title":"Live coding right now","webpage_url":"https://www.youtube.com/watch?v=live12345","live_status":"is_live","is_live":true,"concurrent_view_count":42,"description":"Current live stream"}' ;; esac
  case "$args" in *v=video12345*) printf '%s\n' '{"id":"video12345","title":"Go interfaces recorded","webpage_url":"https://www.youtube.com/watch?v=video12345","live_status":"was_live","was_live":true,"duration":900,"description":"Recorded stream"}' ;; esac
  exit 0
fi
case "$args" in
  *LIVEPAGE*)
    printf '%s\n' '{"id":"live12345","title":"Live coding right now","webpage_url":"https://www.youtube.com/watch?v=live12345","live_status":"is_live"}'
    printf '%s\n' '{"id":"video12345","title":"Go interfaces recorded","webpage_url":"https://www.youtube.com/watch?v=video12345"}'
    ;;
  *'ytsearch35:Go interfaces'*)
    printf '%s\n' '{"id":"video12345","title":"Go interfaces recorded","webpage_url":"https://www.youtube.com/watch?v=video12345"}'
    ;;
esac
`
	fakePath := filepath.Join(binDir, "yt-dlp")
	fake = strings.ReplaceAll(fake, "LIVEPAGE", "'"+resultsURL("Go interfaces", Filters{Type: TypeLive})+"'")
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
  *LIVEPAGE*)
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
	fake = strings.ReplaceAll(fake, "LIVEPAGE", "'"+resultsURL("topic", Filters{Type: TypeLive})+"'")
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

// Two results from different sources must never share a rank, or the order
// would depend on which query finishes first.
func TestRanksOfDifferentSourcesNeverTie(t *testing.T) {
	seen := map[int]string{}
	add := func(rank int, source string) {
		if other, found := seen[rank]; found {
			t.Errorf("rank %d is used by %s and by %s", rank, other, source)
		}
		seen[rank] = source
	}
	for p := 0; p < videoResultCount; p++ {
		add(mainRanking.at(p), "main")
	}
	for p := 0; p < playlistCount; p++ {
		add(playlistRanking.at(p), "playlist")
	}
	for p := 0; p < liveResultCount; p++ {
		add(liveRanking.at(p), "live")
	}
	// A search can have Shorts that match the query and Shorts that do not,
	// so both kinds count.
	matching, other := shortRanking("a"), shortRanking("zzz")
	for p := 0; p < shortResultCount; p++ {
		add(matching(p, "a b"), "matching Short")
	}
	for p := 0; p < shortResultCount; p++ {
		add(other(p, "unrelated"), "other Short")
	}
}

// Several videos share one yt-dlp run, and each result is matched to its video
// by id, whatever the order of the output.
func TestFetchDetailsBatchUsesOneRunForSeveralVideos(t *testing.T) {
	binDir := t.TempDir()
	log := filepath.Join(binDir, "runs.log")
	fake := "#!/bin/sh\necho run >> " + log + "\n" +
		`printf '%s\n' '{"id":"bbbbbbbbbbb","title":"B","duration":20}'` + "\n" +
		`printf '%s\n' '{"id":"aaaaaaaaaaa","title":"A","duration":10}'` + "\n"
	fake = strings.ReplaceAll(fake, "LIVEPAGE", "'"+resultsURL("topic", Filters{Type: TypeLive})+"'")
	if err := os.WriteFile(filepath.Join(binDir, "yt-dlp"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	videos := []media.Video{{ID: "aaaaaaaaaaa", Title: "a"}, {ID: "bbbbbbbbbbb", Title: "b"}}
	got := map[string]float64{}
	err := FetchDetailsBatch(context.Background(), videos, locale.Locale{}, func(v media.Video) {
		if !v.Detailed {
			t.Errorf("%s should be marked as detailed", v.ID)
		}
		got[v.ID] = v.Duration
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["aaaaaaaaaaa"] != 10 || got["bbbbbbbbbbb"] != 20 {
		t.Fatalf("details were matched to the wrong videos: %v", got)
	}
	data, _ := os.ReadFile(log)
	if runs := strings.Count(string(data), "run"); runs != 1 {
		t.Fatalf("yt-dlp ran %d times, want 1", runs)
	}
}

func TestSearchHidesTheChannelAndPutsItsLiveStreamFirst(t *testing.T) {
	binDir := t.TempDir()
	fake := `#!/bin/sh
args="$*"
case "$args" in
  *--dump-single-json*) exit 1 ;;
  *LIVEPAGE*)
    printf '%s\n' '{"id":"stream00001","title":"Own stream","live_status":"is_live","channel_id":"UCchannel1"}'
    printf '%s\n' '{"id":"stream00002","title":"Other stream","live_status":"is_live","channel_id":"UCother"}'
    ;;
  *'ytsearch35:topic'*)
    printf '%s\n' '{"id":"UCchannel1","title":"Topic","url":"https://www.youtube.com/channel/UCchannel1"}'
    printf '%s\n' '{"id":"first000001","title":"First","channel_id":"UCchannel1"}'
    printf '%s\n' '{"id":"second00001","title":"Second"}'
    ;;
esac
`
	fake = strings.ReplaceAll(fake, "LIVEPAGE", "'"+resultsURL("topic", Filters{Type: TypeLive})+"'")
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
	if _, found := rank["UCchannel1"]; found {
		t.Error("the channel must not be a result")
	}
	if rank["stream00001"] >= rank["first000001"] {
		t.Errorf("the channel's own stream should come first, ranks: %v", rank)
	}
	if rank["stream00002"] <= rank["second00001"] {
		t.Errorf("a stream of another channel keeps its normal place, ranks: %v", rank)
	}
}

func TestFoldNameIgnoresAccentsCaseAndPunctuation(t *testing.T) {
	for in, want := range map[string]string{
		"CazéTV":    "cazetv",
		"Lofi Girl": "lofigirl",
		"Ação! 24h": "acao24h",
	} {
		if got := foldName(in); got != want {
			t.Errorf("foldName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestChannelMatchByNameNeedsALongEnoughName(t *testing.T) {
	c := newTopChannel("cazetv ao vivo", 1)
	if !c.matches(media.Video{Channel: "CazéTV"}, "") {
		t.Error("the channel name inside the query should match")
	}
	if c.matches(media.Video{Channel: "Other Channel"}, "") {
		t.Error("a different channel must not match")
	}
	short := newTopChannel("go tutorial", 1)
	if short.matches(media.Video{Channel: "Go"}, "") {
		t.Error("a very short channel name must not match by name")
	}
	if c.matches(media.Video{Channel: "CazéTV", ChannelID: "UCa"}, "UCb") {
		t.Error("with a channel ID, only that channel matches")
	}
}

// runFakeSearch runs Search for "topic" with a fake yt-dlp and returns the rank
// of every result.
func runFakeSearch(t *testing.T, fake string) map[string]int {
	t.Helper()
	binDir := t.TempDir()
	fake = strings.ReplaceAll(fake, "LIVEPAGE", "'"+resultsURL("topic", Filters{Type: TypeLive})+"'")
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
	return rank
}

func TestSearchFindsTheChannelsStreamsByNameWhenNoChannelIsReturned(t *testing.T) {
	rank := runFakeSearch(t, `#!/bin/sh
case "$*" in
  *--dump-single-json*) exit 1 ;;
  *LIVEPAGE*)
    printf '%s\n' '{"id":"streamname01","title":"Named","live_status":"is_live","channel":"Tópic"}'
    printf '%s\n' '{"id":"streamother01","title":"Other","live_status":"is_live","channel":"Somebody"}'
    ;;
  *'ytsearch35:topic'*)
    printf '%s\n' '{"id":"first000001","title":"First"}'
    printf '%s\n' '{"id":"second00001","title":"Second"}'
    ;;
esac
`)
	if rank["streamname01"] >= rank["first000001"] {
		t.Errorf("a stream of the channel named in the query should come first, ranks: %v", rank)
	}
	if rank["streamother01"] <= rank["second00001"] {
		t.Errorf("a stream of another channel keeps its normal place, ranks: %v", rank)
	}
}

func TestSearchTakesTheChannelFromTheLivePageToo(t *testing.T) {
	rank := runFakeSearch(t, `#!/bin/sh
case "$*" in
  *--dump-single-json*) exit 1 ;;
  *LIVEPAGE*)
    printf '%s\n' '{"id":"UCchannel1","title":"Some name","url":"https://www.youtube.com/channel/UCchannel1"}'
    printf '%s\n' '{"id":"streamown001","title":"Own","live_status":"is_live","channel_id":"UCchannel1","channel":"Unrelated name"}'
    printf '%s\n' '{"id":"streamother01","title":"Other","live_status":"is_live","channel_id":"UCother","channel":"Topic"}'
    ;;
  *'ytsearch35:topic'*)
    printf '%s\n' '{"id":"first000001","title":"First"}'
    ;;
esac
`)
	if _, found := rank["UCchannel1"]; found {
		t.Error("the channel must not be a result")
	}
	if rank["streamown001"] >= rank["first000001"] {
		t.Errorf("the matched channel's stream should come first, ranks: %v", rank)
	}
	if rank["streamother01"] <= rank["first000001"] {
		t.Errorf("with a channel ID, a stream of another channel must not be boosted, ranks: %v", rank)
	}
}
