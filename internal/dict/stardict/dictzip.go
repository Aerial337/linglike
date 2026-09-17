package stardict

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
)

// dictzip provides random access to a .dz (dictzip) file: a gzip file whose
// FEXTRA field ("RA") lists the compressed size of each fixed-size chunk.
// Each chunk was flushed with Z_FULL_FLUSH so it can be inflated on its own.
type dictzip struct {
	f        *os.File
	chunkLen int
	offsets  []int64 // compressed offset of each chunk (absolute in file)
	sizes    []int   // compressed size of each chunk
	mu       sync.Mutex
	cache    map[int][]byte
}

func openDictzip(path string) (*dictzip, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	hdr := make([]byte, 12)
	if _, err := io.ReadFull(f, hdr); err != nil {
		f.Close()
		return nil, err
	}
	if hdr[0] != 0x1f || hdr[1] != 0x8b || hdr[2] != 8 {
		f.Close()
		return nil, errors.New("not a gzip file")
	}
	flg := hdr[3]
	if flg&4 == 0 {
		f.Close()
		return nil, errors.New("gzip file has no dictzip random access table")
	}
	xlen := int(binary.LittleEndian.Uint16(hdr[10:]))
	extra := make([]byte, xlen)
	if _, err := io.ReadFull(f, extra); err != nil {
		f.Close()
		return nil, err
	}
	dz := &dictzip{f: f, cache: map[int][]byte{}}
	found := false
	for p := 0; p+4 <= len(extra); {
		si1, si2 := extra[p], extra[p+1]
		l := int(binary.LittleEndian.Uint16(extra[p+2:]))
		sub := extra[p+4 : min(p+4+l, len(extra))]
		p += 4 + l
		if si1 == 'R' && si2 == 'A' && len(sub) >= 6 {
			ver := binary.LittleEndian.Uint16(sub[0:])
			if ver != 1 {
				f.Close()
				return nil, fmt.Errorf("unsupported dictzip version %d", ver)
			}
			dz.chunkLen = int(binary.LittleEndian.Uint16(sub[2:]))
			n := int(binary.LittleEndian.Uint16(sub[4:]))
			if len(sub) < 6+2*n {
				f.Close()
				return nil, errors.New("truncated dictzip chunk table")
			}
			dz.sizes = make([]int, n)
			for i := 0; i < n; i++ {
				dz.sizes[i] = int(binary.LittleEndian.Uint16(sub[6+2*i:]))
			}
			found = true
		}
	}
	if !found || dz.chunkLen == 0 {
		f.Close()
		return nil, errors.New("gzip file has no dictzip random access table")
	}
	// Skip the remaining optional header fields to find the data start.
	pos := int64(12 + xlen)
	skipZ := func() error {
		b := make([]byte, 1)
		for {
			if _, err := f.ReadAt(b, pos); err != nil {
				return err
			}
			pos++
			if b[0] == 0 {
				return nil
			}
		}
	}
	if flg&8 != 0 { // FNAME
		if err := skipZ(); err != nil {
			f.Close()
			return nil, err
		}
	}
	if flg&16 != 0 { // FCOMMENT
		if err := skipZ(); err != nil {
			f.Close()
			return nil, err
		}
	}
	if flg&2 != 0 { // FHCRC
		pos += 2
	}
	dz.offsets = make([]int64, len(dz.sizes))
	for i, s := range dz.sizes {
		dz.offsets[i] = pos
		pos += int64(s)
	}
	return dz, nil
}

func (dz *dictzip) Close() error { return dz.f.Close() }

func (dz *dictzip) chunk(i int) ([]byte, error) {
	dz.mu.Lock()
	if b, ok := dz.cache[i]; ok {
		dz.mu.Unlock()
		return b, nil
	}
	dz.mu.Unlock()
	if i < 0 || i >= len(dz.sizes) {
		return nil, errors.New("dictzip chunk out of range")
	}
	comp := make([]byte, dz.sizes[i])
	if _, err := dz.f.ReadAt(comp, dz.offsets[i]); err != nil && err != io.EOF {
		return nil, err
	}
	fr := flate.NewReader(bytes.NewReader(comp))
	out := make([]byte, 0, dz.chunkLen)
	buf := bytes.NewBuffer(out)
	_, err := io.CopyN(buf, fr, int64(dz.chunkLen))
	fr.Close()
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	b := buf.Bytes()
	dz.mu.Lock()
	if len(dz.cache) > 64 {
		dz.cache = map[int][]byte{}
	}
	dz.cache[i] = b
	dz.mu.Unlock()
	return b, nil
}

// ReadAt reads size bytes starting at the uncompressed offset off.
func (dz *dictzip) ReadAt(off int64, size int) ([]byte, error) {
	out := make([]byte, 0, size)
	for size > 0 {
		ci := int(off / int64(dz.chunkLen))
		co := int(off % int64(dz.chunkLen))
		b, err := dz.chunk(ci)
		if err != nil {
			return nil, err
		}
		if co >= len(b) {
			break
		}
		n := len(b) - co
		if n > size {
			n = size
		}
		out = append(out, b[co:co+n]...)
		off += int64(n)
		size -= n
	}
	return out, nil
}
