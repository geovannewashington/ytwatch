package quality

import "testing"

func TestFormat(t *testing.T) {
	for name, want := range map[string]string{
		"best":  "",
		"":      "",
		"1080p": "bestvideo[height<=1080]+bestaudio/best[height<=1080]/best",
		"480P":  "bestvideo[height<=480]+bestaudio/best[height<=480]/best",
		"nope":  "",
	} {
		if got := Format(name); got != want {
			t.Errorf("Format(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestValidAndNormalize(t *testing.T) {
	if !Valid("720p") || Valid("720") || Valid("") {
		t.Fatal("Valid is wrong")
	}
	if Normalize("  1080P ") != "1080p" || Normalize("") != Best {
		t.Fatal("Normalize is wrong")
	}
}
