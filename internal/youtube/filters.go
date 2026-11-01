package youtube

import (
	"encoding/base64"
	"net/url"
	"strings"
)

// Filters are the search filters of the youtube.com results page. Each field
// is the index of the option in Rows, so the zero value is "no filter": any
// type, any length, any date.
// The names and options follow the filter menu of youtube.com.
type Filters struct {
	Type     int
	Duration int
	Date     int
}

// Row is one line of the filter menu.
type Row struct {
	Name    string
	Options []string
	// Help explains the row in the menu.
	Help string
}

// Rows describes the filters, in the order the menu shows them. Option 0 of
// every row is the default.
var Rows = []Row{
	{"Type", []string{"Any", "Videos", "Shorts", "Live", "Playlists"}, "What to search for. Live: streams happening now."},
	{"Duration", []string{"Any", "Under 4 minutes", "4 - 20 minutes", "Over 20 minutes"}, "How long the video is."},
	{"Upload date", []string{"Any", "Today", "This week", "This month", "This year"}, "When the video was published."},
}

// Row indexes, for use with Get and Set.
const (
	RowType = iota
	RowDuration
	RowDate
)

// Options of the Type row.
const (
	TypeAny = iota
	TypeVideos
	TypeShorts
	TypeLive
	TypePlaylists
)

// Get returns the selected option of a row.
func (f Filters) Get(row int) int {
	return [...]int{f.Type, f.Duration, f.Date}[row]
}

// Set selects an option of a row, wrapping around the row's options.
func (f *Filters) Set(row, option int) {
	n := len(Rows[row].Options)
	option = ((option % n) + n) % n
	switch row {
	case RowType:
		f.Type = option
	case RowDuration:
		f.Duration = option
	case RowDate:
		f.Date = option
	}
}

// Applied returns the filters YouTube can apply. It has no upload date filter
// for Shorts: a search for Shorts with a date finds nothing, so the date is
// left out.
func (f Filters) Applied() Filters {
	if f.Type == TypeShorts {
		f.Date = 0
	}
	return f
}

// DateIgnored reports whether the upload date is set but has no effect.
func (f Filters) DateIgnored() bool { return f.Type == TypeShorts }

// Active reports whether any filter differs from the default.
func (f Filters) Active() bool { return f != Filters{} }

// Names lists the active filters, such as ["Live", "This week"].
func (f Filters) Names() []string {
	f = f.Applied()
	var names []string
	for row := range Rows {
		if option := f.Get(row); option != 0 {
			names = append(names, Rows[row].Options[option])
		}
	}
	return names
}

// Summary lists the active filters, such as "Live, This week", or ""
// when there are none.
func (f Filters) Summary() string { return strings.Join(f.Names(), ", ") }

// YouTube's own codes for the options above, indexed like Rows. They are the
// numbers inside the "sp" search parameter, which is a base64 protobuf.
var (
	typeCodes     = [...]byte{0, 1, 9, 1, 3} // any, video, Shorts, live (a video), playlist
	durationCodes = [...]byte{0, 1, 3, 2}    // under 4, 4-20, over 20 minutes
	dateCodes     = [...]byte{0, 2, 3, 4, 5}
)

// Param builds YouTube's "sp" search parameter.
func (f Filters) Param() string {
	f = f.Applied()
	var inner []byte
	if code := dateCodes[f.Date]; code != 0 {
		inner = append(inner, 0x08, code) // field 1: upload date
	}
	typeCode := typeCodes[f.Type]
	if typeCode == 0 && (f.Duration != 0 || f.Date != 0) {
		// These filters only apply to videos. Say so, so that channels and
		// playlists do not mix into the results.
		typeCode = typeCodes[TypeVideos]
	}
	if typeCode != 0 {
		inner = append(inner, 0x10, typeCode) // field 2: type
	}
	if code := durationCodes[f.Duration]; code != 0 {
		inner = append(inner, 0x18, code) // field 3: duration
	}
	if f.Type == TypeLive {
		inner = append(inner, 0x40, 1) // field 8: live
	}
	if len(inner) == 0 {
		return ""
	}
	out := append([]byte{0x12, byte(len(inner))}, inner...) // field 2: the filters
	return base64.StdEncoding.EncodeToString(out)
}

// resultsURL is the youtube.com results page for query with the filters
// applied. The parameter is escaped twice, which is what yt-dlp needs to pass
// it on correctly.
func resultsURL(query string, f Filters) string {
	u := "https://www.youtube.com/results?search_query=" + url.QueryEscape(query)
	if param := f.Param(); param != "" {
		u += "&sp=" + url.QueryEscape(url.QueryEscape(param))
	}
	return u
}
