package youtube

import (
	"context"
	"errors"
	"fmt"
	"github.com/geovannewashington/ytwatch/internal/locale"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

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

func init() {
	// Tests never use the network. A test that needs a page sets its own.
	fetchResultsPage = func(context.Context, string, locale.Locale) ([]byte, error) {
		return nil, errors.New("offline")
	}
}

func TestShortRankingPutsMatchingTitlesFirst(t *testing.T) {
	rank := shortRanking("Ranking funny cat")
	// The exact title, ignoring case and punctuation, goes first of all.
	if got := rank(7, "RANKING funny cat!"); got != -1 {
		t.Errorf("an exact match has rank %d", got)
	}
	// Every query word is in the title: just after the regular result at the
	// same position.
	if got := rank(0, "Ranking Funny Cat Moment 😂"); got != mainRanking.at(0)+shortMatchOffset {
		t.Errorf("a matching Short at position 0 has rank %d", got)
	}
	// Otherwise it joins the group after the first few results, in order.
	// The first Short of the page is YouTube's best match, whatever its title
	// says (it may be translated).
	if got := rank(0, "Ninguém está lendo nada"); got != shortFirstRank {
		t.Errorf("the first Short has rank %d, want %d", got, shortFirstRank)
	}
	a, b := rank(1, "Something else"), rank(2, "Another one")
	if a != shortGroupBase+3 || b <= a || a < mainRanking.at(5) {
		t.Errorf("group ranks = %d, %d", a, b)
	}
	// An empty query matches nothing.
	if got := shortRanking("")(1, "x"); got != shortGroupBase+3 {
		t.Errorf("empty query rank = %d", got)
	}
}

func TestSearchAddsShortsToAMixedSearch(t *testing.T) {
	old := fetchResultsPage
	defer func() { fetchResultsPage = old }()
	var asked string
	fetchResultsPage = func(_ context.Context, u string, _ locale.Locale) ([]byte, error) {
		asked = u
		var b strings.Builder
		b.WriteString(`<script>var ytInitialData = {"x":[`)
		for i := 0; i < 12; i++ {
			title := "Other short"
			if i == 3 {
				title = "Ranking Funny Cat Moment"
			}
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"shortsLockupViewModel":{"entityId":"shorts-shelf-item-short%06d","indexInCollection":%d,"overlayMetadata":{"primaryText":{"content":%q}}}}`, i, i, title)
		}
		b.WriteString(`]};</script>`)
		return []byte(b.String()), nil
	}
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "yt-dlp"), []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	ranks := map[string]int{}
	var mu sync.Mutex
	Search(context.Background(), "ranking funny cat moment", Filters{}, nil, locale.Locale{}, func(e Event) {
		if e.Kind == EventResult {
			mu.Lock()
			ranks[e.Video.Title+"/"+e.Video.ID] = e.Rank
			mu.Unlock()
		}
	})
	if !strings.Contains(asked, "sp=EgIQCQ") {
		t.Fatalf("the Shorts page was not asked for: %q", asked)
	}
	if len(ranks) != shortResultCount {
		t.Fatalf("got %d Shorts, want %d", len(ranks), shortResultCount)
	}
	if got := ranks["Ranking Funny Cat Moment/short000003"]; got != -1 {
		t.Errorf("the matching Short has rank %d", got)
	}
}
