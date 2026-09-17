// Package mdx reads MDict dictionaries (*.mdx, engine versions 1.x and 2.x).
//
// Layout: header (UTF-16LE XML attributes) | key block section | record
// block section. Key blocks map headwords to offsets in the concatenated
// (decompressed) record blocks. Blocks are stored raw, LZO1X or zlib
// compressed; the key block info of v2 files is additionally obfuscated
// with a RIPEMD-128 derived key when Encrypted has bit 2 set.
package mdx

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/adler32"
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/aerial337/linglike/internal/dict"
	lzo "github.com/rasky/go-lzo"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/encoding/unicode"
)

func init() {
	dict.Register(".mdx", func(p string) (dict.Dictionary, error) { return Open(p) })
}

type recordBlock struct {
	fileOff    int64 // absolute offset of the compressed block in the file
	compSize   int64
	decompSize int64
	start      int64 // decompressed start offset (cumulative)
}

// Dictionary is a loaded MDX file. Keys are held in memory, record blocks
// are decompressed on demand and cached.
type Dictionary struct {
	path     string
	name     string
	header   map[string]string
	version  float64
	numWidth int
	encoding encoding.Encoding // nil = UTF-8
	utf16    bool
	encrypt  int
	styles   map[string][2]string

	words   []string
	offsets []int64 // record offset per key
	blocks  []recordBlock
	index   *dict.Index

	f     *os.File
	mu    sync.Mutex
	cache map[int][]byte
}

// Open loads an MDX dictionary.
func Open(path string) (*Dictionary, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	d := &Dictionary{path: path, f: f, cache: map[int][]byte{}}
	if err := d.load(); err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	d.name = strings.TrimSpace(html.UnescapeString(d.header["Title"]))
	if d.name == "" || strings.EqualFold(d.name, "Title (No HTML code allowed)") {
		d.name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	d.index = dict.BuildIndex(d.words)
	return d, nil
}

var headerAttrRe = regexp.MustCompile(`(?s)(\w+)="(.*?)"`)

func (d *Dictionary) readNumber(b []byte) (uint64, []byte, error) {
	if len(b) < d.numWidth {
		return 0, nil, io.ErrUnexpectedEOF
	}
	if d.numWidth == 8 {
		return binary.BigEndian.Uint64(b), b[8:], nil
	}
	return uint64(binary.BigEndian.Uint32(b)), b[4:], nil
}

func (d *Dictionary) load() error {
	var hdrLen [4]byte
	if _, err := io.ReadFull(d.f, hdrLen[:]); err != nil {
		return err
	}
	n := binary.BigEndian.Uint32(hdrLen[:])
	if n > 1<<24 {
		return errors.New("not an MDX file")
	}
	hdr := make([]byte, n)
	if _, err := io.ReadFull(d.f, hdr); err != nil {
		return err
	}
	var sum [4]byte
	if _, err := io.ReadFull(d.f, sum[:]); err != nil {
		return err
	}
	if binary.LittleEndian.Uint32(sum[:]) != adler32.Checksum(hdr) {
		return errors.New("header checksum mismatch")
	}
	keyBlockOffset := int64(4 + n + 4)

	text, err := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder().Bytes(hdr)
	if err != nil {
		return fmt.Errorf("header: %w", err)
	}
	d.header = map[string]string{}
	for _, m := range headerAttrRe.FindAllStringSubmatch(string(text), -1) {
		d.header[m[1]] = html.UnescapeString(m[2])
	}
	d.version, _ = strconv.ParseFloat(strings.TrimSpace(d.header["GeneratedByEngineVersion"]), 64)
	if d.version == 0 {
		return errors.New("missing GeneratedByEngineVersion")
	}
	if d.version >= 3 {
		return errors.New("MDX version 3 files are not supported")
	}
	d.numWidth = 4
	if d.version >= 2 {
		d.numWidth = 8
	}
	switch strings.ToUpper(strings.TrimSpace(d.header["Encoding"])) {
	case "", "UTF-8", "UTF8":
		d.encoding = nil
	case "UTF-16", "UTF16":
		d.utf16 = true
		d.encoding = unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM)
	case "GBK", "GB2312", "GB18030":
		d.encoding = simplifiedchinese.GB18030
	case "BIG5", "BIG-5":
		d.encoding = traditionalchinese.Big5
	case "SHIFT_JIS", "SHIFT-JIS", "SJIS":
		d.encoding = japanese.ShiftJIS
	case "EUC-JP":
		d.encoding = japanese.EUCJP
	case "EUC-KR":
		d.encoding = korean.EUCKR
	case "ISO8859-1", "ISO-8859-1", "LATIN1", "WINDOWS-1252", "CP1252":
		d.encoding = charmap.Windows1252
	default:
		d.encoding = nil
	}
	switch strings.TrimSpace(d.header["Encrypted"]) {
	case "", "No", "0":
		d.encrypt = 0
	case "Yes":
		d.encrypt = 1
	default:
		d.encrypt, _ = strconv.Atoi(strings.TrimSpace(d.header["Encrypted"]))
	}
	if d.encrypt&1 != 0 {
		return errors.New("record blocks are encrypted (registration required); not supported")
	}
	if ss := d.header["StyleSheet"]; ss != "" {
		d.styles = map[string][2]string{}
		lines := strings.Split(strings.ReplaceAll(ss, "\r\n", "\n"), "\n")
		for i := 0; i+2 < len(lines); i += 3 {
			d.styles[strings.TrimSpace(lines[i])] = [2]string{lines[i+1], lines[i+2]}
		}
	}
	return d.readKeys(keyBlockOffset)
}

