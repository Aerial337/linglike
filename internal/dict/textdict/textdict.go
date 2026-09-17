// Package textdict reads simple tab separated dictionaries: one entry per
// line, "headword<TAB>definition". A literal "\n" in the definition is a
// line break; definitions may also contain HTML. Lines starting with '#'
// are comments. Files may be .txt or .tsv, UTF-8 (BOM optional).
package textdict

import (
	"bufio"
	"html"
	"os"
	"path/filepath"
	"strings"

	"github.com/aerial337/linglike/internal/dict"
)

func init() {
	dict.Register(".txt", func(p string) (dict.Dictionary, error) { return Open(p) })
	dict.Register(".tsv", func(p string) (dict.Dictionary, error) { return Open(p) })
}

// Dictionary is a loaded text dictionary.
type Dictionary struct {
	path  string
	name  string
	words []string
	defs  []string
	index *dict.Index
}

// Open loads a text dictionary.
func Open(path string) (*Dictionary, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	d := &Dictionary{path: path, name: strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			line = strings.TrimPrefix(line, "\uFEFF")
			first = false
		}
		if strings.HasPrefix(line, "#") {
			if strings.HasPrefix(line, "#name:") {
				d.name = strings.TrimSpace(line[6:])
			}
			continue
		}
		w, def, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		w = strings.TrimSpace(w)
		if w == "" {
			continue
		}
		d.words = append(d.words, w)
		d.defs = append(d.defs, def)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	d.index = dict.BuildIndex(d.words)
	return d, nil
}

func (d *Dictionary) Name() string { return d.name }
func (d *Dictionary) Path() string { return d.path }
func (d *Dictionary) Count() int   { return len(d.words) }
func (d *Dictionary) Close() error { return nil }

func (d *Dictionary) Lookup(word string) []dict.Entry {
	var out []dict.Entry
	for _, id := range d.index.Exact(word) {
		def := d.defs[id]
		body := def
		if !strings.Contains(def, "<") {
			body = html.EscapeString(def)
		}
		body = strings.ReplaceAll(body, `\n`, "<br/>")
		out = append(out, dict.Entry{Word: d.words[id], Body: body, HTML: true})
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
