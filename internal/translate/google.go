// Package translate provides machine translation of text through Google
// Translate's public web endpoints (no API key required).
package translate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Result is a translation result.
type Result struct {
	Text         string
	SourceLang   string
	Alternatives []Alternative
	// Transliteration of the source text, when available.
	SourceTranslit string
}

// Alternative is a dictionary-style alternative translation (per part of
// speech) as shown under Google Translate's result.
type Alternative struct {
	PartOfSpeech string
	Terms        []string
}

// Client translates text. The zero value is usable.
type Client struct {
	HTTP *http.Client
}

// DefaultClient is used by the package level Google function.
var DefaultClient = &Client{}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"

// Google translates text from source (or "auto") to target using the
// default client.
func Google(text, source, target string) (*Result, error) {
	return DefaultClient.Translate(context.Background(), text, source, target)
}

// Translate translates text. It tries the full endpoint first (which also
// returns dictionary alternatives) and falls back to the simpler one.
func (c *Client) Translate(ctx context.Context, text, source, target string) (*Result, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("nothing to translate")
	}
	if source == "" {
		source = "auto"
	}
	if target == "" {
		target = "en"
	}
	res, err := c.translateFull(ctx, text, source, target)
	if err == nil {
		return res, nil
	}
	res2, err2 := c.translateSimple(ctx, text, source, target)
	if err2 == nil {
		return res2, nil
	}
	return nil, fmt.Errorf("google translate: %v; fallback: %v", err, err2)
}

func (c *Client) get(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return body, nil
}

func (c *Client) translateFull(ctx context.Context, text, source, target string) (*Result, error) {
	q := url.Values{}
	q.Set("client", "gtx")
	q.Set("sl", source)
	q.Set("tl", target)
	q.Set("hl", target)
	q.Set("ie", "UTF-8")
	q.Set("oe", "UTF-8")
	q.Set("q", text)
	u := "https://translate.googleapis.com/translate_a/single?" + q.Encode() + "&dt=t&dt=bd&dt=rm"
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	var root []any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("unexpected response: %w", err)
	}
	if len(root) == 0 {
		return nil, errors.New("empty response")
	}
	res := &Result{}
	// root[0]: list of sentences [translated, original, ...]; the last item
	// may hold transliterations at index 2/3.
	if sentences, ok := root[0].([]any); ok {
		var sb strings.Builder
		for _, s := range sentences {
			seg, ok := s.([]any)
			if !ok || len(seg) == 0 {
				continue
			}
			if t, ok := seg[0].(string); ok {
				sb.WriteString(t)
			} else if len(seg) > 3 {
				if tr, ok := seg[3].(string); ok {
					res.SourceTranslit = tr
				}
			}
		}
		res.Text = sb.String()
	}
	// root[1]: dictionary [[pos, [terms...], [[term, [reverse...], ...]...], ...], ...]
	if len(root) > 1 {
		if dictList, ok := root[1].([]any); ok {
			for _, d := range dictList {
				item, ok := d.([]any)
				if !ok || len(item) < 2 {
					continue
				}
				alt := Alternative{}
				alt.PartOfSpeech, _ = item[0].(string)
				if terms, ok := item[1].([]any); ok {
					for _, t := range terms {
						if s, ok := t.(string); ok {
							alt.Terms = append(alt.Terms, s)
						}
					}
				}
				if len(alt.Terms) > 0 {
					res.Alternatives = append(res.Alternatives, alt)
				}
			}
		}
	}
	// root[2]: detected source language.
	if len(root) > 2 {
		res.SourceLang, _ = root[2].(string)
	}
	if res.Text == "" {
		return nil, errors.New("no translation in response")
	}
	return res, nil
}