func (d *Dictionary) readKeys(off int64) error {
	if _, err := d.f.Seek(off, io.SeekStart); err != nil {
		return err
	}
	metaLen := 4 * 4
	if d.version >= 2 {
		metaLen = 8 * 5
	}
	meta := make([]byte, metaLen)
	if _, err := io.ReadFull(d.f, meta); err != nil {
		return err
	}
	b := meta
	var err error
	var numKeyBlocks, numEntries, infoDecompSize, infoSize, keyBlockSize uint64
	if numKeyBlocks, b, err = d.readNumber(b); err != nil {
		return err
	}
	if numEntries, b, err = d.readNumber(b); err != nil {
		return err
	}
	if d.version >= 2 {
		if infoDecompSize, b, err = d.readNumber(b); err != nil {
			return err
		}
	}
	if infoSize, b, err = d.readNumber(b); err != nil {
		return err
	}
	if keyBlockSize, _, err = d.readNumber(b); err != nil {
		return err
	}
	_ = infoDecompSize
	if d.version >= 2 {
		var sum [4]byte
		if _, err := io.ReadFull(d.f, sum[:]); err != nil {
			return err
		}
		if binary.BigEndian.Uint32(sum[:]) != adler32.Checksum(meta) {
			return errors.New("key block meta checksum mismatch")
		}
	}
	if infoSize > 1<<30 || keyBlockSize > 1<<31 {
		return errors.New("corrupt key block sizes")
	}
	info := make([]byte, infoSize)
	if _, err := io.ReadFull(d.f, info); err != nil {
		return err
	}
	sizes, err := d.decodeKeyBlockInfo(info)
	if err != nil {
		return err
	}
	if uint64(len(sizes)) != numKeyBlocks {
		return fmt.Errorf("key block count mismatch (%d != %d)", len(sizes), numKeyBlocks)
	}
	keyBlocks := make([]byte, keyBlockSize)
	if _, err := io.ReadFull(d.f, keyBlocks); err != nil {
		return err
	}
	d.words = make([]string, 0, numEntries)
	d.offsets = make([]int64, 0, numEntries)
	p := 0
	for _, s := range sizes {
		if p+int(s.comp) > len(keyBlocks) {
			return errors.New("truncated key blocks")
		}
		blk, err := decompressBlock(keyBlocks[p:p+int(s.comp)], int(s.decomp))
		if err != nil {
			return fmt.Errorf("key block: %w", err)
		}
		d.splitKeyBlock(blk)
		p += int(s.comp)
	}
	recordOff, err := d.f.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	return d.readRecordIndex(recordOff)
}

