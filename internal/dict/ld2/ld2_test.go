package ld2

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildLD2 writes a minimal but structurally faithful LD2 file with the
// given entries, using the layout described in the package comment.
func buildLD2(t *testing.T, words []string, defs []string, wordEnc func(string) []byte) []byte {
	t.Helper()
	if len(words) != len(defs) {
		t.Fatal("bad test data")
	}
	le := binary.LittleEndian
	// inflated payload: index | words | xml
	var wordsBuf, xmlBuf bytes.Buffer
	var idx bytes.Buffer
	for i := range words {
		binary.Write(&idx, le, int32(wordsBuf.Len()))
		binary.Write(&idx, le, int32(xmlBuf.Len()))
		idx.WriteByte(0) // flags
		idx.WriteByte(0) // refs
		wordsBuf.Write(wordEnc(words[i]))
		xmlBuf.WriteString(defs[i])
	}
	// sentinel record
	binary.Write(&idx, le, int32(wordsBuf.Len()))
	binary.Write(&idx, le, int32(xmlBuf.Len()))
	idx.WriteByte(0)
	idx.WriteByte(0)
	payload := append(append(append([]byte{}, idx.Bytes()...), wordsBuf.Bytes()...), xmlBuf.Bytes()...)

	// compress in two streams
	half := len(payload) / 2
	var streams [][]byte
	for _, part := range [][]byte{payload[:half], payload[half:]} {
		var b bytes.Buffer
		zw := zlib.NewWriter(&b)
		zw.Write(part)
		zw.Close()
		streams = append(streams, b.Bytes())
	}

	// dictionary section
	var dictSec bytes.Buffer
	numDefs := len(words)
	indexArrayLen := 4 * numDefs
	binary.Write(&dictSec, le, int32(1)) // +0 dictionary type
	binary.Write(&dictSec, le, int32(0)) // +4 length (patched below)
	binary.Write(&dictSec, le, int32(indexArrayLen))
	binary.Write(&dictSec, le, int32(idx.Len()))
	binary.Write(&dictSec, le, int32(wordsBuf.Len()))
	binary.Write(&dictSec, le, int32(xmlBuf.Len()))
	dictSec.Write(make([]byte, 0x1C-24))
	for i := 0; i < numDefs; i++ {
		binary.Write(&dictSec, le, int32(i))
	}
	// compressed data header: 8 bytes, then stream end offsets
	dictSec.Write(make([]byte, 8))
	binary.Write(&dictSec, le, int32(0)) // first int (skipped by reader)
	off := 0
	for _, s := range streams {
		off += len(s)
		binary.Write(&dictSec, le, int32(off))
	}
	for _, s := range streams {
		dictSec.Write(s)
	}
	ds := dictSec.Bytes()
	le.PutUint32(ds[4:], uint32(len(ds)-8))

	// file: header up to 0x60, then data section of type 3 (dictionary
	// follows directly).
	var f bytes.Buffer
	f.WriteString("?LD2")
	f.Write(make([]byte, 0x60-4))
	hdr := f.Bytes()
	le.PutUint32(hdr[0x5C:], 0) // data section at 0x60
	// type-3 section: the dictionary header *is* the section, so its
	// first int must be 3.
	le.PutUint32(ds[0:], 3)
	f.Write(ds)
	return f.Bytes()
}

