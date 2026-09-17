// Package ld2 reads Lingoes dictionary files (*.ld2 / *.ldf).
//
// File layout (all integers little endian):
//
//	0x00  magic "?LD2" or "?LDF"
//	0x18  version (two uint16)
//	0x1C  dictionary id (8 bytes)
//	0x5C  int32: offset of the data section, relative to 0x60
//
// Data section:
//
//	+0   int32 type            (3 = dictionary follows directly)
//	+4   int32 info length     (otherwise the dictionary starts at +12+len)
//
// Dictionary header at offsetWithIndex:
//
//	+0   int32 dictionary type
//	+4   int32 length          (limit = offsetWithIndex + 8 + length)
//	+8   int32 offset of compressed data header, relative to +0x1C
//	+12  int32 inflated size of the index table
//	+16  int32 inflated size of the words section
//	+20  int32 inflated size of the definitions ("xml") section
//	+0x1C int32[] index array, up to the compressed data header
//
// Compressed data header: 8 bytes, then int32 end offsets of consecutive
// zlib streams, then the streams themselves. Inflating and concatenating the
// streams gives index table | words | definitions. Each index record is 10
// bytes: int32 word offset, int32 definition offset, byte flags, byte refs.
// The next record's offsets mark the end of the current one, so the last
// record is only a sentinel.
package ld2

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/aerial337/linglike/internal/dict"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
)

func init() {
	dict.Register(".ld2", func(p string) (dict.Dictionary, error) { return Open(p) })
	dict.Register(".ldf", func(p string) (dict.Dictionary, error) { return Open(p) })
}

const recordLen = 10

// Dictionary is a loaded LD2 file. All data is held in memory (inflated).
type Dictionary struct {
	path     string
	name     string
	data     []byte // inflated: index | words | xml
	offWords int
	offXml   int
	count    int
	wordEnc  encoding.Encoding // nil = UTF-8
	xmlEnc   encoding.Encoding
	words    []string
	index    *dict.Index
}

// Open loads and inflates an LD2 file.
func Open(path string) (*Dictionary, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	d, err := Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	d.path = path
	d.name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return d, nil
}

// Parse parses the raw bytes of an LD2 file.
func Parse(raw []byte) (*Dictionary, error) {
	if len(raw) < 0x60 || raw[0] != '?' || raw[1] != 'L' || raw[2] != 'D' {
		return nil, errors.New("not a Lingoes LD2 file")
	}
	le := binary.LittleEndian
	getInt := func(off int) (int, error) {
		if off < 0 || off+4 > len(raw) {
			return 0, fmt.Errorf("offset 0x%x out of range", off)
		}
		return int(int32(le.Uint32(raw[off:]))), nil
	}
	rel, err := getInt(0x5C)
	if err != nil {
		return nil, err
	}
	offsetData := rel + 0x60
	typ, err := getInt(offsetData)
	if err != nil {
		return nil, errors.New("file contains no dictionary data")
	}
	offsetWithIndex := offsetData
	if typ != 3 {
		infoLen, err := getInt(offsetData + 4)
		if err != nil {
			return nil, err
		}
		offsetWithIndex = offsetData + 12 + infoLen
		if offsetWithIndex+0x1C > len(raw) {
			return nil, errors.New("file contains no dictionary data (online dictionary?)")
		}
	}

	length, err := getInt(offsetWithIndex + 4)
	if err != nil {
		return nil, err
	}
	limit := offsetWithIndex + 8 + length
	if limit > len(raw) {
		limit = len(raw)
	}
	offsetIndex := offsetWithIndex + 0x1C
	hdrRel, err := getInt(offsetWithIndex + 8)
	if err != nil {
		return nil, err
	}
	offsetCompressedHeader := hdrRel + offsetIndex
	inflatedIndexLen, _ := getInt(offsetWithIndex + 12)
	inflatedWordsLen, _ := getInt(offsetWithIndex + 16)
	inflatedXmlLen, _ := getInt(offsetWithIndex + 20)
	if inflatedIndexLen < recordLen || inflatedWordsLen < 0 || inflatedXmlLen < 0 {
		return nil, errors.New("corrupt dictionary header")
	}

	// Stream table: skip 8 bytes, then int32 relative end offsets.
	pos := offsetCompressedHeader + 8
	var ends []int
	off, err := getInt(pos)
	if err != nil {
		return nil, err
	}
	pos += 4
	for off+pos < limit {
		off, err = getInt(pos)
		if err != nil {
			return nil, err
		}
		pos += 4
		ends = append(ends, off)
	}
	offsetCompressed := pos

	total := inflatedIndexLen + inflatedWordsLen + inflatedXmlLen
	out := bytes.NewBuffer(make([]byte, 0, total))
	last := offsetCompressed
	for _, e := range ends {
		end := offsetCompressed + e
		if end > len(raw) || end < last {
			return nil, fmt.Errorf("corrupt stream table (0x%x)", end)
		}
		if err := inflateInto(out, raw[last:end]); err != nil {
			return nil, fmt.Errorf("inflate: %w", err)
		}
		last = end
	}
	data := out.Bytes()
	if len(data) < total {
		return nil, fmt.Errorf("inflated %d bytes, expected %d", len(data), total)
	}

	d := &Dictionary{
		data:     data,
		offWords: inflatedIndexLen,
		offXml:   inflatedIndexLen + inflatedWordsLen,
		count:    inflatedIndexLen/recordLen - 1,
	}
	d.detectEncodings()
	d.words = make([]string, d.count)
	for i := 0; i < d.count; i++ {
		w, _ := d.rawEntry(i)
		d.words[i] = decode(w, d.wordEnc)
	}
	d.index = dict.BuildIndex(d.words)
	return d, nil
}

