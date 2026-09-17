package ld2

import (
	"html"
	"net/url"
	"regexp"
	"strings"
)

// Lingoes definition markup is a small XML vocabulary. Observed tags:
//
//	C        whole entry            F   one sense group (homograph)
//	E        inflections "a|b"      H   head (M pronunciation, L headword)
//	I        body                   N   sense block (numbered definitions)
//	P/U      part of speech         Q   definition; several Q in one N are numbered
//	X        translation of a Q     R   paragraph/row
//	J D=".." labelled section       K   raw HTML inside <![CDATA[ ]]>
//	a/Z/Y    links (synonym / antonym / cross reference)
//	x K=col  coloured text, g label inside it     n   line break
//	h g      emphasis               m/l subscript / superscript
//	v        phonetic               Ô P="css"  styled span     y  group
//
// Raw HTML in K sections uses links of the form
// dict://key.[$DictID]/word which are rewritten to entry://word.

var (
	dictLinkRe = regexp.MustCompile(`(?i)dict://key\.\[\$DictID\]/`)
	attrRe     = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)\s*=\s*"([^"]*)"`)
)

// EntryLink builds an entry:// URL for a headword.
func EntryLink(word string) string {
	return "entry://" + url.PathEscape(strings.TrimSpace(word))
}

// ToHTML converts the definition markup of one entry (possibly several
// referenced definitions) into an HTML fragment.
func ToHTML(parts []string) string {
	var sb strings.Builder
	for i, p := range parts {
		if i > 0 {
			sb.WriteString(`<div class="ld2-sep"></div>`)
		}
		convert(&sb, p)
	}
	return sb.String()
}

type tag struct {
	name    string
	attrs   map[string]string
	closing bool
	self    bool
}

func parseTag(s string) tag {
	// s is the text between '<' and '>'
	t := tag{attrs: map[string]string{}}
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "/") {
		t.closing = true
		s = strings.TrimSpace(s[1:])
	}
	if strings.HasSuffix(s, "/") {
		t.self = true
		s = strings.TrimSpace(s[:len(s)-1])
	}
	sp := strings.IndexAny(s, " \t\r\n")
	if sp < 0 {
		t.name = s
		return t
	}
	t.name = s[:sp]
	for _, m := range attrRe.FindAllStringSubmatch(s[sp:], -1) {
		t.attrs[m[1]] = m[2]
	}
	return t
}

// linkText extracts the plain text between the current position and the
// matching close tag, used to build entry:// links for a/Z/Y tags.
func linkText(src string, name string) (text string, end int) {
	close := "</" + name
	i := strings.Index(src, close)
	if i < 0 {
		return "", -1
	}
	j := strings.Index(src[i:], ">")
	if j < 0 {
		return "", -1
	}
	return src[:i], i + j + 1
}

