// Implements the Tiger/192 hash function as specified in http://www.cs.technion.ac.il/~biham/Reports/Tiger/tiger/tiger.html
//
// Tiger/160 and Tiger/128 are simply truncations of the Tiger/192 sum, so there's no specific implementation for those.
package tiger

import (
	"bytes"
	"encoding/binary"
	"hash"
)

type tiger struct {
	padding byte // 0x01 on Tiger, 0x80 on Tiger2

	a, b, c uint64

	// size of digest
	sz uint64

	buf []byte
}

// Returns a new hash.Hash that calculates the Tiger/192 hash digest.
func New() hash.Hash {
	t := &tiger{padding: 0x01}
	t.Reset()
	return t
}

// Returns a new hash.Hash that calculates the Tiger2/192 hash digest.
//
// Tiger2 is exactly the same as Tiger but with a different padding scheme: while Tiger uses MD4's scheme
// of a 0x01 byte followed by zeros, Tiger2 uses MD5's scheme of a 0x80 byte followed by zeros.
func New2() hash.Hash {
	t := &tiger{padding: 0x80}
	t.Reset()
	return t
}

const (
	BlockSize = 64 // 512 bits
	Size      = 24 // 192 bits
)

func (t *tiger) BlockSize() int {
	return BlockSize
}

func (t *tiger) Size() int {
	return Size
}

func (t *tiger) Reset() {
	t.buf = nil
	t.a = 0x0123456789ABCDEF
	t.b = 0xFEDCBA9876543210
	t.c = 0xF096A5B4C3B2E187
	t.sz = 0
}

func (t *tiger) Write(p []byte) (n int, err error) {
	n, err = len(p), nil
	t.sz += uint64(n)

	p = t.readFrom(p)

	if len(p) > 0 {
		t.buf = t.readFrom(append(t.buf, p...))
	}

	return
}

func (t *tiger) Sum(b []byte) []byte {
	s := *t

	// make a copy of the write buffer so we don't overwrite the live struct's buffer
	sbuf := make([]byte, len(s.buf))
	copy(sbuf, s.buf)
	s.buf = sbuf

	sz := s.sz
	pad := [BlockSize]byte{s.padding, 0 /* ... */}
	if len(s.buf) < 56 {
		s.Write(pad[:56-len(s.buf)])
	} else {
		s.Write(pad[:BlockSize+(56-len(s.buf))])
	}

	// length of entire message in bits
	binary.Write(&s, binary.LittleEndian, sz<<3)

	if len(s.buf) > 0 {
		panic("leftover bytes in buffer")
	}

	// I swear I'm not sure if this is *really* supposed to be LittleEndian or BigEndian
	// LittleEndian matches the test vectors though so...
	buf := bytes.NewBuffer(make([]byte, 0, Size))
	binary.Write(buf, binary.LittleEndian, s.a)
	binary.Write(buf, binary.LittleEndian, s.b)
	binary.Write(buf, binary.LittleEndian, s.c)
	return append(b, buf.Bytes()...)
}
