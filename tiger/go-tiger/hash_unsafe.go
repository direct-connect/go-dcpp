// +build !safe

package tiger

import (
	"unsafe"
)

// Splits the block into uint64s and runs the hash.
//
// uses unsafe for fast conversion from []byte to []uint64.
// Overall benchmark time 104.62 MB/s.
func (t *tiger) readFrom(buf []byte) []byte {
	for len(buf) >= BlockSize {
		// Pun the byte slice into an uint64 slice
		// We don't use its len directly as it'll be incorrect,
		// but we already know it has enough for the iteration.
		ptr := unsafe.Pointer(&buf)
		buf64 := []uint64(*(*[]uint64)(ptr))

		// Read the uint64 slice into an array
		x := [8]uint64{}
		for i := range x {
			x[i] = buf64[i]
		}
		buf = buf[BlockSize:]
		t.tigerBlock(x)
	}
	return buf
}
