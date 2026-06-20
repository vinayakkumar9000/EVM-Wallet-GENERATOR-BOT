package core

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/text/width"
)

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// StripANSI removes ANSI escape sequences from s.
func StripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

// DisplayWidth returns the terminal display width of s (ANSI codes ignored).
func DisplayWidth(s string) int {
	plain := StripANSI(s)
	w := 0
	for _, r := range plain {
		switch width.LookupRune(r).Kind() {
		case width.EastAsianWide, width.EastAsianFullwidth, width.EastAsianAmbiguous:
			w += 2
		default:
			w += 1
		}
	}
	return w
}

// BoxTop prints the top border of a box with the given inner width.
func BoxTop(innerWidth int) string {
	return "  ╔" + strings.Repeat("═", innerWidth) + "╗"
}

// BoxBottom prints the bottom border of a box with the given inner width.
func BoxBottom(innerWidth int) string {
	return "  ╚" + strings.Repeat("═", innerWidth) + "╝"
}

// BoxRow formats a box row with content aligned inside innerWidth columns.
// align: -1 left, 0 center, 1 right.
func BoxRow(innerWidth int, content string, align int) string {
	dw := DisplayWidth(content)
	pad := innerWidth - dw
	if pad < 0 {
		pad = 0
	}

	var left, right int
	switch {
	case align < 0:
		left, right = 0, pad
	case align > 0:
		left, right = pad, 0
	default:
		left = pad / 2
		right = pad - left
	}

	return fmt.Sprintf("  ║%s%s%s║", strings.Repeat(" ", left), content, strings.Repeat(" ", right))
}