type blockSize struct{ comp, decomp uint64 }

func (d *Dictionary) decodeKeyBlockInfo(info []byte) ([]blockSize, error) {
	if d.version >= 2 {
		if len(info) < 8 || !bytes.Equal(info[:4], []byte{2, 0, 0, 0}) {
			return nil, errors.New("unexpected key block info header")
		}
		if d.encrypt&2 != 0 {
			key := ripemd128(append(append([]byte{}, info[4:8]...), 0x95, 0x36, 0, 0))
			info = append(append([]byte{}, info[:8]...), fastDecrypt(info[8:], key)...)
		}
		zr, err := zlib.NewReader(bytes.NewReader(info[8:]))
		if err != nil {
			return nil, fmt.Errorf("key block info: %w", err)
		}
		dec, err := io.ReadAll(zr)
		zr.Close()
		if err != nil {
			return nil, fmt.Errorf("key block info: %w", err)
		}
		info = dec
	}
	var out []blockSize
	byteWidth, textTerm := 1, 0
	if d.version >= 2 {
		byteWidth, textTerm = 2, 1
	}
	charW := 1
	if d.utf16 {
		charW = 2
	}
	i := 0
	for i < len(info) {
		var err error
		// number of entries in this block (unused)
		if _, _, err = d.readNumber(info[i:]); err != nil {
			return nil, err
		}
		i += d.numWidth
		if i+byteWidth > len(info) {
			return nil, io.ErrUnexpectedEOF
		}
		headLen := int(info[i])
		if byteWidth == 2 {
			headLen = int(binary.BigEndian.Uint16(info[i:]))
		}
		i += byteWidth + (headLen+textTerm)*charW
		if i+byteWidth > len(info) {
			return nil, io.ErrUnexpectedEOF
		}
		tailLen := int(info[i])
		if byteWidth == 2 {
			tailLen = int(binary.BigEndian.Uint16(info[i:]))
		}
		i += byteWidth + (tailLen+textTerm)*charW
		var bs blockSize
		if bs.comp, _, err = d.readNumber(info[i:]); err != nil {
			return nil, err
		}
		i += d.numWidth
		if bs.decomp, _, err = d.readNumber(info[i:]); err != nil {
			return nil, err
		}
		i += d.numWidth
		out = append(out, bs)
	}
	return out, nil
}

func decompressBlock(blk []byte, decompSize int) ([]byte, error) {
	if len(blk) < 8 {
		return nil, errors.New("block too short")
	}
	typ := binary.LittleEndian.Uint32(blk[:4])
	sum := binary.BigEndian.Uint32(blk[4:8])
	data := blk[8:]
	var out []byte
	switch typ {
	case 0:
		out = data
	case 1:
		o, err := lzo.Decompress1X(bytes.NewReader(data), len(data), decompSize)
		if err != nil {
			return nil, fmt.Errorf("lzo: %w", err)
		}
		out = o
	case 2:
		zr, err := zlib.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		o, err := io.ReadAll(zr)
		zr.Close()
		if err != nil {
			return nil, err
		}
		out = o
	default:
		return nil, fmt.Errorf("unknown compression type %d", typ)
	}
	if adler32.Checksum(out) != sum {
		return nil, errors.New("block checksum mismatch")
	}
	return out, nil
}

