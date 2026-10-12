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
		{"short path", Video{ID: "abc12345", WebpageURL: "https://www.youtube.com/shorts/abc12345", Duration: 40}, KindShort},
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
