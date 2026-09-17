// Package render builds the HTML pages shown in the result panes, styled
// after Lingoes: one collapsible section per dictionary with a coloured
// header bar, plus a section for machine translation.
package render

import (
	"fmt"
	"html"
	"strings"

	"github.com/aerial337/linglike/internal/dict"
	"github.com/aerial337/linglike/internal/translate"
)

// Section is one block of the result page.
type Section struct {
	ID    string
	Title string
	Kind  string // "dict", "translate", "info"
	Body  string // HTML
}

// Page describes a complete result page.
type Page struct {
	Query    string
	Sections []Section
	// Compact selects the smaller popup style.
	Compact bool
}

// CSS is the stylesheet shared by all pages.
const CSS = `
* { box-sizing: border-box; }
html, body { margin: 0; padding: 0; background: #fff; }
body { font-family: "Segoe UI", Tahoma, Arial, sans-serif; font-size: 13px; color: #222; padding: 6px 8px 10px 8px; line-height: 1.45; }
body.compact { font-size: 12px; padding: 4px 6px 6px 6px; }
a { color: #0645ad; text-decoration: none; }
a:hover { text-decoration: underline; }
.sec { margin: 0 0 8px 0; border: 1px solid #c8d4e6; border-radius: 3px; background: #fff; }
.sec-h { display: block; padding: 3px 6px; background: #dfe8f6; background: linear-gradient(#edf3fb, #d3dff0); border-bottom: 1px solid #c8d4e6; color: #1c3d7a; font-weight: bold; cursor: pointer; }
.sec-h .flag { display: inline-block; width: 14px; height: 10px; margin-right: 6px; vertical-align: -1px; background: #4a7bd0; border: 1px solid #2c58a8; }
.sec-h .flag.tr { background: #e0a030; border-color: #b57d16; }
.sec-h .flag.info { background: #9aa; border-color: #778; }
.sec-h .arrow { float: right; color: #6a7f9f; font-weight: normal; font-size: 11px; }
.sec-b { padding: 4px 8px 6px 8px; overflow-x: auto; }
.sec.closed .sec-b { display: none; }
.hw { font-size: 15px; font-weight: bold; color: #000; margin: 4px 0 2px 0; }
.hw .ld2-pron, .ld2-pron { color: #7a3b00; font-weight: normal; font-size: 12px; margin-left: 6px; }
.entry { margin: 0 0 8px 0; }
.entry + .entry { border-top: 1px dotted #b8c4d8; padding-top: 6px; }
.ld2-pos { color: #b32d00; font-style: italic; }
.ld2-defs { margin: 2px 0 2px 22px; padding: 0; }
.ld2-defs li { margin: 1px 0; }
.ld2-def { margin: 1px 0; }
.ld2-trans { color: #555; margin-left: 12px; }
.ld2-forms { color: #666; font-size: 12px; margin: 2px 0; }
.ld2-forms:before { content: "Forms: "; }
.ld2-label { display: inline-block; color: #1c3d7a; font-weight: bold; margin: 4px 8px 2px 0; }
.ld2-syn { margin-right: 10px; }
.ld2-ant { color: #a0143c; margin-right: 10px; }
.ld2-sense { margin: 2px 0; }
.ld2-sep { border-top: 1px dotted #b8c4d8; margin: 6px 0; }
.ld2-hl { background: #fff4c2; }
.ld2-row { margin: 2px 0; }
.ld2-x { font-weight: bold; }
.ld2-hw { font-weight: bold; }
.ld2-head { margin: 1px 0; }
.sd-t { color: #7a3b00; }
.sd-m { white-space: normal; }
.tr-text { font-size: 14px; margin: 4px 0 6px 0; }
.rtl { direction: rtl; text-align: right; unicode-bidi: embed; font-family: "Segoe UI", Tahoma, "Noto Naskh Arabic", Arial, sans-serif; }
.rtl.tr-text { font-size: 15px; line-height: 1.7; }
.rtl .tr-alt .pos { margin-right: 0; margin-left: 6px; }
.rtl ol, .rtl ul { padding-right: 22px; padding-left: 0; }
.rtl .ld2-defs { margin: 2px 22px 2px 0; }
.tr-meta { color: #777; font-size: 11px; }
.tr-alt { margin: 2px 0; }
.tr-alt .pos { color: #b32d00; font-style: italic; margin-right: 6px; }
.info { color: #666; }
.err { color: #a00; }
.nores { color: #666; padding: 10px; }
table { border-collapse: collapse; }
td, th { padding: 2px 4px; vertical-align: top; }
img { max-width: 100%; }
`

const script = `
function tog(id){var e=document.getElementById(id);if(!e)return;if(e.className.indexOf('closed')>=0){e.className=e.className.replace(' closed','');}else{e.className+=' closed';}}
`

