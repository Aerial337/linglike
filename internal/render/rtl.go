package render

import (
	"regexp"
	"unicode"
)

var tagStripRe = regexp.MustCompile(`<[^>]*>`)

// isRTLRune reports whether r belongs to a right-to-left script.
func isRTLRune(r rune) bool {
	return unicode.Is(unicode.Arabic, r) || unicode.Is(unicode.Hebrew, r) ||
		unicode.Is(unicode.Syriac, r) || unicode.Is(unicode.Thaana, r) ||
		unicode.Is(unicode.Nko, r) || unicode.Is(unicode.Adlam, r) ||
		(r >= 0xFB1D && r <= 0xFDFF) || (r >= 0xFE70 && r <= 0xFEFF)
}

// IsRTL reports whether text is predominantly written in a right-to-left
// script (Arabic, Persian, Urdu, Hebrew, …). The embedded browser does not
// support dir="auto", so direction is decided here.
func IsRTL(text string) bool {
	rtl, ltr := 0, 0
	for _, r := range text {
		switch {
		case isRTLRune(r):
			rtl++
		case unicode.IsLetter(r):
			ltr++
		}
	}
	return rtl > 0 && rtl >= ltr
}

// IsRTLHTML applies IsRTL to the visible text of an HTML fragment.
func IsRTLHTML(fragment string) bool {
	return IsRTL(tagStripRe.ReplaceAllString(fragment, " "))
}

// dirClass returns the extra class and dir attribute for a block that
// contains text (or nothing when the text is left-to-right).
func dirAttr(text string) string {
	if IsRTL(text) {
		return ` class="rtl" dir="rtl"`
	}
	return ""
}
