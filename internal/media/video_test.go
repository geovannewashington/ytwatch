package media

import (
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
