package strutil

import (
	"strings"
	"unicode/utf8"
)

// PadLeft prepends specified padding characters to a string until it reaches a target width, 
// using rune counting for accurate width determination.
// It uses rune counting to correctly handle multi-byte runes for both length checks and padding.
func PadLeft(s string, width int, pad rune) string {
	if width <= 0 { 
		return s // Or handle error, but per scope, return original
	}

	currentRuneLength := utf8.RuneCountInString(s)

	if currentRuneLength >= width {
		return s // Already wide enough
	}

	paddingNeeded := width - currentRuneLength

	// Build the padding string by repeating the rune. Since we are counting runes, 
	// we construct the padding string by appending the rune 'paddingNeeded' times.
	padding := strings.Repeat(string(pad), paddingNeeded)

	return padding + s
}