func (d *Dictionary) decodeText(b []byte) string {
	if d.encoding == nil {
		if bytes.IndexByte(b, 0) >= 0 {
			b = bytes.ReplaceAll(b, []byte{0}, nil)
		}
		return strings.ToValidUTF8(string(b), "�")
	}
	s, err := d.encoding.NewDecoder().Bytes(b)
	if err != nil {
		return strings.ToValidUTF8(string(b), "�")
	}
	return strings.ReplaceAll(string(s), "\x00", "")
}

func (d *Dictionary) splitKeyBlock(blk []byte) {
	i := 0
	for i+d.numWidth < len(blk) {
		id, _, err := d.readNumber(blk[i:])
		if err != nil {
			return
		}
		i += d.numWidth
		end := -1
		if d.utf16 {
			for k := i; k+1 < len(blk); k += 2 {
				if blk[k] == 0 && blk[k+1] == 0 {
					end = k
					break
				}
			}
		} else {
			for k := i; k < len(blk); k++ {
				if blk[k] == 0 {
					end = k
					break
				}
			}
		}
		if end < 0 {
			end = len(blk)
		}
		word := strings.TrimSpace(d.decodeText(blk[i:end]))
		d.words = append(d.words, word)
		d.offsets = append(d.offsets, int64(id))
		i = end + 1
		if d.utf16 {
			i++
		}
	}
}

func (d *Dictionary) readRecordIndex(off int64) error {
	if _, err := d.f.Seek(off, io.SeekStart); err != nil {
		return err
	}
	meta := make([]byte, 4*d.numWidth)
	if _, err := io.ReadFull(d.f, meta); err != nil {
		return err
	}
	b := meta
	var err error
	var numBlocks, numEntries, infoSize, dataSize uint64
	if numBlocks, b, err = d.readNumber(b); err != nil {
		return err
	}
	if numEntries, b, err = d.readNumber(b); err != nil {
		return err
	}
	_ = numEntries
	if infoSize, b, err = d.readNumber(b); err != nil {
		return err
	}
	if dataSize, _, err = d.readNumber(b); err != nil {
		return err
	}
	_ = dataSize
	if infoSize != numBlocks*uint64(2*d.numWidth) || infoSize > 1<<30 {
		return errors.New("corrupt record block info")
	}
	info := make([]byte, infoSize)
	if _, err := io.ReadFull(d.f, info); err != nil {
		return err
	}
	dataOff := off + int64(len(meta)) + int64(infoSize)
	d.blocks = make([]recordBlock, 0, numBlocks)
	var fileOff, start int64 = dataOff, 0
	for i := 0; i < len(info); {
		comp, _, err := d.readNumber(info[i:])
		if err != nil {
			return err
		}
		i += d.numWidth
		decomp, _, err := d.readNumber(info[i:])
		if err != nil {
			return err
		}
		i += d.numWidth
		d.blocks = append(d.blocks, recordBlock{fileOff: fileOff, compSize: int64(comp), decompSize: int64(decomp), start: start})
		fileOff += int64(comp)
		start += int64(decomp)
	}
	return nil
}

func (d *Dictionary) block(i int) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if b, ok := d.cache[i]; ok {
		return b, nil
	}
	rb := d.blocks[i]
	if rb.compSize > 1<<28 {
		return nil, errors.New("record block too large")
	}
	comp := make([]byte, rb.compSize)
	if _, err := d.f.ReadAt(comp, rb.fileOff); err != nil && err != io.EOF {
		return nil, err
	}
	out, err := decompressBlock(comp, int(rb.decompSize))
	if err != nil {
		return nil, err
	}
	if len(d.cache) > 32 {
		d.cache = map[int][]byte{}
	}
	d.cache[i] = out
	return out, nil
}

