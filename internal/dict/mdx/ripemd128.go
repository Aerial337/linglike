package mdx

import (
	"encoding/binary"
	"math/bits"
)

// ripemd128 computes the RIPEMD-128 digest of msg. MDict derives the key
// block encryption key from it.
func ripemd128(msg []byte) []byte {
	var (
		r  = [64]int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 7, 4, 13, 1, 10, 6, 15, 3, 12, 0, 9, 5, 2, 14, 11, 8, 3, 10, 14, 4, 9, 15, 8, 1, 2, 7, 0, 6, 13, 11, 5, 12, 1, 9, 11, 10, 0, 8, 12, 4, 13, 3, 7, 15, 14, 5, 6, 2}
		rp = [64]int{5, 14, 7, 0, 9, 2, 11, 4, 13, 6, 15, 8, 1, 10, 3, 12, 6, 11, 3, 7, 0, 13, 5, 10, 14, 15, 8, 12, 4, 9, 1, 2, 15, 5, 1, 3, 7, 14, 6, 9, 11, 8, 12, 2, 10, 0, 4, 13, 8, 6, 4, 1, 3, 11, 15, 0, 5, 12, 2, 13, 9, 7, 10, 14}
		s  = [64]int{11, 14, 15, 12, 5, 8, 7, 9, 11, 13, 14, 15, 6, 7, 9, 8, 7, 6, 8, 13, 11, 9, 7, 15, 7, 12, 15, 9, 11, 7, 13, 12, 11, 13, 6, 7, 14, 9, 13, 15, 14, 8, 13, 6, 5, 12, 7, 5, 11, 12, 14, 15, 14, 15, 9, 8, 9, 14, 5, 6, 8, 6, 5, 12}
		sp = [64]int{8, 9, 9, 11, 13, 15, 15, 5, 7, 7, 8, 11, 14, 14, 12, 6, 9, 13, 15, 7, 12, 8, 9, 11, 7, 7, 12, 7, 6, 15, 13, 11, 9, 7, 15, 11, 8, 6, 6, 14, 12, 13, 5, 14, 13, 13, 7, 5, 15, 5, 8, 11, 14, 14, 6, 14, 6, 9, 12, 9, 12, 5, 15, 8}
	)
	f := func(j int, x, y, z uint32) uint32 {
		switch {
		case j < 16:
			return x ^ y ^ z
		case j < 32:
			return (x & y) | (^x & z)
		case j < 48:
			return (x | ^y) ^ z
		default:
			return (x & z) | (y & ^z)
		}
	}
	K := func(j int) uint32 {
		switch {
		case j < 16:
			return 0
		case j < 32:
			return 0x5a827999
		case j < 48:
			return 0x6ed9eba1
		default:
			return 0x8f1bbcdc
		}
	}
	Kp := func(j int) uint32 {
		switch {
		case j < 16:
			return 0x50a28be6
		case j < 32:
			return 0x5c4dd124
		case j < 48:
			return 0x6d703ef3
		default:
			return 0
		}
	}
	// padding
	origLen := len(msg)
	padLen := 64 - (((origLen-56)%64)+64)%64 // 1..64, as in the reference
	m := make([]byte, 0, origLen+padLen+8)
	m = append(m, msg...)
	m = append(m, 0x80)
	for i := 1; i < padLen; i++ {
		m = append(m, 0)
	}
	m = binary.LittleEndian.AppendUint64(m, uint64(origLen)*8)

	h0, h1, h2, h3 := uint32(0x67452301), uint32(0xefcdab89), uint32(0x98badcfe), uint32(0x10325476)
	for blk := 0; blk+64 <= len(m); blk += 64 {
		var X [16]uint32
		for i := 0; i < 16; i++ {
			X[i] = binary.LittleEndian.Uint32(m[blk+4*i:])
		}
		A, B, C, D := h0, h1, h2, h3
		Ap, Bp, Cp, Dp := h0, h1, h2, h3
		for j := 0; j < 64; j++ {
			T := bits.RotateLeft32(A+f(j, B, C, D)+X[r[j]]+K(j), s[j])
			A, D, C, B = D, C, B, T
			T = bits.RotateLeft32(Ap+f(63-j, Bp, Cp, Dp)+X[rp[j]]+Kp(j), sp[j])
			Ap, Dp, Cp, Bp = Dp, Cp, Bp, T
		}
		T := h1 + C + Dp
		h1 = h2 + D + Ap
		h2 = h3 + A + Bp
		h3 = h0 + B + Cp
		h0 = T
	}
	out := make([]byte, 0, 16)
	out = binary.LittleEndian.AppendUint32(out, h0)
	out = binary.LittleEndian.AppendUint32(out, h1)
	out = binary.LittleEndian.AppendUint32(out, h2)
	out = binary.LittleEndian.AppendUint32(out, h3)
	return out
}

// fastDecrypt reverses MDict's simple key-info block obfuscation.
func fastDecrypt(data, key []byte) []byte {
	b := make([]byte, len(data))
	prev := byte(0x36)
	for i := range data {
		t := (data[i] >> 4) | (data[i] << 4)
		t = t ^ prev ^ byte(i) ^ key[i%len(key)]
		prev = data[i]
		b[i] = t
	}
	return b
}
