package mdx

import (
	"bytes"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

func TestRipemd128(t *testing.T) {
	got := hex.EncodeToString(ripemd128([]byte("The quick brown fox jumps over the lazy dog")))
	if got != "3fa9b57f053c053fbe2735b2380db596" {
		t.Fatalf("ripemd128 = %s", got)
	}
	if got := hex.EncodeToString(ripemd128(nil)); got != "cdf26213a150dc3ecb610f18f6b38b46" {
		t.Fatalf("ripemd128(empty) = %s", got)
	}
}

func TestFastDecryptRoundTrip(t *testing.T) {
	// fastEncrypt is the inverse of fastDecrypt as defined by MDict.
	enc := func(data, key []byte) []byte {
		b := make([]byte, len(data))
		prev := byte(0x36)
		for i := range data {
			t := data[i] ^ prev ^ byte(i) ^ key[i%len(key)]
			t = (t >> 4) | (t << 4)
			prev = t
			b[i] = t
		}
		return b
	}
	key := ripemd128([]byte("k"))
	msg := []byte("hello key block info, this is a test message")
	if got := fastDecrypt(enc(msg, key), key); !bytes.Equal(got, msg) {
		t.Fatalf("round trip failed: %q", got)
	}
}

// TestSampleMDX reads a real dictionary if LINGLIKE_MDX points at one.
func TestSampleMDX(t *testing.T) {
	p := os.Getenv("LINGLIKE_MDX")
	if p == "" {
		t.Skip("LINGLIKE_MDX not set")
	}
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if d.Count() == 0 {
		t.Fatal("no entries")
	}
	word := os.Getenv("LINGLIKE_MDX_WORD")
	if word == "" {
		word = d.words[0]
	}
	es := d.Lookup(word)
	if len(es) == 0 {
		t.Fatalf("no entry for %q", word)
	}
	if strings.HasPrefix(es[0].Body, "<i>") && strings.Contains(es[0].Body, "mismatch") {
		t.Fatalf("entry error: %s", es[0].Body)
	}
	t.Logf("%s: %d entries, %q -> %d bytes", d.Name(), d.Count(), word, len(es[0].Body))
}