func convert(sb *strings.Builder, src string) {
	var stack []string // html close tags to emit on matching Lingoes close tag
	push := func(open, close string) {
		sb.WriteString(open)
		stack = append(stack, close)
	}
	pop := func() {
		if n := len(stack); n > 0 {
			sb.WriteString(stack[n-1])
			stack = stack[:n-1]
		}
	}

	// qCount counts <Q> children of the <N> starting at src[i:]
	qCount := func(rest string) int {
		end := strings.Index(rest, "</N")
		if end < 0 {
			end = len(rest)
		}
		return strings.Count(rest[:end], "<Q>") + strings.Count(rest[:end], "<Q ")
	}
	var nList []*senseState // one per open N

	i := 0
	for i < len(src) {
		c := src[i]
		if c != '<' {
			j := strings.IndexByte(src[i:], '<')
			if j < 0 {
				j = len(src) - i
			}
			writeText(sb, src[i:i+j])
			i += j
			continue
		}
		if strings.HasPrefix(src[i:], "<![CDATA[") {
			end := strings.Index(src[i:], "]]>")
			if end < 0 {
				end = len(src) - i
				sb.WriteString(rewriteHTML(src[i+9:]))
				i = len(src)
				continue
			}
			sb.WriteString(rewriteHTML(src[i+9 : i+end]))
			i += end + 3
			continue
		}
		end := strings.IndexByte(src[i:], '>')
		if end < 0 {
			writeText(sb, src[i:])
			break
		}
		t := parseTag(src[i+1 : i+end])
		i += end + 1
		if t.closing {
			switch t.name {
			case "N":
				if n := len(nList); n > 0 {
					if nList[n-1].opened {
						sb.WriteString("</ol>")
					}
					nList = nList[:n-1]
				}
			}
			pop()
			continue
		}
		switch t.name {
		case "n", "br":
			sb.WriteString("<br/>")
			continue
		case "hr":
			sb.WriteString("<hr/>")
			continue
		}
		if t.self {
			continue
		}
		switch t.name {
		case "C":
			push(`<div class="ld2">`, "</div>")
		case "F":
			push(`<div class="ld2-f">`, "</div>")
		case "E":
			text, e := linkText(src[i:], "E")
			if e >= 0 {
				forms := strings.Split(strings.TrimSpace(text), "|")
				sb.WriteString(`<div class="ld2-forms">`)
				for k, f := range forms {
					if k > 0 {
						sb.WriteString(", ")
					}
					sb.WriteString(`<a href="` + EntryLink(f) + `">` + html.EscapeString(f) + `</a>`)
				}
				sb.WriteString("</div>")
				i += e
			} else {
				push(`<div class="ld2-forms">`, "</div>")
			}
		case "H":
			push(`<div class="ld2-head">`, "</div>")
		case "M", "v":
			push(`<span class="ld2-pron">[`, "]</span>")
		case "L":
			push(`<span class="ld2-hw">`, "</span>")
		case "I":
			push(`<div class="ld2-body">`, "</div>")
		case "N":
			nList = append(nList, &senseState{ol: qCount(src[i:]) > 1})
			push(`<div class="ld2-sense">`, "</div>")
		case "P":
			push(`<span class="ld2-pos">`, "</span> ")
		case "U":
			push("<i>", "</i>")
		case "Q":
			if n := len(nList); n > 0 && nList[n-1].ol {
				if !nList[n-1].opened {
					sb.WriteString(`<ol class="ld2-defs">`)
					nList[n-1].opened = true
				}
				push("<li>", "</li>")
			} else {
				push(`<div class="ld2-def">`, "</div>")
			}
		case "X":
			push(`<div class="ld2-trans">`, "</div>")
		case "R":
			push(`<div class="ld2-row">`, "</div>")
		case "J":
			label := t.attrs["D"]
			if label != "" {
				sb.WriteString(`<div class="ld2-label">` + html.EscapeString(label) + `</div>`)
			}
			push(`<div class="ld2-section">`, "</div>")
		case "K":
			push(`<div class="ld2-html">`, "</div>")
		case "a", "Z", "Y":
			text, e := linkText(src[i:], t.name)
			if e >= 0 {
				plain := stripTags(text)
				class := "ld2-syn"
				if t.name == "Z" {
					class = "ld2-ant"
				}
				sb.WriteString(`<a class="` + class + `" href="` + EntryLink(plain) + `">`)
				convert(sb, text)
				sb.WriteString("</a>")
				i += e
			} else {
				push("<span>", "</span>")
			}
		case "x":
			col := t.attrs["K"]
			if col != "" && isColor(col) {
				push(`<span class="ld2-x" style="color:`+col+`">`, "</span>")
			} else {
				push(`<span class="ld2-x">`, "</span>")
			}
		case "g":
			push("<b>", "</b>")
		case "h":
			push(`<span class="ld2-hl">`, "</span>")
		case "m":
			push("<sub>", "</sub>")
		case "l":
			push("<sup>", "</sup>")
		case "y":
			push(`<div class="ld2-group">`, "</div>")
		case "Ô", "Û":
			style := t.attrs["P"]
			if style != "" && isStyle(style) {
				push(`<span style="`+html.EscapeString(style)+`">`, "</span>")
			} else {
				push("<span>", "</span>")
			}
		default:
			// Unknown Lingoes tag or plain HTML inside markup: keep the HTML
			// tags that are harmless, drop the rest.
			lower := strings.ToLower(t.name)
			switch lower {
			case "b", "i", "u", "em", "strong", "sub", "sup", "span", "div", "p",
				"table", "tr", "td", "th", "tbody", "thead", "ul", "ol", "li",
				"font", "center", "small", "big", "tt", "pre", "blockquote":
				push("<"+lower+">", "</"+lower+">")
			default:
				push("<span>", "</span>")
			}
		}
	}
	for len(stack) > 0 {
		pop()
	}
}

type senseState struct {
	ol     bool // several Q children: render as numbered list
	opened bool // the <ol> has been written
}

func writeText(sb *strings.Builder, s string) {
	// Text is XML escaped already (&amp; etc.). Replace control characters
	// used by Lingoes as soft separators and tabs.
	s = strings.NewReplacer("", " ", "", " ", "\t", "&nbsp;&nbsp;").Replace(s)
	// Lingoes synonym dictionaries mark sections with [?s] / [?a].
	s = strings.ReplaceAll(s, "[?s]", `<span class="ld2-label">Synonyms</span>`)
	s = strings.ReplaceAll(s, "[?a]", `<span class="ld2-label">Antonyms</span>`)
	sb.WriteString(s)
}

// rewriteHTML prepares raw HTML from a CDATA section for display.
func rewriteHTML(s string) string {
	s = dictLinkRe.ReplaceAllString(s, "entry://")
	s = strings.ReplaceAll(s, "", " ")
	s = strings.ReplaceAll(s, "", " ")
	return s
}

var tagRe = regexp.MustCompile(`<[^>]*>`)

func stripTags(s string) string {
	return html.UnescapeString(strings.TrimSpace(tagRe.ReplaceAllString(s, "")))
}

func isColor(s string) bool {
	if len(s) != 7 && len(s) != 4 || s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func isStyle(s string) bool {
	return !strings.ContainsAny(s, `<>"'`) && !strings.Contains(strings.ToLower(s), "expression")
}