func (c *Client) translateSimple(ctx context.Context, text, source, target string) (*Result, error) {
	q := url.Values{}
	q.Set("client", "dict-chrome-ex")
	q.Set("sl", source)
	q.Set("tl", target)
	q.Set("q", text)
	u := "https://clients5.google.com/translate_a/t?" + q.Encode()
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("unexpected response: %w", err)
	}
	res := &Result{}
	// Formats seen: ["text"], [["text","lang"]], {"sentences":[{"trans":..}],"src":..}
	switch v := root.(type) {
	case []any:
		if len(v) == 0 {
			return nil, errors.New("empty response")
		}
		switch first := v[0].(type) {
		case string:
			res.Text = first
		case []any:
			if len(first) > 0 {
				res.Text, _ = first[0].(string)
			}
			if len(first) > 1 {
				res.SourceLang, _ = first[1].(string)
			}
		}
	case map[string]any:
		if sents, ok := v["sentences"].([]any); ok {
			var sb strings.Builder
			for _, s := range sents {
				if m, ok := s.(map[string]any); ok {
					if t, ok := m["trans"].(string); ok {
						sb.WriteString(t)
					}
				}
			}
			res.Text = sb.String()
		}
		res.SourceLang, _ = v["src"].(string)
	}
	if res.Text == "" {
		return nil, errors.New("no translation in response")
	}
	return res, nil
}

// Language is a language selectable as a translation target.
type Language struct {
	Code string
	Name string
}

// Languages lists the languages supported by Google Translate, sorted by name.
func Languages() []Language {
	out := make([]Language, 0, len(languageNames))
	for code, name := range languageNames {
		out = append(out, Language{code, name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// LanguageName returns the display name of a language code.
func LanguageName(code string) string {
	if n, ok := languageNames[strings.ToLower(code)]; ok {
		return n
	}
	return code
}

var languageNames = map[string]string{
	"af": "Afrikaans", "sq": "Albanian", "am": "Amharic", "ar": "Arabic", "hy": "Armenian",
	"az": "Azerbaijani", "eu": "Basque", "be": "Belarusian", "bn": "Bengali", "bs": "Bosnian",
	"bg": "Bulgarian", "ca": "Catalan", "ceb": "Cebuano", "zh-CN": "Chinese (Simplified)",
	"zh-TW": "Chinese (Traditional)", "co": "Corsican", "hr": "Croatian", "cs": "Czech",
	"da": "Danish", "nl": "Dutch", "en": "English", "eo": "Esperanto", "et": "Estonian",
	"fi": "Finnish", "fr": "French", "fy": "Frisian", "gl": "Galician", "ka": "Georgian",
	"de": "German", "el": "Greek", "gu": "Gujarati", "ht": "Haitian Creole", "ha": "Hausa",
	"haw": "Hawaiian", "he": "Hebrew", "hi": "Hindi", "hmn": "Hmong", "hu": "Hungarian",
	"is": "Icelandic", "ig": "Igbo", "id": "Indonesian", "ga": "Irish", "it": "Italian",
	"ja": "Japanese", "jv": "Javanese", "kn": "Kannada", "kk": "Kazakh", "km": "Khmer",
	"rw": "Kinyarwanda", "ko": "Korean", "ku": "Kurdish", "ky": "Kyrgyz", "lo": "Lao",
	"la": "Latin", "lv": "Latvian", "lt": "Lithuanian", "lb": "Luxembourgish",
	"mk": "Macedonian", "mg": "Malagasy", "ms": "Malay", "ml": "Malayalam", "mt": "Maltese",
	"mi": "Maori", "mr": "Marathi", "mn": "Mongolian", "my": "Myanmar (Burmese)", "ne": "Nepali",
	"no": "Norwegian", "ny": "Nyanja (Chichewa)", "or": "Odia (Oriya)", "ps": "Pashto",
	"fa": "Persian", "pl": "Polish", "pt": "Portuguese", "pa": "Punjabi", "ro": "Romanian",
	"ru": "Russian", "sm": "Samoan", "gd": "Scots Gaelic", "sr": "Serbian", "st": "Sesotho",
	"sn": "Shona", "sd": "Sindhi", "si": "Sinhala", "sk": "Slovak", "sl": "Slovenian",
	"so": "Somali", "es": "Spanish", "su": "Sundanese", "sw": "Swahili", "sv": "Swedish",
	"tl": "Tagalog (Filipino)", "tg": "Tajik", "ta": "Tamil", "tt": "Tatar", "te": "Telugu",
	"th": "Thai", "tr": "Turkish", "tk": "Turkmen", "uk": "Ukrainian", "ur": "Urdu",
	"ug": "Uyghur", "uz": "Uzbek", "vi": "Vietnamese", "cy": "Welsh", "xh": "Xhosa",
	"yi": "Yiddish", "yo": "Yoruba", "zu": "Zulu",
}
