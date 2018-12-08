// +build !safe

package tiger

import (
	"unsafe"
)

// Uses unsafe to pun the c uint64 into an array of its bytes.
// Overall benchmark 110.49 MB/s.
func tigerRound(a, b, c, x, mul uint64) (new_a, new_b, new_c uint64) {
	c ^= x
	cb := [8]byte(*(*[8]byte)(unsafe.Pointer(&c)))

	a -= sBox[0][cb[0]] ^ sBox[1][cb[2]] ^ sBox[2][cb[4]] ^ sBox[3][cb[6]]
	b += sBox[3][cb[1]] ^ sBox[2][cb[3]] ^ sBox[1][cb[5]] ^ sBox[0][cb[7]]
	b *= mul

	return a, b, c
}
