package tui

// The screen is drawn line by line, and every line must be as wide as the
// terminal thinks it is. A character that the width library counts differently
// from the terminal makes a line wider than the screen: the terminal wraps it,
// every line below lands one row lower, and Bubble Tea, which moves the cursor
// up by a number of lines to repaint, redraws at the wrong rows until the whole
// screen is drawn again. A soft hyphen (U+00AD), common in German text, is
// enough: the library counts 0 cells and tmux counts 1.
//
// These are the characters that lipgloss and tmux 3.7 count differently, found
// by printing each one in tmux and reading where the cursor ended up. They are
// removed from the text the screen shows.
var unsafeWidth = [...][2]rune{
	{0x00AD, 0x00AD}, // soft hyphen
	{0x1160, 0x11FF}, // Hangul jamo that combine with the one before
	{0x2028, 0x2029}, // line and paragraph separators
	{0x2065, 0x2065},
	{0x261D, 0x261D}, // symbols and emoji that terminals draw wide
	{0x2630, 0x2637},
	{0x268A, 0x268F},
	{0x26F9, 0x26F9},
	{0x270C, 0x270D},
	{0x2E3A, 0x2E3B},
	{0x2FFC, 0x2FFF},
	{0x3030, 0x3030},
	{0x303D, 0x303D},
	{0x3164, 0x3164}, // Hangul filler
	{0x31E4, 0x31E5},
	{0x31EF, 0x31EF},
	{0x3248, 0x324F},
	{0x3297, 0x3297},
	{0x3299, 0x3299},
	{0x4DC0, 0x4DFF},
	{0xD7B0, 0xD7C6},
	{0xD7CB, 0xD7FB},
	{0xFA6E, 0xFA6F},
	{0xFADA, 0xFAFF},
	{0xFF9E, 0xFF9F},
	{0xFFA0, 0xFFA0},
	{0xFFF0, 0xFFFB},
	{0x13430, 0x1343F},
	{0x16FF2, 0x16FF6},
	{0x187F8, 0x187FF},
	{0x18CFF, 0x18CFF},
	{0x18D09, 0x18D1E},
	{0x18D80, 0x18DF2},
	{0x1D300, 0x1D356},
	{0x1D360, 0x1D376},
	{0x1F1E6, 0x1F1FF}, // regional indicators, the letters of flags
	{0x1F202, 0x1F202},
	{0x1F237, 0x1F237},
	{0x1F260, 0x1F265},
	{0x1F3CB, 0x1F3CC},
	{0x1F3FB, 0x1F3FF}, // skin tones
	{0x1F574, 0x1F575},
	{0x1F590, 0x1F590},
	{0x1F6D8, 0x1F6D8},
	{0x1FA89, 0x1FA8A},
	{0x1FA8E, 0x1FA8F},
	{0x1FABE, 0x1FABE},
	{0x1FAC6, 0x1FAC6},
	{0x1FAC8, 0x1FAC8},
	{0x1FACD, 0x1FACD},
	{0x1FADC, 0x1FADC},
	{0x1FADF, 0x1FADF},
	{0x1FAE9, 0x1FAEA},
	{0x1FAEF, 0x1FAEF},
}

// widthIsUnsafe reports whether the width of r is not the same for the width
// library and the terminal, so it must not be drawn.
func widthIsUnsafe(r rune) bool {
	if r < 0xAD {
		return false
	}

	for _, span := range unsafeWidth {
		if r < span[0] {
			return false // the table is sorted
		}
		if r <= span[1] {
			return true
		}
	}

	return false
}
