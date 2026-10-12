package youtube

import "testing"

// The expected values are the "sp" parameters youtube.com itself uses for
// each single filter.
func TestParamMatchesYouTubesOwnValues(t *testing.T) {
	for name, tc := range map[string]struct {
		filters Filters
		want    string
	}{
		"no filters":     {Filters{}, ""},
		"videos only":    {Filters{Type: TypeVideos}, "EgIQAQ=="},
		"Shorts only":    {Filters{Type: TypeShorts}, "EgIQCQ=="},
		"playlists only": {Filters{Type: TypePlaylists}, "EgIQAw=="},
		"live":           {Filters{Type: TypeLive}, "EgQQAUAB"},
		"under 4 min":    {Filters{Duration: 1}, "EgQQARgB"},
		"4-20 min":       {Filters{Duration: 2}, "EgQQARgD"},
		"over 20 min":    {Filters{Duration: 3}, "EgQQARgC"},
		"today":          {Filters{Date: 1}, "EgQIAhAB"},
		"this week":      {Filters{Date: 2}, "EgQIAxAB"},
		"this month":     {Filters{Date: 3}, "EgQIBBAB"},
		"this year":      {Filters{Date: 4}, "EgQIBRAB"},
		"week and long":  {Filters{Date: 2, Duration: 3}, "EgYIAxABGAI="},
	} {
		if got := tc.filters.Param(); got != tc.want {
			t.Errorf("%s: Param() = %q, want %q", name, got, tc.want)
		}
	}
}

func TestActiveSummaryAndSet(t *testing.T) {
	if (Filters{}).Active() || (Filters{}).Summary() != "" {
		t.Fatal("the zero value means no filters")
	}
	f := Filters{}
	f.Set(RowDate, 2)
	f.Set(RowDuration, 3)
	f.Set(RowType, TypeLive)
	if !f.Active() {
		t.Fatal("filters should be active")
	}
	if got, want := f.Summary(), "Live, Over 20 minutes, This week"; got != want {
		t.Fatalf("Summary() = %q, want %q", got, want)
	}
	// Set wraps around both ends of a row.
	f.Set(RowDate, -1)
	if f.Date != len(Rows[RowDate].Options)-1 {
		t.Fatalf("Set(-1) = %d, want the last option", f.Date)
	}
	f.Set(RowDate, len(Rows[RowDate].Options))
	if f.Date != 0 {
		t.Fatalf("Set past the end = %d, want 0", f.Date)
	}
	for row := range Rows {
		if (Filters{}).Get(row) != 0 {
			t.Errorf("row %d default is not 0", row)
		}
	}
}

func TestResultsURLEscapesTheQueryAndParam(t *testing.T) {
	got := resultsURL("go tutorial & more", Filters{Type: TypePlaylists})
	want := "https://www.youtube.com/results?search_query=go+tutorial+%26+more&sp=EgIQAw%253D%253D"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}