func inflateInto(w io.Writer, chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	zr, err := zlib.NewReader(bytes.NewReader(chunk))
	if err != nil {
		return err
	}
	defer zr.Close()
	_, err = io.Copy(w, zr)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return err
	}
	return nil
}

type idxRecord struct {
	wordPos, xmlPos   int
	flags, refs       int
	nextWord, nextXml int
}

func (d *Dictionary) record(i int) (r idxRecord, ok bool) {
	p := i * recordLen
	if i < 0 || p+recordLen+8 > d.offWords {
		return r, false
	}
	le := binary.LittleEndian
	r.wordPos = int(int32(le.Uint32(d.data[p:])))
	r.xmlPos = int(int32(le.Uint32(d.data[p+4:])))
	r.flags = int(d.data[p+8])
	r.refs = int(d.data[p+9])
	r.nextWord = int(int32(le.Uint32(d.data[p+10:])))
	r.nextXml = int(int32(le.Uint32(d.data[p+14:])))
	return r, true
}

func (d *Dictionary) slice(off, from, to int) []byte {
	a, b := off+from, off+to
	if a < 0 || b > len(d.data) || a > b {
		return nil
	}
	return d.data[a:b]
}

// rawEntry returns the undecoded headword bytes and the definition bytes of
// entry i. Definitions of referenced entries are joined with ", " as in
// Lingoes' own reader.
func (d *Dictionary) rawEntry(i int) (word []byte, xml [][]byte) {
	r, ok := d.record(i)
	if !ok {
		return nil, nil
	}
	wordPos := r.wordPos
	if x := d.slice(d.offXml, r.xmlPos, r.nextXml); len(x) > 0 {
		xml = append(xml, x)
	}
	for k := 0; k < r.refs; k++ {
		refBytes := d.slice(d.offWords, wordPos, wordPos+4)
		if refBytes == nil {
			break
		}
		ref := int(int32(binary.LittleEndian.Uint32(refBytes)))
		if rr, ok := d.record(ref); ok {
			if x := d.slice(d.offXml, rr.xmlPos, rr.nextXml); len(x) > 0 {
				xml = append(xml, x)
			}
		}
		wordPos += 4
	}
	word = d.slice(d.offWords, wordPos, r.nextWord)
	return word, xml
}

var candidateEncodings = []struct {
	name string
	enc  encoding.Encoding
}{
	{"UTF-8", nil},
	{"UTF-16LE", unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)},
	{"UTF-16BE", unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM)},
	{"EUC-JP", japanese.EUCJP},
	{"EUC-KR", korean.EUCKR},
	{"GB18030", simplifiedchinese.GB18030},
	{"Big5", traditionalchinese.Big5},
}

