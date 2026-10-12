package youtube

import "testing"

func TestParseShortsReadsTheShelfInPageOrder(t *testing.T) {
	page := []byte(`<script>var ytInitialData = {"a":{"contents":[
	{"shortsLockupViewModel":{"entityId":"shorts-shelf-item-BBBBBBBBBBB","indexInCollection":1,
	 "accessibilityText":"Second, 2 views - play Short","overlayMetadata":{"primaryText":{"content":"Second"}}}},
	{"shortsLockupViewModel":{"entityId":"shorts-shelf-item-AAAAAAAAAAA","indexInCollection":0,
	 "accessibilityText":"First, one, 16 million views - play Short"}},
	{"shortsLockupViewModel":{"entityId":"shorts-shelf-item-AAAAAAAAAAA","indexInCollection":2}}
	]}};</script>`)
	got, err := parseShorts(page)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d Shorts, want 2 (duplicates dropped)", len(got))
	}
	if got[0].ID != "AAAAAAAAAAA" || got[0].Title != "First, one" || !got[0].Short ||
		got[0].WebpageURL != "https://www.youtube.com/shorts/AAAAAAAAAAA" {
		t.Errorf("first = %+v", got[0])
	}
	if got[1].ID != "BBBBBBBBBBB" || got[1].Title != "Second" {
		t.Errorf("second = %+v", got[1])
	}
}

func TestParseShortsRejectsOtherPages(t *testing.T) {
	if _, err := parseShorts([]byte("<html>consent</html>")); err == nil {
		t.Fatal("want an error")
	}
}