func TestParseSynthetic(t *testing.T) {
	words := []string{"apple", "Banana split", "cherry"}
	defs := []string{
		"<C><F><H><M>'æpl</M></H><I><N><P><U>n.</U></P><Q>a fruit</Q><Q>a company</Q></N></I></F></C>",
		"<C><F><H /><I><N><Q>dessert</Q></N></I></F></C>",
		"<C><F><K><![CDATA[<b>cherry</b> <a href=\"dict://key.[$DictID]/apple\">apple</a>]]></K></F></C>",
	}
	raw := buildLD2(t, words, defs, func(s string) []byte { return []byte(s) })
	d, err := Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.Count() != 3 {
		t.Fatalf("count = %d", d.Count())
	}
	if d.wordEnc != nil || d.xmlEnc != nil {
		t.Fatalf("expected UTF-8 detection")
	}
	es := d.Lookup("APPLE")
	if len(es) != 1 || es[0].Word != "apple" {
		t.Fatalf("lookup apple: %+v", es)
	}
	if !strings.Contains(es[0].Body, "<ol class=\"ld2-defs\"><li>a fruit</li><li>a company</li></ol>") {
		t.Errorf("body: %s", es[0].Body)
	}
	if !strings.Contains(es[0].Body, `<span class="ld2-pos"><i>n.</i></span>`) {
		t.Errorf("pos missing: %s", es[0].Body)
	}
	es = d.Lookup("banana  split")
	if len(es) != 1 || !strings.Contains(es[0].Body, `<div class="ld2-def">dessert</div>`) {
		t.Errorf("banana: %+v", es)
	}
	es = d.Lookup("cherry")
	if len(es) != 1 || !strings.Contains(es[0].Body, `href="entry://apple"`) {
		t.Errorf("cherry link rewrite: %+v", es)
	}
	if got := d.Prefix("b", 5); len(got) != 1 || got[0] != "Banana split" {
		t.Errorf("prefix: %v", got)
	}
	if es := d.Lookup("bananasplit"); len(es) != 1 {
		t.Errorf("loose lookup failed: %v", es)
	}
}

func TestParseUTF16Words(t *testing.T) {
	enc := func(s string) []byte {
		var b []byte
		for _, r := range s {
			b = append(b, byte(r), byte(r>>8))
		}
		return b
	}
	words := []string{"größe", "日本語", "über"}
	defs := []string{"<C><F><H /><I><N>size</N></I></F></C>", "<C><F><H /><I><N>Japanese</N></I></F></C>", "<C><F><H /><I><N>over</N></I></F></C>"}
	d, err := Parse(buildLD2(t, words, defs, enc))
	if err != nil {
		t.Fatal(err)
	}
	if d.wordEnc == nil {
		t.Fatalf("expected UTF-16LE word encoding")
	}
	if es := d.Lookup("日本語"); len(es) != 1 || !strings.Contains(es[0].Body, "Japanese") {
		t.Errorf("lookup: %+v", es)
	}
}

func TestRejectsGarbage(t *testing.T) {
	if _, err := Parse([]byte("not a dictionary at all, definitely not")); err == nil {
		t.Fatal("expected error")
	}
	if _, err := Parse(append([]byte("?LD2"), make([]byte, 200)...)); err == nil {
		t.Fatal("expected error for empty file")
	}
}

// TestRealFiles exercises real Lingoes dictionaries when LINGLIKE_LD2_DIR
// points at a directory containing them.
func TestRealFiles(t *testing.T) {
	dir := os.Getenv("LINGLIKE_LD2_DIR")
	if dir == "" {
		t.Skip("LINGLIKE_LD2_DIR not set")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.ld2"))
	if len(files) == 0 {
		t.Skip("no .ld2 files")
	}
	for _, f := range files {
		d, err := Open(f)
		if err != nil {
			t.Errorf("%s: %v", filepath.Base(f), err)
			continue
		}
		if d.Count() == 0 {
			t.Errorf("%s: no entries", filepath.Base(f))
		}
		// every entry must render without panicking and have a word
		empty := 0
		for i := 0; i < d.Count(); i++ {
			e, ok := d.Entry(i)
			if !ok {
				t.Fatalf("%s: entry %d missing", filepath.Base(f), i)
			}
			if e.Word == "" {
				empty++
			}
		}
		if empty > d.Count()/100 {
			t.Errorf("%s: %d of %d entries have empty headwords", filepath.Base(f), empty, d.Count())
		}
		d.Close()
	}
}

func TestConciseEnglish(t *testing.T) {
	dir := os.Getenv("LINGLIKE_LD2_DIR")
	if dir == "" {
		t.Skip("LINGLIKE_LD2_DIR not set")
	}
	d, err := Open(filepath.Join(dir, "Concise English Dictionary.ld2"))
	if err != nil {
		t.Skip(err)
	}
	defer d.Close()
	es := d.Lookup("happy")
	if len(es) != 1 {
		t.Fatalf("happy: %d results", len(es))
	}
	for _, want := range []string{"'hæpɪ", "adj.", "<li>enjoying or showing or marked by joy or pleasure or good fortune</li>", "<li>well expressed and to the point</li>"} {
		if !strings.Contains(es[0].Body, want) {
			t.Errorf("missing %q in %s", want, es[0].Body)
		}
	}
}