// validIn reports whether b decodes strictly (no replacement characters)
// under enc.
func validIn(b []byte, enc encoding.Encoding) bool {
	if len(b) == 0 {
		return true
	}
	if enc == nil {
		if !utf8.Valid(b) {
			return false
		}
		for _, r := range string(b) {
			if r == utf8.RuneError || (r < 0x20 && r != '\t' && r != '\n' && r != '\r') {
				return false
			}
		}
		return true
	}
	s, err := enc.NewDecoder().Bytes(b)
	if err != nil {
		return false
	}
	if bytes.ContainsRune(s, utf8.RuneError) || bytes.ContainsRune(s, 0) {
		return false
	}
	return true
}

func decode(b []byte, enc encoding.Encoding) string {
	if enc == nil {
		if utf8.Valid(b) {
			return string(b)
		}
		return strings.ToValidUTF8(string(b), "�")
	}
	s, err := enc.NewDecoder().Bytes(b)
	if err != nil {
		return strings.ToValidUTF8(string(b), "�")
	}
	return string(s)
}

// detectEncodings tries the candidate encodings on a sample of entries
// (like Lingoes' reference reader) and picks the first that decodes cleanly
// for headwords and, separately, for definitions.
func (d *Dictionary) detectEncodings() {
	sample := 64
	if d.count < sample {
		sample = d.count
	}
	step := 1
	if d.count > sample && sample > 0 {
		step = d.count / sample
	}
	var words, xmls [][]byte
	for i := 0; i < d.count && len(words) < sample; i += step {
		w, x := d.rawEntry(i)
		words = append(words, w)
		xmls = append(xmls, x...)
	}
	pick := func(samples [][]byte) encoding.Encoding {
		for _, c := range candidateEncodings {
			ok := true
			for _, s := range samples {
				if !validIn(s, c.enc) {
					ok = false
					break
				}
			}
			if ok {
				return c.enc
			}
		}
		return candidateEncodings[1].enc // UTF-16LE, Lingoes' default
	}
	d.wordEnc = pick(words)
	d.xmlEnc = pick(xmls)
}

// Name implements dict.Dictionary.
func (d *Dictionary) Name() string { return d.name }

// SetName overrides the display name.
func (d *Dictionary) SetName(n string) { d.name = n }

// Path implements dict.Dictionary.
func (d *Dictionary) Path() string { return d.path }

// Count implements dict.Dictionary.
func (d *Dictionary) Count() int { return d.count }

// Close implements dict.Dictionary.
func (d *Dictionary) Close() error { d.data = nil; d.words = nil; d.index = nil; return nil }

// Entry returns entry i with its definition converted to HTML.
func (d *Dictionary) Entry(i int) (dict.Entry, bool) {
	if i < 0 || i >= d.count {
		return dict.Entry{}, false
	}
	_, xmls := d.rawEntry(i)
	parts := make([]string, 0, len(xmls))
	for _, x := range xmls {
		parts = append(parts, decode(x, d.xmlEnc))
	}
	return dict.Entry{Word: d.words[i], Body: ToHTML(parts), HTML: true}, true
}

// RawEntry returns entry i with the definition markup untouched.
func (d *Dictionary) RawEntry(i int) (word string, defs []string) {
	if i < 0 || i >= d.count {
		return "", nil
	}
	_, xmls := d.rawEntry(i)
	for _, x := range xmls {
		defs = append(defs, decode(x, d.xmlEnc))
	}
	return d.words[i], defs
}

// Lookup implements dict.Dictionary.
func (d *Dictionary) Lookup(word string) []dict.Entry {
	var out []dict.Entry
	for _, id := range d.index.Exact(word) {
		if e, ok := d.Entry(int(id)); ok {
			out = append(out, e)
		}
	}
	return out
}

// Prefix implements dict.Dictionary.
func (d *Dictionary) Prefix(prefix string, limit int) []string {
	ids := d.index.Prefix(prefix, limit)
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, d.words[id])
	}
	return out
}
