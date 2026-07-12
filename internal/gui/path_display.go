package gui

import (
	"path/filepath"
	"strconv"
	"unicode"
	"unicode/utf8"
)

const (
	leftToRightIsolate    = "\u2066"
	popDirectionalIsolate = "\u2069"
)

// displayFilesystemPath produces a presentation-only representation of a path.
// It never changes the path used for filesystem operations. Directional
// isolation prevents surrounding UI text from changing the visual order, while
// quoting makes control and Unicode format characters (including bidi
// overrides) explicit and distinguishable from literal text.
func displayFilesystemPath(path string) string {
	return displayUntrustedText(path)
}

func displayUntrustedText(value string) string {
	rendered := value
	if pathNeedsDisplayEscaping(value) {
		rendered = strconv.QuoteToGraphic(value)
	}
	return leftToRightIsolate + rendered + popDirectionalIsolate
}

func displayFilesystemName(path string) string {
	return displayFilesystemPath(filepath.Base(path))
}

func pathNeedsDisplayEscaping(value string) bool {
	if !utf8.ValidString(value) {
		return true
	}
	for _, r := range value {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			return true
		}
	}
	return false
}
