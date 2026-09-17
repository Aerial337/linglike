package stardict

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeDictzip writes data as a dictzip file with the given chunk length.
func writeDictzip(t *testing.T, path string, data []byte, chunkLen int) {
	t.Helper()
	var chunks [][]byte
	for i := 0; i < len(data); i += chunkLen {
		end := min(i+chunkLen, len(data))
		var b bytes.Buffer
		fw, _ := flate.NewWriter(&b, flate.BestSpeed)
		fw.Write(data[i:end])
		fw.Close()
		chunks = append(chunks, b.Bytes())
	}
	var extra bytes.Buffer
	extra.WriteString("RA")
	sub := make([]byte, 6+2*len(chunks))
	binary.LittleEndian.PutUint16(sub[0:], 1)
	binary.LittleEndian.PutUint16(sub[2:], uint16(chunkLen))
	binary.LittleEndian.PutUint16(sub[4:], uint16(len(chunks)))
	for i, c := range chunks {
		binary.LittleEndian.PutUint16(sub[6+2*i:], uint16(len(c)))
	}
	binary.Write(&extra, binary.LittleEndian, uint16(len(sub)))
	extra.Write(sub)

	var f bytes.Buffer
	f.Write([]byte{0x1f, 0x8b, 8, 4 | 8, 0, 0, 0, 0, 0, 3}) // FEXTRA|FNAME
	binary.Write(&f, binary.LittleEndian, uint16(extra.Len()))
	f.Write(extra.Bytes())
	f.WriteString("test.dict\x00")
	for _, c := range chunks {
		f.Write(c)
	}
	f.Write(make([]byte, 8)) // crc/size (not checked by the reader)
	if err := os.WriteFile(path, f.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func makeDict(t *testing.T, dir string, compress bool, sameType string) string {
	t.Helper()
	type e struct{ w, def string }
	entries := []e{
		{"alpha", "first letter"},
		{"beta", "second\nletter"},
		{"Gamma", "third letter <b>bold</b>"},
	}
	var dictBuf, idxBuf bytes.Buffer
	for _, en := range entries {
		off := dictBuf.Len()
		if sameType == "" {
			dictBuf.WriteString("m")
			dictBuf.WriteString(en.def)
			dictBuf.WriteByte(0)
			dictBuf.WriteString("t")
			dictBuf.WriteString("phon-" + en.w)
			dictBuf.WriteByte(0)
		} else {
			dictBuf.WriteString(en.def)
		}
		idxBuf.WriteString(en.w)
		idxBuf.WriteByte(0)
		binary.Write(&idxBuf, binary.BigEndian, uint32(off))
		binary.Write(&idxBuf, binary.BigEndian, uint32(dictBuf.Len()-off))
	}
	base := filepath.Join(dir, "test")
	ifo := "StarDict's dict ifo file\nversion=2.4.2\nbookname=Test Dict\nwordcount=3\nidxfilesize=" +
		itoa(idxBuf.Len()) + "\n"
	if sameType != "" {
		ifo += "sametypesequence=" + sameType + "\n"
	}
	os.WriteFile(base+".ifo", []byte(ifo), 0o644)
	os.WriteFile(base+".idx", idxBuf.Bytes(), 0o644)
	if compress {
		writeDictzip(t, base+".dict.dz", dictBuf.Bytes(), 16)
	} else {
		os.WriteFile(base+".dict", dictBuf.Bytes(), 0o644)
	}
	// synonyms: "alfa" -> entry 0
	var syn bytes.Buffer
	syn.WriteString("alfa\x00")
	binary.Write(&syn, binary.BigEndian, uint32(0))
	os.WriteFile(base+".syn", syn.Bytes(), 0o644)
	return base + ".ifo"
}

func itoa(i int) string {
	return strings.TrimSpace(strings.Repeat(" ", 0) + string(rune('0'+i/10)) + string(rune('0'+i%10)))
}

func TestPlainAndDictzip(t *testing.T) {
	for _, compress := range []bool{false, true} {
		for _, seq := range []string{"", "m"} {
			dir := t.TempDir()
			ifo := makeDict(t, dir, compress, seq)
			d, err := Open(ifo)
			if err != nil {
				t.Fatalf("compress=%v seq=%q: %v", compress, seq, err)
			}
			if d.Name() != "Test Dict" || d.Count() != 3 {
				t.Errorf("name/count: %s %d", d.Name(), d.Count())
			}
			es := d.Lookup("gamma")
			if len(es) != 1 || !strings.Contains(es[0].Body, "third letter &lt;b&gt;bold&lt;/b&gt;") {
				t.Errorf("gamma (compress=%v seq=%q): %+v", compress, seq, es)
			}
			es = d.Lookup("beta")
			if len(es) != 1 || !strings.Contains(es[0].Body, "second<br/>letter") {
				t.Errorf("beta: %+v", es)
			}
			if seq == "" && !strings.Contains(es[0].Body, "[phon-beta]") {
				t.Errorf("phonetic field missing: %s", es[0].Body)
			}
			es = d.Lookup("alfa")
			if len(es) != 1 || es[0].Word != "alpha" {
				t.Errorf("synonym: %+v", es)
			}
			if p := d.Prefix("a", 10); len(p) != 1 || p[0] != "alpha" {
				t.Errorf("prefix: %v", p)
			}
			d.Close()
		}
	}
}

func TestDictzipRandomAccess(t *testing.T) {
	data := []byte(strings.Repeat("0123456789abcdef", 100))
	p := filepath.Join(t.TempDir(), "x.dz")
	writeDictzip(t, p, data, 64)
	dz, err := openDictzip(p)
	if err != nil {
		t.Fatal(err)
	}
	defer dz.Close()
	for _, tc := range []struct{ off, n int }{{0, 5}, {60, 10}, {1000, 600}, {1590, 10}} {
		got, err := dz.ReadAt(int64(tc.off), tc.n)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, data[tc.off:tc.off+tc.n]) {
			t.Errorf("ReadAt(%d,%d) = %q", tc.off, tc.n, got)
		}
	}
}

func TestXdxf(t *testing.T) {
	got := renderXdxf(`<k>word</k> <kref>other</kref> <ex>example</ex> <c c="red">r</c>`)
	for _, want := range []string{"<b>word</b>", `<a href="entry://other">other</a>`, "<i>example</i>", `<span style="color:red">r</span>`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}
