// Package dict defines the dictionary abstraction shared by all supported
// dictionary file formats (Lingoes LD2, StarDict, MDict MDX, plain text).
package dict

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// Entry is a single headword with its definition.
type Entry struct {
	// Word is the headword exactly as stored in the dictionary.
	Word string
	// Body is the definition. When HTML is true it is an HTML fragment,
	// otherwise it is plain text (newlines are significant).
	Body string
	HTML bool
}

// Dictionary is a loaded dictionary file.
type Dictionary interface {
	// Name is the display name of the dictionary.
	Name() string
	// Path is the file the dictionary was loaded from.
	Path() string
	// Count is the number of headwords.
	Count() int
	// Lookup returns the entries whose headword matches word
	// (case-insensitively). It returns nil when nothing matches.
	Lookup(word string) []Entry
	// Prefix returns up to limit distinct headwords starting with prefix,
	// in dictionary order. Used for suggestions while typing.
	Prefix(prefix string, limit int) []string
	// Close releases resources held by the dictionary.
	Close() error
}

// Opener loads a dictionary file. Format packages register themselves at init.
type Opener func(path string) (Dictionary, error)

var openers = map[string]Opener{}

// Register associates a lower-case file extension (including the dot) with
// an Opener.
func Register(ext string, o Opener) { openers[strings.ToLower(ext)] = o }

// SupportedExtensions returns the registered extensions, sorted.
func SupportedExtensions() []string {
	out := make([]string, 0, len(openers))
	for e := range openers {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

// ErrUnsupported is returned by Open for unknown file extensions.
var ErrUnsupported = errors.New("unsupported dictionary format")

// Open loads a dictionary, choosing the format by file extension.
func Open(path string) (Dictionary, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".dz" && strings.HasSuffix(strings.ToLower(path), ".dict.dz") {
		ext = ".ifo"
		path = strings.TrimSuffix(path, path[len(path)-len(".dict.dz"):]) + ".ifo"
	}
	if ext == ".dict" || ext == ".idx" {
		ext = ".ifo"
		path = path[:len(path)-len(filepath.Ext(path))] + ".ifo"
	}
	o, ok := openers[ext]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, ext)
	}
	return o(path)
}

// NormalizeKey converts a headword into the canonical form used for index
// comparisons: trimmed, lower-cased, inner whitespace collapsed.
func NormalizeKey(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	if strings.ContainsAny(s, "\t\n\r  ") {
		s = strings.Join(strings.Fields(s), " ")
	}
	return s
}

// LooseKey is a more aggressive normalisation that keeps only letters and
// digits. It is used as a fallback when an exact key lookup fails, so that
// "e-mail" finds "email" and "don't" finds "dont".
func LooseKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Index is a sorted, case-insensitive headword index shared by the format
// implementations. It maps normalised keys to entry ids.
type Index struct {
	keys  []string // sorted normalised keys
	ids   []int32  // ids parallel to keys
	loose map[string][]int32
}

// BuildIndex creates an Index from a list of headwords; id i refers to words[i].
func BuildIndex(words []string) *Index {
	n := len(words)
	idx := &Index{keys: make([]string, n), ids: make([]int32, n)}
	for i, w := range words {
		idx.keys[i] = NormalizeKey(w)
		idx.ids[i] = int32(i)
	}
	sort.Sort(idx)
	return idx
}

func (ix *Index) Len() int           { return len(ix.keys) }
func (ix *Index) Less(i, j int) bool { return ix.keys[i] < ix.keys[j] }
func (ix *Index) Swap(i, j int) {
	ix.keys[i], ix.keys[j] = ix.keys[j], ix.keys[i]
	ix.ids[i], ix.ids[j] = ix.ids[j], ix.ids[i]
}

// Exact returns ids whose key equals the normalised word. If none match and
// the loose form of the word matches, those ids are returned instead.
func (ix *Index) Exact(word string) []int32 {
	key := NormalizeKey(word)
	if key == "" {
		return nil
	}
	lo := sort.SearchStrings(ix.keys, key)
	var out []int32
	for i := lo; i < len(ix.keys) && ix.keys[i] == key; i++ {
		out = append(out, ix.ids[i])
	}
	if len(out) > 0 {
		return out
	}
	lk := LooseKey(word)
	if lk == "" {
		return nil
	}
	ix.buildLoose()
	return ix.loose[lk]
}

func (ix *Index) buildLoose() {
	if ix.loose != nil {
		return
	}
	m := make(map[string][]int32, len(ix.keys))
	for i, k := range ix.keys {
		lk := LooseKey(k)
		if lk == "" || lk == k {
			// identical keys are already served by the exact path; still
			// store them so that "e-mail" can find "email".
		}
		if lk != "" {
			m[lk] = append(m[lk], ix.ids[i])
		}
	}
	ix.loose = m
}

// Prefix returns up to limit ids whose key starts with prefix, in key order.
func (ix *Index) Prefix(prefix string, limit int) []int32 {
	p := NormalizeKey(prefix)
	if p == "" {
		return nil
	}
	lo := sort.SearchStrings(ix.keys, p)
	var out []int32
	last := ""
	for i := lo; i < len(ix.keys) && len(out) < limit; i++ {
		if !strings.HasPrefix(ix.keys[i], p) {
			break
		}
		if ix.keys[i] == last {
			continue
		}
		last = ix.keys[i]
		out = append(out, ix.ids[i])
	}
	return out
}

// Manager holds the ordered list of enabled dictionaries and performs
// lookups across all of them.
type Manager struct {
	Dicts []Dictionary
}

// Result is the outcome of a lookup in one dictionary.
type Result struct {
	Dict    Dictionary
	Entries []Entry
}

// Lookup queries every dictionary in order and returns the non-empty results.
func (m *Manager) Lookup(word string) []Result {
	word = strings.TrimSpace(word)
	if word == "" {
		return nil
	}
	var out []Result
	for _, d := range m.Dicts {
		if es := d.Lookup(word); len(es) > 0 {
			out = append(out, Result{Dict: d, Entries: es})
		}
	}
	return out
}

// Suggest merges prefix suggestions from all dictionaries.
func (m *Manager) Suggest(prefix string, limit int) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range m.Dicts {
		for _, w := range d.Prefix(prefix, limit) {
			k := NormalizeKey(w)
			if !seen[k] {
				seen[k] = true
				out = append(out, w)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return NormalizeKey(out[i]) < NormalizeKey(out[j]) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Close closes all dictionaries.
func (m *Manager) Close() {
	for _, d := range m.Dicts {
		_ = d.Close()
	}
	m.Dicts = nil
}
