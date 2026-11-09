package youtube

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geovannewashington/ytwatch/internal/locale"
	"github.com/geovannewashington/ytwatch/internal/media"
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

func fakeChannelYtDlp(t *testing.T) (log string) {
	t.Helper()
	binDir := t.TempDir()
	log = filepath.Join(binDir, "log")
	fake := `#!/bin/sh
echo "$*" >> ` + log + `
case "$*" in
  *@x/shorts*) echo "ERROR: This channel does not have a shorts tab" >&2; exit 1 ;;
  *@x/streams*) ;;
  *@x/podcasts*)
    printf '%s\n' '{"id":"PLshow12345","title":"A show","url":"https://www.youtube.com/playlist?list=PLshow12345"}'
    ;;
  *@x/playlists*)
    printf '%s\n' '{"id":"PLabc123456","title":"A list","channel":"Playlist","url":"https://www.youtube.com/playlist?list=PLabc123456","playlist_uploader":"The X"}'
    printf '%s\n' '{"id":"UC0000000000000000000000","title":"Not a playlist","url":"https://www.youtube.com/channel/UC0000000000000000000000"}'
    ;;
  *@x/videos*)
    printf '%s\n' '{"id":"first000001","title":"First","url":"https://www.youtube.com/watch?v=first000001","playlist_uploader":"The X","playlist_channel_id":"UCthex"}'
    printf '%s\n' '{"id":"second00001","title":"Second","url":"https://www.youtube.com/watch?v=second00001","channel_url":"https://www.youtube.com/channel/UCreal"}'
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "yt-dlp"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	// The page of the tab is not fetched from YouTube. It has one course, which
	// yt-dlp does not list.
	old := fetchResultsPage
	t.Cleanup(func() { fetchResultsPage = old })
	fetchResultsPage = func(context.Context, string, locale.Locale) ([]byte, error) {
		return []byte(coursePage), nil
	}

	return log
}

// coursePage is a channel page with a course and a playlist, as YouTube sends
// them.
const coursePage = `<script>var ytInitialData = {"contents":{"items":[` +
	`{"lockupViewModel":{"contentType":"LOCKUP_CONTENT_TYPE_COURSE","contentId":"PLcourse0001",` +
	`"contentImage":{"collectionThumbnailViewModel":{"primaryThumbnail":{"thumbnailViewModel":{` +
	`"image":{"sources":[{"url":"https://i.ytimg.com/vi/abcdefghijk/hqdefault.jpg"}]},` +
	`"overlays":[{"thumbnailOverlayBadgeViewModel":{"thumbnailBadges":[{"thumbnailBadgeViewModel":{"text":"10 lessons"}}]}}]}}}},` +
	`"metadata":{"lockupMetadataViewModel":{"title":{"content":"A course"}}}}},` +
	`{"lockupViewModel":{"contentType":"LOCKUP_CONTENT_TYPE_PLAYLIST","contentId":"PLabc123456",` +
	`"metadata":{"lockupMetadataViewModel":{"title":{"content":"A list"}}}}},` +
	`{"lockupViewModel":{"contentType":"LOCKUP_CONTENT_TYPE_COURSE","contentId":"bad id!",` +
	`"metadata":{"lockupMetadataViewModel":{"title":{"content":"Not safe"}}}}}` +
	`]}};</script>`

func browseChannel(tab int, name string) (rows []Event, done Event) {
	BrowseChannel(context.Background(), "https://www.youtube.com/@x/", tab, name, locale.Locale{}, func(e Event) {
		switch e.Kind {
		case EventResult:
			rows = append(rows, e)
		case EventSearchDone:
			done = e
		}
	})
	return rows, done
}

func TestBrowseChannelListsATabAndNamesTheChannel(t *testing.T) {
	log := fakeChannelYtDlp(t)

	rows, done := browseChannel(ChannelVideos, "Fallback")
	if len(rows) != 2 || done.Err != nil || done.Count != 2 {
		t.Fatalf("rows = %d, done = %+v", len(rows), done)
	}
	if rows[0].Video.Channel != "The X" || rows[1].Video.Channel != "The X" {
		t.Errorf("the channel name should come from the page: %q, %q", rows[0].Video.Channel, rows[1].Video.Channel)
	}
	if rows[0].Video.ChannelURL != "https://www.youtube.com/@x/" || rows[1].Video.ChannelURL != "https://www.youtube.com/channel/UCreal" {
		t.Errorf("an empty channel link is filled and a known one kept: %q, %q", rows[0].Video.ChannelURL, rows[1].Video.ChannelURL)
	}
	if rows[0].Video.ChannelID != "UCthex" {
		t.Errorf("the channel ID comes from the tab: %q", rows[0].Video.ChannelID)
	}
	if rows[0].Rank >= rows[1].Rank {
		t.Error("ranks must follow the tab's order")
	}

	data, _ := os.ReadFile(log)
	if !strings.Contains(string(data), "https://www.youtube.com/@x/videos") {
		t.Errorf("yt-dlp should read the tab of the channel: %s", data)
	}
}

func TestBrowseChannelTabsAndTheirEmptyCases(t *testing.T) {
	log := fakeChannelYtDlp(t)

	rows, done := browseChannel(ChannelPlaylists, "Fallback")
	// The courses come after the playlists, but rank before them.
	slices.SortFunc(rows, func(a, b Event) int { return a.Rank - b.Rank })
	if len(rows) != 2 || rows[0].Video.ID != "PLcourse0001" || rows[1].Video.ID != "PLabc123456" ||
		rows[1].Video.Channel != "The X" || done.Err != nil {
		t.Fatalf("the Playlists tab keeps playlists only, after the courses the page tells: %+v %+v", rows, done)
	}
	if rows[0].Rank >= rows[1].Rank || !rows[0].Video.IsPlaylist() || rows[0].Video.Title != "A course" {
		t.Errorf("a course is a playlist, shown first: %+v", rows[0])
	}

	// Podcasts are listed like playlists, without the courses.
	if rows, done = browseChannel(ChannelPodcasts, ""); len(rows) != 1 || rows[0].Video.ID != "PLshow12345" || done.Err != nil ||
		rows[0].Video.PlaylistType != media.PlaylistPodcast {
		t.Fatalf("the Podcasts tab lists its shows: %+v %+v", rows, done)
	}

	if rows, done = browseChannel(ChannelLive, ""); len(rows) != 0 || done.Err == nil ||
		!strings.Contains(done.Err.Error(), "no live found") {
		t.Fatalf("an empty tab says so: %+v %+v", rows, done)
	}

	// yt-dlp fails on a tab the channel does not have: that is an empty tab.
	if _, done = browseChannel(ChannelShorts, ""); done.Err == nil || done.Err.Error() != "no shorts found in this channel" {
		t.Fatalf("a missing tab is an empty one: %+v", done)
	}

	data, _ := os.ReadFile(log)
	for _, tab := range []string{"/playlists", "/podcasts", "/streams", "/shorts"} {
		if !strings.Contains(string(data), "@x"+tab) {
			t.Errorf("missing a request for %s: %s", tab, data)
		}
	}
}

func TestSearchWithTheChannelsFilterKeepsOnlyChannels(t *testing.T) {
	binDir := t.TempDir()
	fake := `#!/bin/sh
echo "$*" >> ` + filepath.Join(binDir, "log") + `
printf '%s\n' '{"id":"UCZwW2PXNovXy_xUIhmYv7cg","title":"Tiago","url":"https://www.youtube.com/channel/UCZwW2PXNovXy_xUIhmYv7cg"}'
printf '%s\n' '{"id":"video000001","title":"A video","url":"https://www.youtube.com/watch?v=video000001"}'
`
	if err := os.WriteFile(filepath.Join(binDir, "yt-dlp"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var ids []string
	Search(context.Background(), "topic", Filters{Type: TypeChannels}, nil, locale.Locale{}, func(e Event) {
		if e.Kind == EventResult {
			ids = append(ids, e.Video.ID)
		}
	})
	if len(ids) != 1 || ids[0] != "UCZwW2PXNovXy_xUIhmYv7cg" {
		t.Fatalf("results = %v, want the channel only", ids)
	}
	data, _ := os.ReadFile(filepath.Join(binDir, "log"))
	if !strings.Contains(string(data), "sp=EgIQAg%253D%253D") || strings.Count(string(data), "\n") != 1 {
		t.Errorf("one query with the channels filter was expected: %s", data)
	}
}

func TestFetchDetailsSkipsChannels(t *testing.T) {
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "yt-dlp"), []byte("#!/bin/sh\ntouch "+filepath.Join(binDir, "ran")+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	channel := media.Video{ID: "UCZwW2PXNovXy_xUIhmYv7cg", URL: "https://www.youtube.com/channel/UCZwW2PXNovXy_xUIhmYv7cg"}
	if err := FetchDetailsBatch(context.Background(), []media.Video{channel}, locale.Locale{}, func(media.Video) {
		t.Error("a channel has no details")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(binDir, "ran")); err == nil {
		t.Error("yt-dlp should not run for a channel")
	}
}

func TestParseChannelPageReadsTheTabsTheChannelHas(t *testing.T) {
	tab := func(path string) string {
		return `{"tabRenderer":{"endpoint":{"commandMetadata":{"webCommandMetadata":{"url":"/@x/` + path + `"}}}}}`
	}
	page := []byte(`<script>var ytInitialData = {"metadata":{"channelMetadataRenderer":{"title":"The X","externalId":"UCthex","vanityChannelUrl":"http://www.youtube.com/@TheX"}},` +
		`"contents":{"twoColumnBrowseResultsRenderer":{"tabs":[` +
		tab("featured") + `,` + tab("playlists") + `,` + tab("videos") + `,` + tab("podcasts") + `,` + tab("posts") +
		`,{"expandableTabRenderer":{}}]}}};</script>`)

	info, err := parseChannelPage(page)
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "UCthex" || info.Name != "The X" || info.Handle != "@TheX" ||
		!slices.Equal(info.Tabs, []int{ChannelVideos, ChannelPodcasts, ChannelPlaylists}) {
		t.Errorf("info = %+v", info)
	}

	if _, err := parseChannelPage([]byte(`<html>consent</html>`)); err == nil {
		t.Error("a page that is not a channel page is an error")
	}
}

func TestParseCoursesKeepsSafeCoursesInPageOrder(t *testing.T) {
	courses := parseCourses([]byte(coursePage))
	if len(courses) != 1 || courses[0].ID != "PLcourse0001" || courses[0].Title != "A course" ||
		courses[0].CanonicalURL() != "https://www.youtube.com/playlist?list=PLcourse0001" {
		t.Fatalf("courses = %+v", courses)
	}
	if c := courses[0]; c.PlaylistType != media.PlaylistCourse || c.PlaylistCount != 10 ||
		c.Thumbnail != "https://i.ytimg.com/vi/abcdefghijk/hqdefault.jpg" || c.PlaylistCountText() != "10 lessons" {
		t.Errorf("a course has its picture, lessons and kind: %+v", c)
	}
	if got := courses[0].ThumbnailCandidates(); len(got) != 1 {
		t.Errorf("thumbnail candidates = %v", got)
	}
	if got := parseCourses([]byte(`<html>consent</html>`)); got != nil {
		t.Errorf("a page that is not a channel page has no courses: %+v", got)
	}
}

func TestBrowseChannelPagesStartAfterThePageBeforeAndSayWhenFull(t *testing.T) {
	log := fakeChannelYtDlp(t)

	var rows []Event
	var done Event
	BrowseChannelFrom(context.Background(), "https://www.youtube.com/@x/", ChannelVideos, 1+BrowsePageSize, "", locale.Locale{}, func(e Event) {
		switch e.Kind {
		case EventResult:
			rows = append(rows, e)
		case EventSearchDone:
			done = e
		}
	})
	data, _ := os.ReadFile(log)
	if !strings.Contains(string(data), "--playlist-start 101") || !strings.Contains(string(data), "--playlist-end 200") {
		t.Errorf("the second page asks for entries 101 to 200: %s", data)
	}
	if len(rows) != 2 || rows[0].Rank != BrowsePageSize*10 {
		t.Errorf("ranks continue after the first page: %+v", rows)
	}
	if done.More {
		t.Error("a page with 2 entries is the last")
	}
}

func TestBrowseSaysWhenAFullPageMayHaveMore(t *testing.T) {
	binDir := t.TempDir()
	fake := "#!/bin/sh\ni=0\nwhile [ $i -lt " + strconv.Itoa(BrowsePageSize) + " ]; do\n  i=$((i+1))\n" +
		"  printf '{\"id\":\"v%010d\",\"title\":\"V\"}\\n' $i\ndone\n"
	if err := os.WriteFile(filepath.Join(binDir, "yt-dlp"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var done Event
	count := 0
	Browse(context.Background(), "https://www.youtube.com/playlist?list=PLabc", locale.Locale{}, func(e Event) {
		switch e.Kind {
		case EventResult:
			count++
		case EventSearchDone:
			done = e
		}
	})
	if count != BrowsePageSize || !done.More {
		t.Fatalf("results = %d, more = %v", count, done.More)
	}
}

func TestHandleOfReadsTheHandleOfAChannelAddress(t *testing.T) {
	for address, want := range map[string]string{
		"http://www.youtube.com/@ForsenVerb":                       "@ForsenVerb",
		"https://www.youtube.com/@x_y.z-1":                         "@x_y.z-1",
		"https://www.youtube.com/channel/UCZwW2PXNovXy_xUIhmYv7cg": "",
		"https://www.youtube.com/@%E3%83%86%E3%82%B9%E3%83%88":     "@テスト",
		"": "",
	} {
		if got := HandleOf(address); got != want {
			t.Errorf("HandleOf(%q = %q, want %q", address, got, want)
		}
	}
}

const artistPage = `<script>var ytInitialData = {"contents":{"items":[` +
	`{"channelRenderer":{"channelId":"UCartist0000000000000000","ownerBadges":[{"metadataBadgeRenderer":{"style":"BADGE_STYLE_TYPE_VERIFIED_ARTIST"}}]}},` +
	`{"channelRenderer":{"channelId":"UCverified000000000000000","ownerBadges":[{"metadataBadgeRenderer":{"style":"BADGE_STYLE_TYPE_VERIFIED"}}]}},` +
	`{"channelRenderer":{"channelId":"UCplain0000000000000000"}}` +
	`]}};</script>`

func TestParseArtistChannelsKeepsOnlyOfficialArtists(t *testing.T) {
	got := parseArtistChannels([]byte(artistPage))
	if len(got) != 1 || !got["UCartist0000000000000000"] {
		t.Errorf("artists = %v", got)
	}
	if parseArtistChannels([]byte("<html>consent</html>")) != nil {
		t.Error("a page that is not a results page has none")
	}
}

func TestTheChannelsFilterMarksArtistChannelsFromThePage(t *testing.T) {
	binDir := t.TempDir()
	fake := `#!/bin/sh
printf '%s\n' '{"id":"UCartist0000000000000000","title":"Singer","channel_is_verified":true,"url":"https://www.youtube.com/channel/UCartist0000000000000000"}'
printf '%s\n' '{"id":"UCverified000000000000000","title":"Maker","channel_is_verified":true,"url":"https://www.youtube.com/channel/UCverified000000000000000"}'
`
	if err := os.WriteFile(filepath.Join(binDir, "yt-dlp"), []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	old := fetchResultsPage
	t.Cleanup(func() { fetchResultsPage = old })
	fetchResultsPage = func(context.Context, string, locale.Locale) ([]byte, error) { return []byte(artistPage), nil }

	badges := map[string]string{}
	Search(context.Background(), "singer", Filters{Type: TypeChannels}, nil, locale.Locale{}, func(e Event) {
		if e.Kind == EventResult {
			badges[e.Video.ID] = e.Video.ChannelBadge()
		}
	})
	if badges["UCartist0000000000000000"] == "" || badges["UCartist0000000000000000"] == badges["UCverified000000000000000"] {
		t.Errorf("an artist channel has its own mark: %q", badges)
	}

	// Without the page the channels are still listed, with the verified mark.
	fetchResultsPage = func(context.Context, string, locale.Locale) ([]byte, error) { return nil, errors.New("offline") }
	badges = map[string]string{}
	Search(context.Background(), "singer", Filters{Type: TypeChannels}, nil, locale.Locale{}, func(e Event) {
		if e.Kind == EventResult {
			badges[e.Video.ID] = e.Video.ChannelBadge()
		}
	})
	if len(badges) != 2 || badges["UCartist0000000000000000"] != badges["UCverified000000000000000"] {
		t.Errorf("without the page both are verified: %q", badges)
	}
}

func TestSearchPagesAskForTheEntriesAfterTheEarlierPages(t *testing.T) {
	run := func(filters Filters, page int) (args string, done Event) {
		binDir := t.TempDir()
		log := filepath.Join(binDir, "log")
		var fake strings.Builder
		fake.WriteString("#!/bin/sh\necho \"$*\" >> " + log + "\ni=0\nwhile [ $i -lt 400 ]; do\n  i=$((i+1))\n")
		fake.WriteString("  printf '{\"id\":\"v%010d\",\"title\":\"V\"}\\n' $i\ndone\n")
		if err := os.WriteFile(filepath.Join(binDir, "yt-dlp"), []byte(fake.String()), 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
		SearchPage(context.Background(), "topic", filters, nil, locale.Locale{}, page, func(e Event) {
			if e.Kind == EventSearchDone {
				done = e
			}
		})
		data, _ := os.ReadFile(log)
		return string(data), done
	}

	args, done := run(Filters{Type: TypeVideos}, 1)
	if strings.Contains(args, "--playlist-start") {
		t.Errorf("the first page starts at the first entry: %s", args)
	}
	if !done.More {
		t.Error("a full page may have a next one")
	}
	if _, done = run(Filters{}, 1); !done.More {
		t.Error("a full first page of the main search may have a next one")
	}
	// A short page is the last one, and so is any Shorts search.
	if _, done = run(Filters{Type: TypeShorts}, 1); done.More {
		t.Error("Shorts have one page")
	}

	// A page asks for more than ResultLimit when kinds are hidden, so that the
	// list still fills; the next page starts after what this one asked for.
	n := fetchCount(ResultLimit, hiddenKinds(nil, Filters{Type: TypeVideos}))
	args, _ = run(Filters{Type: TypeVideos}, 2)
	if want := fmt.Sprintf("--playlist-start %d --playlist-end %d", n+1, 2*n); !strings.Contains(args, want) {
		t.Errorf("the second filtered page is entries %d to %d: %s", n+1, 2*n, args)
	}

	n = fetchCount(videoResultCount, hiddenKinds(nil, Filters{}))
	args, _ = run(Filters{}, 2)
	if !strings.Contains(args, fmt.Sprintf("ytsearch%d:topic", 2*n)) || !strings.Contains(args, fmt.Sprintf("--playlist-start %d", n+1)) {
		t.Errorf("the second page of the main search is entries %d to %d: %s", n+1, 2*n, args)
	}
	if strings.Contains(args, "shorts") {
		t.Errorf("Shorts have no second page: %s", args)
	}
}