// record returns the raw record for key id.
func (d *Dictionary) record(id int) ([]byte, error) {
	start := d.offsets[id]
	var end int64 = -1
	if id+1 < len(d.offsets) {
		end = d.offsets[id+1]
	}
	bi := sort.Search(len(d.blocks), func(i int) bool { return d.blocks[i].start > start }) - 1
	if bi < 0 || bi >= len(d.blocks) {
		return nil, errors.New("record offset out of range")
	}
	blk, err := d.block(bi)
	if err != nil {
		return nil, err
	}
	rb := d.blocks[bi]
	s := start - rb.start
	e := int64(len(blk))
	if end >= 0 && end-rb.start < e && end > start {
		e = end - rb.start
	}
	if s < 0 || s > int64(len(blk)) {
		return nil, errors.New("record offset out of range")
	}
	return blk[s:e], nil
}

var styleRe = regexp.MustCompile("`(\\d+)`")

func (d *Dictionary) substituteStyles(s string) string {
	if d.styles == nil || !strings.Contains(s, "`") {
		return s
	}
	var sb strings.Builder
	var open string
	last := 0
	for _, m := range styleRe.FindAllStringSubmatchIndex(s, -1) {
		sb.WriteString(s[last:m[0]])
		sb.WriteString(open)
		open = ""
		st, ok := d.styles[s[m[2]:m[3]]]
		if ok {
			sb.WriteString(st[0])
			open = st[1]
		}
		last = m[1]
	}
	sb.WriteString(s[last:])
	sb.WriteString(open)
	return sb.String()
}

var (
	entryLinkRe = regexp.MustCompile(`(?i)href\s*=\s*(["']?)entry://`)
	soundRe     = regexp.MustCompile(`(?i)<a\s[^>]*href\s*=\s*["']?sound://[^>]*>(.*?)</a>`)
	linkWordRe  = regexp.MustCompile(`(?i)@@@LINK=(.+)`)
)

// Entry renders key id as HTML, following @@@LINK redirects.
func (d *Dictionary) Entry(id int) (dict.Entry, bool) {
	return d.entry(id, 0)
}

func (d *Dictionary) entry(id int, depth int) (dict.Entry, bool) {
	if id < 0 || id >= len(d.words) {
		return dict.Entry{}, false
	}
	raw, err := d.record(id)
	if err != nil {
		return dict.Entry{Word: d.words[id], Body: "<i>" + html.EscapeString(err.Error()) + "</i>", HTML: true}, true
	}
	text := strings.TrimRight(d.decodeText(raw), "\x00\r\n ")
	if m := linkWordRe.FindStringSubmatch(text); m != nil && depth < 4 {
		target := strings.TrimSpace(m[1])
		if ids := d.index.Exact(target); len(ids) > 0 {
			e, ok := d.entry(int(ids[0]), depth+1)
			if ok {
				e.Word = d.words[id]
				return e, true
			}
		}
		return dict.Entry{Word: d.words[id], Body: `See <a href="entry://` + html.EscapeString(target) + `">` + html.EscapeString(target) + `</a>`, HTML: true}, true
	}
	text = d.substituteStyles(text)
	text = soundRe.ReplaceAllString(text, "$1")
	return dict.Entry{Word: d.words[id], Body: text, HTML: true}, true
}

func (d *Dictionary) Name() string { return d.name }
func (d *Dictionary) Path() string { return d.path }
func (d *Dictionary) Count() int   { return len(d.words) }
func (d *Dictionary) Close() error { return d.f.Close() }

// Header returns the parsed header attributes.
func (d *Dictionary) Header() map[string]string { return d.header }

func (d *Dictionary) Lookup(word string) []dict.Entry {
	var out []dict.Entry
	seen := map[string]bool{}
	for _, id := range d.index.Exact(word) {
		e, ok := d.Entry(int(id))
		if !ok || seen[e.Body] {
			continue
		}
		seen[e.Body] = true
		out = append(out, e)
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

// Description returns the dictionary's description from the header.
func (d *Dictionary) Description() string { return d.header["Description"] }

var _ = entryLinkRe
