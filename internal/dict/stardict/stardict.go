// Package stardict reads StarDict dictionaries (.ifo + .idx[.gz] + .dict[.dz]
// and optional .syn).
package stardict

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/aerial337/linglike/internal/dict"
)

func init() {
	dict.Register(".ifo", func(p string) (dict.Dictionary, error) { return Open(p) })
}

type reader interface {
	ReadAt(off int64, size int) ([]byte, error)
	Close() error
}

type plainFile struct{ f *os.File }

func (p plainFile) ReadAt(off int64, size int) ([]byte, error) {
	b := make([]byte, size)
	n, err := p.f.ReadAt(b, off)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return b[:n], nil
}
func (p plainFile) Close() error { return p.f.Close() }

type idxEntry struct {
	off  uint64
	size uint32
}

// Dictionary is a loaded StarDict dictionary.
type Dictionary struct {
	path    string
	name    string
	info    map[string]string
	words   []string
	entries []idxEntry
	syn     []synEntry // synonym -> index into words
	index   *dict.Index
	synIdx  *dict.Index
	data    reader
	seq     string
}

type synEntry struct {
	word string
	ref  int
}

// Open loads a StarDict dictionary given the path to its .ifo file.
func Open(ifoPath string) (*Dictionary, error) {
	base := strings.TrimSuffix(ifoPath, filepath.Ext(ifoPath))
	info, err := readIfo(ifoPath)
	if err != nil {
		return nil, err
	}
	d := &Dictionary{path: ifoPath, info: info, seq: info["sametypesequence"]}
	d.name = info["bookname"]
	if d.name == "" {
		d.name = filepath.Base(base)
	}
	offBits := 32
	if info["idxoffsetbits"] == "64" {
		offBits = 64
	}
	idxBytes, err := readMaybeGz(base+".idx", base+".idx.gz")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(ifoPath), err)
	}
	if err := d.parseIdx(idxBytes, offBits); err != nil {
		return nil, err
	}
	if synBytes, err := readMaybeGz(base+".syn", base+".syn.gz"); err == nil {
		d.parseSyn(synBytes)
	}
	if f, err := os.Open(base + ".dict"); err == nil {
		d.data = plainFile{f}
	} else if dz, err := openDictzip(base + ".dict.dz"); err == nil {
		d.data = dz
	} else {
		return nil, fmt.Errorf("%s: missing .dict or .dict.dz: %w", filepath.Base(ifoPath), err)
	}
	d.index = dict.BuildIndex(d.words)
	if len(d.syn) > 0 {
		sw := make([]string, len(d.syn))
		for i, s := range d.syn {
			sw[i] = s.word
		}
		d.synIdx = dict.BuildIndex(sw)
	}
	return d, nil
}

