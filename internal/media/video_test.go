package media

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestKind(t *testing.T) {
	tests := []struct {
		name  string
		video Video
		want  Kind
	}{
		{"ordinary", Video{ID: "abc12345", Duration: 240}, KindVideo},
		{"live status", Video{ID: "abc12345", LiveStatus: "is_live"}, KindLive},
		{"live flag", Video{ID: "abc12345", IsLive: true}, KindLive},
		{"upcoming", Video{ID: "abc12345", LiveStatus: "is_upcoming"}, KindUpcoming},
		{"past live", Video{ID: "abc12345", LiveStatus: "was_live"}, KindPastLive},
		{
			"short path",
			Video{ID: "abc12345", WebpageURL: "https://www.youtube.com/shorts/abc12345", Duration: 40},
			KindShort,
		},
		{"portrait short", Video{ID: "abc12345", Duration: 40, Width: 720, Height: 1280}, KindShort},
		{"short duration is not enough", Video{ID: "abc12345", Duration: 40, Width: 1280, Height: 720}, KindVideo},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.video.Kind(); got != tt.want {
				t.Fatalf("Kind() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCanonicalURL(t *testing.T) {
	got := (Video{ID: "dQw4w9WgXcQ"}).CanonicalURL()
	want := "https://www.youtube.com/watch?v=dQw4w9WgXcQ"
	if got != want {
		t.Fatalf("CanonicalURL() = %q, want %q", got, want)
	}
}

func TestAgeText(t *testing.T) {
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	video := Video{Timestamp: float64(now.Add(-26 * time.Hour).Unix())}
	if got := video.AgeText(now); got != "1d ago" {
		t.Fatalf("AgeText() = %q, want %q", got, "1d ago")
	}
}

func TestPlaylistResult(t *testing.T) {
	data := []byte(`{"_type":"url","id":"PLpCqPSEm2Xe8sEY2haMDUVgwbkIs5NCJI","title":"Go course",
		"url":"https://www.youtube.com/playlist?list=PLpCqPSEm2Xe8sEY2haMDUVgwbkIs5NCJI",
		"thumbnails":[{"url":"http://insecure/x.jpg"},{"url":"https://i.ytimg.com/vi/abc/hq.jpg"}]}`)
	var v Video
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	if !v.IsPlaylist() || v.Kind() != KindPlaylist {
		t.Fatalf("want playlist, got kind %v", v.Kind())
	}
	if got := v.ThumbnailCandidates(); len(got) != 1 || got[0] != "https://i.ytimg.com/vi/abc/hq.jpg" {
		t.Fatalf("thumbnail candidates = %v", got)
	}
	if (Video{ID: "video0001"}).IsPlaylist() {
		t.Fatal("a plain video is not a playlist")
	}
}

func TestPlaylistDetails(t *testing.T) {
	data := []byte(`{"_type":"playlist","id":"PLpCqPSEm2Xe8sEY2haMDUVgwbkIs5NCJI","title":"Go course",
		"playlist_count":110,"modified_date":"20260201","description":"About the course",
		"entries":[{"title":"One","duration":186},{"title":"Two","duration":3725.4},{"title":"Three","duration":null}]}`)
	var details Video
	if err := json.Unmarshal(data, &details); err != nil {
		t.Fatal(err)
	}
	base := Video{ID: details.ID, URL: "https://www.youtube.com/playlist?list=" + details.ID, Title: "Go course"}
	v := MergeDetails(base, details)

	if got := v.PlaylistCountText(); got != "110 videos" {
		t.Fatalf("count = %q", got)
	}
	if v.Description != "About the course" || len(v.Entries) != 3 {
		t.Fatalf("merge lost data: %+v", v)
	}
	want := []string{"3:06", "1:02:05", ""}
	for i, e := range v.Entries {
		if got := e.DurationText(); got != want[i] {
			t.Errorf("entry %d duration = %q, want %q", i, got, want[i])
		}
	}
	if (Video{PlaylistCount: 1}).PlaylistCountText() != "1 video" || (Video{}).PlaylistCountText() != "" {
		t.Fatal("unexpected count text for 1 or unknown")
	}
}

func TestUpdatedText(t *testing.T) {
	now := time.Date(2026, 10, 12, 15, 0, 0, 0, time.UTC)
	cases := map[string]string{
		"20261012": "updated today",
		"20261011": "updated yesterday",
		"20261005": "updated 7d ago",
		"20260801": "updated 2mo ago",
		"20240101": "updated 2y ago",
		"":         "",
		"garbage":  "",
	}
	for date, want := range cases {
		if got := (Video{ModifiedDate: date}).UpdatedText(now); got != want {
			t.Errorf("UpdatedText(%q) = %q, want %q", date, got, want)
		}
	}
}

func TestThumbnailCandidatesGoFromBestToWorst(t *testing.T) {
	v := Video{ID: "video0001", Thumbnails: []ThumbnailRef{
		{URL: "https://i.ytimg.com/vi/video0001/hq720.jpg?sqp=small", Width: 360},
		{URL: "https://i.ytimg.com/vi/video0001/hq720.jpg?sqp=large", Width: 720},
		{URL: "http://insecure.example/huge.jpg", Width: 4000},
	}}
	want := []string{
		"https://i.ytimg.com/vi/video0001/hq720.jpg",
		"https://i.ytimg.com/vi/video0001/hq720.jpg?sqp=large",
		"https://i.ytimg.com/vi/video0001/hq720.jpg?sqp=small",
		"https://i.ytimg.com/vi/video0001/mqdefault.jpg",
	}
	if got := v.ThumbnailCandidates(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestThumbnailCandidatesForPlaylistsAndUnknownIDs(t *testing.T) {
	playlist := Video{
		ID:         "PLpCqPSEm2Xe8sEY2haMDUVgwbkIs5NCJI",
		URL:        "https://www.youtube.com/playlist?list=PLpCqPSEm2Xe8sEY2haMDUVgwbkIs5NCJI",
		Thumbnails: []ThumbnailRef{{URL: "https://i.ytimg.com/vi/abc/hqdefault.jpg?sqp=x", Width: 480}},
	}
	if got := playlist.ThumbnailCandidates(); len(got) != 1 || got[0] != playlist.Thumbnails[0].URL {
		t.Fatalf("playlist candidates = %v", got)
	}
	if got := (Video{}).ThumbnailCandidates(); len(got) != 0 {
		t.Fatalf("no id and no list should give none, got %v", got)
	}
}

func TestDurationTextFormatsShortsAsMinutesAndSeconds(t *testing.T) {
	cases := []struct {
		video Video
		want  string
	}{
		{Video{Duration: 15, DurationString: "15"}, "0:15"},
		{Video{Duration: 4073, DurationString: "1:07:53"}, "1:07:53"},
		{Video{Duration: 0, DurationString: "12:34"}, "12:34"},
		{Video{}, "duration unknown"},
	}
	for _, tc := range cases {
		if got := tc.video.DurationText(); got != tc.want {
			t.Errorf("DurationText(%+v) = %q, want %q", tc.video, got, tc.want)
		}
	}
}

func TestIsChannelRecognizesChannelEntriesOnly(t *testing.T) {
	cases := map[string]bool{
		"https://www.youtube.com/channel/UCSJ4gkVC6NrvII8umztf0Ow": true,
		"https://www.youtube.com/@LofiGirl":                        true,
		"https://www.youtube.com/watch?v=rFZHOHl-L8A":              false,
		"https://www.youtube.com/playlist?list=PL123456":           false,
		"https://www.youtube.com/shorts/abcdefghijk":               false,
	}
	for rawURL, want := range cases {
		if got := (Video{URL: rawURL}).IsChannel(); got != want {
			t.Errorf("IsChannel(%s) = %v, want %v", rawURL, got, want)
		}
	}
}

func TestAgeTextSaysWhenALiveStreamStarted(t *testing.T) {
	now := time.Now()
	live := func(ago time.Duration) Video {
		return Video{LiveStatus: "is_live", Timestamp: float64(now.Add(-ago).Unix())}
	}
	for ago, want := range map[time.Duration]string{
		20 * time.Second: "started streaming just now",
		25 * time.Minute: "started streaming 25m ago",
		3 * time.Hour:    "started streaming 3h ago",
		50 * time.Hour:   "started streaming 2d ago",
	} {
		if got := live(ago).AgeText(now); got != want {
			t.Errorf("live for %v: got %q, want %q", ago, got, want)
		}
	}
	recording := Video{Timestamp: float64(now.Add(-3 * time.Hour).Unix())}
	if got := recording.AgeText(now); got != "3h ago" {
		t.Errorf("a recording keeps the plain age, got %q", got)
	}
}

func TestViewsTextSaysWatchingNowForLiveStreams(t *testing.T) {
	live := Video{LiveStatus: "is_live", ConcurrentViewers: 41200}
	if got := live.ViewsText(); got != "41.2K watching now" {
		t.Errorf("got %q", got)
	}
	if got := (Video{ViewCount: 41200}).ViewsText(); got != "41.2K views" {
		t.Errorf("a recording shows views, got %q", got)
	}
}

func TestHasChatReplayNeedsAPastStreamAndAToken(t *testing.T) {
	for name, tc := range map[string]struct {
		video Video
		want  bool
	}{
		"past stream with token": {Video{LiveStatus: "was_live", ChatReplayToken: "tok"}, true},
		"past stream, no token":  {Video{LiveStatus: "was_live"}, false},
		"plain video":            {Video{LiveStatus: "not_live", ChatReplayToken: "tok"}, false},
		"stream on air":          {Video{LiveStatus: "is_live", ChatReplayToken: "tok"}, false},
	} {
		if got := tc.video.HasChatReplay(); got != tc.want {
			t.Errorf("%s: HasChatReplay() = %v, want %v", name, got, tc.want)
		}
	}

	merged := MergeDetails(Video{ID: "abcdefghijk"}, Video{LiveStatus: "was_live", ChatReplayToken: "tok"})
	if merged.ChatReplayToken != "tok" {
		t.Error("MergeDetails should keep the chat replay token")
	}
	if kept := MergeDetails(merged, Video{}); kept.ChatReplayToken != "tok" {
		t.Error("details without a token must not erase it")
	}
}

func TestChannelBadge(t *testing.T) {
	for name, tc := range map[string]struct {
		video Video
		want  string
	}{
		"none":     {Video{}, ""},
		"verified": {Video{ChannelVerified: true}, "\uf058"},
		"artist":   {Video{ChannelVerified: true, ChannelArtist: true}, "\uf001"},
		"artist, even if only the artist flag is set": {Video{ChannelArtist: true}, "\uf001"},
	} {
		if got := tc.video.ChannelBadge(); got != tc.want {
			t.Errorf("%s: ChannelBadge() = %q, want %q", name, got, tc.want)
		}
	}
}

func TestMergeDetailsKeepsTheChannelBadge(t *testing.T) {
	flat := Video{ID: "abcdefghijk", ChannelVerified: true} // from the search
	merged := MergeDetails(flat, Video{ChannelVerified: true, ChannelArtist: true})
	if !merged.ChannelVerified || !merged.ChannelArtist {
		t.Errorf("the details tell it is an artist: %+v", merged)
	}

	if kept := MergeDetails(merged, Video{}); !kept.ChannelVerified || !kept.ChannelArtist {
		t.Errorf("details without a badge must not remove it: %+v", kept)
	}
}

func TestChannelResult(t *testing.T) {
	data := []byte(`{"_type":"url","id":"UCZwW2PXNovXy_xUIhmYv7cg","title":"Tiago","channel":"Tiago",
		"url":"https://www.youtube.com/channel/UCZwW2PXNovXy_xUIhmYv7cg","uploader_id":"@TiagoTaquelim",
		"channel_follower_count":46300,
		"thumbnails":[{"url":"//yt3.ggpht.com/a=s88","width":88},{"url":"//yt3.ggpht.com/a=s176","width":176},{"url":"//plain.example/x"}]}`)
	var v Video
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	if !v.IsChannel() || v.IsPlaylist() || v.Kind() != KindChannel {
		t.Fatalf("want channel, got kind %v", v.Kind())
	}
	if got := v.SubscribersText(); got != "46.3K subscribers" {
		t.Errorf("SubscribersText() = %q", got)
	}
	// Channel pictures come without a scheme; the largest comes first.
	want := []string{"https://yt3.ggpht.com/a=s176", "https://yt3.ggpht.com/a=s88", "https://plain.example/x"}
	if got := v.ThumbnailCandidates(); !reflect.DeepEqual(got, want) {
		t.Errorf("thumbnail candidates = %v, want %v", got, want)
	}
	if got := v.ChannelPage(); got != "https://www.youtube.com/channel/UCZwW2PXNovXy_xUIhmYv7cg" {
		t.Errorf("ChannelPage() = %q", got)
	}
	if _, hideable := ParseKind("channels"); hideable {
		t.Error("channels cannot be hidden")
	}
	if (Video{ID: "video0001"}).IsChannel() {
		t.Error("a plain video is not a channel")
	}
}

func TestChannelPageOfAVideo(t *testing.T) {
	const id = "UCZwW2PXNovXy_xUIhmYv7cg"
	for name, tc := range map[string]struct {
		video Video
		want  string
	}{
		"channel URL":    {Video{ID: "video0001", ChannelURL: "https://www.youtube.com/@x"}, "https://www.youtube.com/@x"},
		"channel ID":     {Video{ID: "video0001", ChannelID: id}, "https://www.youtube.com/channel/" + id},
		"URL wins":       {Video{ID: "video0001", ChannelURL: "https://www.youtube.com/@x", ChannelID: id}, "https://www.youtube.com/@x"},
		"nothing":        {Video{ID: "video0001"}, ""},
		"odd channel ID": {Video{ID: "video0001", ChannelID: "../etc"}, ""},
		"not a web link": {Video{ID: "video0001", ChannelURL: "javascript:alert(1)"}, ""},
	} {
		if got := tc.video.ChannelPage(); got != tc.want {
			t.Errorf("%s: ChannelPage() = %q, want %q", name, got, tc.want)
		}
	}
	if (Video{}).SubscribersText() != "" || (Video{SubscriberCount: 1}).SubscribersText() != "1 subscriber" {
		t.Error("SubscribersText is wrong")
	}
}
