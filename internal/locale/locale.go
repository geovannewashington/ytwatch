// Package locale holds the language and region used for searching YouTube.
package locale

import (
	"fmt"
	"regexp"
	"strings"
)

// Locale is the language and region of a search. Empty values mean YouTube's
// own choice, which follows the connection and the browser.
type Locale struct {
	// Language is a code such as "pt", "ja" or "pt-BR". YouTube uses it for
	// the language of titles, descriptions and the ranking of results.
	Language string
	// Region is a two-letter country code such as "BR". YouTube uses the
	// country for what is popular and what is available.
	Region string
}

var (
	languagePattern = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z]{2,4})?$`)
	regionPattern   = regexp.MustCompile(`^[A-Z]{2}$`)
)

// NormalizeLanguage trims a language code and fixes its case: "pt" for the
// language and "BR" or "Hans" for the part after a dash. An empty value is
// allowed and means "not set".
func NormalizeLanguage(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	if i := strings.IndexByte(value, '-'); i > 0 {
		suffix := strings.ToLower(value[i+1:])
		if len(suffix) == 2 {
			suffix = strings.ToUpper(suffix)
		} else if len(suffix) > 0 {
			suffix = strings.ToUpper(suffix[:1]) + suffix[1:]
		}
		value = strings.ToLower(value[:i]) + "-" + suffix
	} else {
		value = strings.ToLower(value)
	}
	if !languagePattern.MatchString(value) {
		return "", fmt.Errorf("invalid language %q (use a code such as pt, ja or pt-BR)", value)
	}
	return value, nil
}

// NormalizeRegion trims and upper-cases a country code. An empty value is
// allowed and means "not set".
func NormalizeRegion(value string) (string, error) {
	value = strings.ToUpper(strings.TrimSpace(value))
	if value == "" {
		return "", nil
	}
	if !regionPattern.MatchString(value) {
		return "", fmt.Errorf("invalid region %q (use a two-letter country code such as BR or JP)", value)
	}
	return value, nil
}

// YtDlpArgs returns the yt-dlp options for the locale. The language goes to
// YouTube's extractor. The region is sent as a fake X-Forwarded-For address
// from that country, which is how yt-dlp does it; YouTube may ignore it.
func (l Locale) YtDlpArgs() []string {
	var args []string
	if l.Language != "" {
		args = append(args, "--extractor-args", "youtube:lang="+l.Language)
	}
	if l.Region != "" {
		args = append(args, "--xff", l.Region)
	}
	return args
}

// PageParams returns the query parameters (hl and gl) for a youtube.com page.
func (l Locale) PageParams() string {
	var parts []string
	if l.Language != "" {
		parts = append(parts, "hl="+l.Language)
	}
	if l.Region != "" {
		parts = append(parts, "gl="+l.Region)
	}
	return strings.Join(parts, "&")
}

// AcceptLanguage returns the value of the Accept-Language header.
func (l Locale) AcceptLanguage() string {
	if l.Language == "" {
		return "en-US,en;q=0.9"
	}
	return l.Language + ",en;q=0.5"
}