func readIfo(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info := map[string]string{}
	sc := bufio.NewScanner(f)
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if first {
			first = false
			if !strings.HasPrefix(line, "StarDict's dict ifo file") {
				return nil, errors.New("not a StarDict .ifo file")
			}
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok {
			info[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return info, nil
}

func readMaybeGz(plain, gz string) ([]byte, error) {
	if b, err := os.ReadFile(plain); err == nil {
		return b, nil
	}
	f, err := os.Open(gz)
	if err != nil {
		return nil, fmt.Errorf("missing %s", filepath.Base(plain))
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

func (d *Dictionary) parseIdx(b []byte, offBits int) error {
	be := binary.BigEndian
	for p := 0; p < len(b); {
		z := -1
		for k := p; k < len(b); k++ {
			if b[k] == 0 {
				z = k
				break
			}
		}
		if z < 0 {
			break
		}
		word := string(b[p:z])
		p = z + 1
		var e idxEntry
		if offBits == 64 {
			if p+12 > len(b) {
				return errors.New("truncated .idx")
			}
			e.off = be.Uint64(b[p:])
			e.size = be.Uint32(b[p+8:])
			p += 12
		} else {
			if p+8 > len(b) {
				return errors.New("truncated .idx")
			}
			e.off = uint64(be.Uint32(b[p:]))
			e.size = be.Uint32(b[p+4:])
			p += 8
		}
		d.words = append(d.words, word)
		d.entries = append(d.entries, e)
	}
	return nil
}

func (d *Dictionary) parseSyn(b []byte) {
	for p := 0; p < len(b); {
		z := -1
		for k := p; k < len(b); k++ {
			if b[k] == 0 {
				z = k
				break
			}
		}
		if z < 0 || z+5 > len(b) {
			break
		}
		word := string(b[p:z])
		ref := int(binary.BigEndian.Uint32(b[z+1:]))
		p = z + 5
		if ref >= 0 && ref < len(d.words) {
			d.syn = append(d.syn, synEntry{word, ref})
		}
	}
}

func (d *Dictionary) Name() string            { return d.name }
func (d *Dictionary) Path() string            { return d.path }
func (d *Dictionary) Count() int              { return len(d.words) }
func (d *Dictionary) Close() error            { return d.data.Close() }
func (d *Dictionary) Info() map[string]string { return d.info }

// Entry returns entry i rendered as HTML.
func (d *Dictionary) Entry(i int) (dict.Entry, bool) {
	if i < 0 || i >= len(d.entries) {
		return dict.Entry{}, false
	}
	e := d.entries[i]
	raw, err := d.data.ReadAt(int64(e.off), int(e.size))
	if err != nil {
		return dict.Entry{Word: d.words[i], Body: "<i>error reading entry: " + html.EscapeString(err.Error()) + "</i>", HTML: true}, true
	}
	return dict.Entry{Word: d.words[i], Body: d.render(raw), HTML: true}, true
}

// render decodes the typed fields of a .dict record into HTML.
func (d *Dictionary) render(raw []byte) string {
	var sb strings.Builder
	fields := splitFields(raw, d.seq)
	for _, f := range fields {
		sb.WriteString(renderField(f.typ, f.data))
	}
	return sb.String()
}

type field struct {
	typ  byte
	data []byte
}

func splitFields(raw []byte, seq string) []field {
	var out []field
	if seq != "" {
		p := 0
		for i := 0; i < len(seq); i++ {
			t := seq[i]
			last := i == len(seq)-1
			if p > len(raw) {
				break
			}
			if last {
				out = append(out, field{t, raw[p:]})
				break
			}
			if isBinary(t) {
				if p+4 > len(raw) {
					break
				}
				n := int(binary.BigEndian.Uint32(raw[p:]))
				p += 4
				end := min(p+n, len(raw))
				out = append(out, field{t, raw[p:end]})
				p = end
			} else {
				z := indexZero(raw, p)
				out = append(out, field{t, raw[p:z]})
				p = min(z+1, len(raw))
			}
		}
		return out
	}
	for p := 0; p < len(raw); {
		t := raw[p]
		p++
		if isBinary(t) {
			if p+4 > len(raw) {
				break
			}
			n := int(binary.BigEndian.Uint32(raw[p:]))
			p += 4
			end := min(p+n, len(raw))
			out = append(out, field{t, raw[p:end]})
			p = end
		} else {
			z := indexZero(raw, p)
			out = append(out, field{t, raw[p:z]})
			p = min(z+1, len(raw))
		}
	}
	return out
}

func isBinary(t byte) bool { return t == 'W' || t == 'P' || t == 'X' || t == 'r' }

func indexZero(b []byte, from int) int {
	for i := from; i < len(b); i++ {
		if b[i] == 0 {
			return i
		}
	}
	return len(b)
}

var (
	xdxfKref = regexp.MustCompile(`(?is)<kref[^>]*>(.*?)</kref>`)
	xdxfTags = regexp.MustCompile(`(?i)</?(k|ex|co|abr|tr|dtrn|iref|rref|nu|sr|def|gr|pos|categ|opt|etm|mrkd|blockquote|c|sup|sub|b|i|u|big|small|ar|deftext|exm|ex_orig|ex_tran|sr|c)( [^>]*)?>`)
)

func renderField(t byte, data []byte) string {
	s := strings.ToValidUTF8(string(data), "�")
	switch t {
	case 'h':
		return `<div class="sd-h">` + rewriteLinks(s) + `</div>`
	case 'g':
		// Pango markup: a subset of HTML-like tags.
		return `<div class="sd-g">` + strings.NewReplacer("\n", "<br/>").Replace(rewriteLinks(s)) + `</div>`
	case 'x':
		return `<div class="sd-x">` + renderXdxf(s) + `</div>`
	case 't':
		return `<div class="sd-t">[` + html.EscapeString(s) + `]</div>`
	case 'y':
		return `<div class="sd-y">` + html.EscapeString(s) + `</div>`
	case 'W', 'P', 'X', 'r':
		return ""
	default: // m, l, n, k, w and unknown text types
		return `<div class="sd-m">` + nl2br(html.EscapeString(s)) + `</div>`
	}
}

func nl2br(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\n", "<br/>")
}

func rewriteLinks(s string) string {
	s = strings.ReplaceAll(s, `href="bword://`, `href="entry://`)
	s = strings.ReplaceAll(s, `href='bword://`, `href='entry://`)
	return s
}

func renderXdxf(s string) string {
	s = xdxfKref.ReplaceAllStringFunc(s, func(m string) string {
		sub := xdxfKref.FindStringSubmatch(m)
		w := html.UnescapeString(sub[1])
		return `<a href="entry://` + html.EscapeString(strings.TrimSpace(w)) + `">` + sub[1] + `</a>`
	})
	s = xdxfTags.ReplaceAllStringFunc(s, func(m string) string {
		lower := strings.ToLower(m)
		closing := strings.HasPrefix(lower, "</")
		name := strings.TrimLeft(lower, "</")
		if i := strings.IndexAny(name, " >"); i >= 0 {
			name = name[:i]
		}
		var repl string
		switch name {
		case "k":
			repl = "b"
		case "ex", "ex_orig", "exm":
			repl = "i"
		case "abr", "gr", "pos":
			repl = "i"
		case "sup", "sub", "b", "i", "u", "big", "small", "blockquote":
			repl = name
		case "c":
			if closing {
				return "</span>"
			}
			col := ""
			if i := strings.Index(m, `c="`); i >= 0 {
				rest := m[i+3:]
				if j := strings.Index(rest, `"`); j >= 0 {
					col = rest[:j]
				}
			}
			if col != "" && !strings.ContainsAny(col, `<>"'`) {
				return `<span style="color:` + col + `">`
			}
			return "<span>"
		case "def", "co", "dtrn", "tr", "deftext":
			if closing {
				return "</div>"
			}
			return `<div class="sd-` + name + `">`
		default:
			return ""
		}
		if closing {
			return "</" + repl + ">"
		}
		return "<" + repl + ">"
	})
	return nl2br(s)
}

func (d *Dictionary) Lookup(word string) []dict.Entry {
	var out []dict.Entry
	seen := map[int]bool{}
	for _, id := range d.index.Exact(word) {
		if e, ok := d.Entry(int(id)); ok && !seen[int(id)] {
			seen[int(id)] = true
			out = append(out, e)
		}
	}
	if d.synIdx != nil {
		for _, id := range d.synIdx.Exact(word) {
			ref := d.syn[id].ref
			if seen[ref] {
				continue
			}
			if e, ok := d.Entry(ref); ok {
				seen[ref] = true
				out = append(out, e)
			}
		}
	}
	return out
}

func (d *Dictionary) Prefix(prefix string, limit int) []string {
	ids := d.index.Prefix(prefix, limit)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, d.words[id])
	}
	return out
}

// String describes the dictionary.
func (d *Dictionary) String() string {
	return d.name + " (" + strconv.Itoa(len(d.words)) + " entries)"
}
