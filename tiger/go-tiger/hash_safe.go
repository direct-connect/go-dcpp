// +build safe

package tiger

import (
	"bytes"
	"encoding/binary"
)

// Splits the block into uint64s and runs the hash.
//
// Uses encoding/binary for safety.
// Overall benchmark time 38.09 MB/s.
func (t *tiger) readFrom(b []byte) []byte {
	buf := bytes.NewBuffer(b)
	for buf.Len() >= BlockSize {
		x := [8]uint64{}

		// NOTE: Ideally, this would be bytes.NativeEndian, but we don't have that.
		// All supported platforms are LE (...and ARM) so this works out fine.
		binary.Read(buf, binary.LittleEndian, x[:])
		t.tigerBlock(x)
	}
	return buf.Bytes()
}
