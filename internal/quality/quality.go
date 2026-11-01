// Package quality holds the video quality levels offered for playing and
// downloading.
package quality

import "strings"

// Best is the default: no limit, so mpv and yt-dlp pick the best they can.
const Best = "best"

// Levels are the accepted qualities, from the highest. A level is the largest
// video height wanted: "720p" means the best video up to 720 pixels high.
var Levels = []string{Best, "1080p", "720p", "480p", "360p"}

// Valid reports whether name is one of Levels.
func Valid(name string) bool {
	for _, level := range Levels {
		if level == name {
			return true
		}
	}

	return false
}

// Normalize lower-cases name and gives Best for an empty one.
func Normalize(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return Best
	}
	return name
}

// Format returns the yt-dlp format for a quality, or "" for Best, which needs
// none. If no stream is within the limit, the best one is used instead, so a
// video that is only available in a lower or higher quality still plays.
// The same string works for mpv's --ytdl-format and yt-dlp's -f.
func Format(name string) string {
	height, ok := strings.CutSuffix(Normalize(name), "p")
	if !ok || !Valid(Normalize(name)) {
		return ""
	}

	limit := "[height<=" + height + "]"
	return "bestvideo" + limit + "+bestaudio/best" + limit + "/best"
}