// HTML renders the page as a complete HTML document.
func HTML(p *Page) string {
	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html>\n<html><head><meta http-equiv=\"X-UA-Compatible\" content=\"IE=edge\"/>\n<meta charset=\"utf-8\"/>\n<title>")
	sb.WriteString(html.EscapeString(p.Query))
	sb.WriteString("</title>\n<style>")
	sb.WriteString(CSS)
	sb.WriteString("</style>\n<script>")
	sb.WriteString(script)
	sb.WriteString("</script></head>\n<body")
	if p.Compact {
		sb.WriteString(` class="compact"`)
	}
	sb.WriteString(">\n")
	if len(p.Sections) == 0 {
		sb.WriteString(`<div class="nores">No results for <b` + dirAttr(p.Query) + `>` + html.EscapeString(p.Query) + `</b>.</div>`)
	}
	for _, s := range p.Sections {
		flag := "flag"
		if s.Kind == "translate" {
			flag += " tr"
		} else if s.Kind == "info" {
			flag += " info"
		}
		sb.WriteString(`<div class="sec" id="` + s.ID + `"><a class="sec-h" href="javascript:tog('` + s.ID + `')" onclick="tog('` + s.ID + `');return false;"><span class="` + flag + `"></span>`)
		sb.WriteString(html.EscapeString(s.Title))
		sb.WriteString(`<span class="arrow">&#9660;</span></a><div class="sec-b">`)
		sb.WriteString(s.Body)
		sb.WriteString("</div></div>\n")
	}
	sb.WriteString("</body></html>")
	return sb.String()
}

// SectionID returns the element id for the i-th section.
func SectionID(i int) string { return fmt.Sprintf("sec%d", i) }

// DictSection renders a dictionary result.
func DictSection(i int, r dict.Result) Section {
	var sb strings.Builder
	for _, e := range r.Entries {
		sb.WriteString(`<div class="entry"><div class="hw"` + dirAttr(e.Word) + `>`)
		sb.WriteString(html.EscapeString(e.Word))
		sb.WriteString("</div>")
		if e.HTML {
			if IsRTLHTML(e.Body) {
				sb.WriteString(`<div class="rtl" dir="rtl">` + e.Body + `</div>`)
			} else {
				sb.WriteString(e.Body)
			}
		} else {
			body := strings.ReplaceAll(html.EscapeString(e.Body), "\n", "<br/>")
			if IsRTL(e.Body) {
				sb.WriteString(`<div class="sd-m rtl" dir="rtl">` + body + "</div>")
			} else {
				sb.WriteString(`<div class="sd-m">` + body + "</div>")
			}
		}
		sb.WriteString("</div>")
	}
	return Section{ID: SectionID(i), Title: r.Dict.Name(), Kind: "dict", Body: sb.String()}
}

// TranslateSection renders a machine translation result (or its error).
func TranslateSection(i int, target string, res *translate.Result, err error) Section {
	var sb strings.Builder
	title := "Google Translate"
	if err != nil {
		sb.WriteString(`<div class="err">` + html.EscapeString(err.Error()) + `</div>`)
	} else if res != nil {
		sb.WriteString(trTextDiv(res.Text))
		meta := ""
		if res.SourceLang != "" {
			meta = translate.LanguageName(res.SourceLang) + " → " + translate.LanguageName(target)
		}
		if res.SourceTranslit != "" {
			meta += "  ·  " + res.SourceTranslit
		}
		if meta != "" {
			sb.WriteString(`<div class="tr-meta">` + html.EscapeString(meta) + `</div>`)
		}
		for _, a := range res.Alternatives {
			altText := strings.Join(a.Terms, " ")
			if IsRTL(altText) {
				sb.WriteString(`<div class="tr-alt rtl" dir="rtl"><span class="pos">` + html.EscapeString(a.PartOfSpeech) + `</span>`)
			} else {
				sb.WriteString(`<div class="tr-alt"><span class="pos">` + html.EscapeString(a.PartOfSpeech) + `</span>`)
			}
			for k, t := range a.Terms {
				if k > 0 {
					sb.WriteString(", ")
				}
				sb.WriteString(`<a href="entry://` + html.EscapeString(t) + `">` + html.EscapeString(t) + `</a>`)
			}
			sb.WriteString("</div>")
		}
	} else {
		sb.WriteString(`<div class="info">Translating…</div>`)
	}
	return Section{ID: SectionID(i), Title: title, Kind: "translate", Body: sb.String()}
}

// trTextDiv renders translated text with the right writing direction.
func trTextDiv(text string) string {
	body := strings.ReplaceAll(html.EscapeString(text), "\n", "<br/>")
	if IsRTL(text) {
		return `<div class="tr-text rtl" dir="rtl">` + body + `</div>`
	}
	return `<div class="tr-text">` + body + `</div>`
}

// LLMSection renders a local LLM translation result (or its error).
func LLMSection(i int, title, target string, res *translate.Result, err error) Section {
	var sb strings.Builder
	if err != nil {
		sb.WriteString(`<div class="err">` + html.EscapeString(err.Error()) + `</div>`)
	} else if res != nil {
		sb.WriteString(trTextDiv(res.Text))
		sb.WriteString(`<div class="tr-meta">` + html.EscapeString("→ "+translate.LanguageName(target)) + `</div>`)
	} else {
		sb.WriteString(`<div class="info">Asking the model…</div>`)
	}
	return Section{ID: SectionID(i), Title: title, Kind: "translate", Body: sb.String()}
}

// InfoSection renders a plain informational message.
func InfoSection(i int, title, msg string) Section {
	return Section{ID: SectionID(i), Title: title, Kind: "info", Body: `<div class="info">` + html.EscapeString(msg) + `</div>`}
}

// SuggestionsSection renders "did you mean" links.
func SuggestionsSection(i int, words []string) Section {
	var sb strings.Builder
	sb.WriteString(`<div class="info">Did you mean: `)
	for k, w := range words {
		if k > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(`<a href="entry://` + html.EscapeString(w) + `">` + html.EscapeString(w) + `</a>`)
	}
	sb.WriteString("</div>")
	return Section{ID: SectionID(i), Title: "Suggestions", Kind: "info", Body: sb.String()}
}
