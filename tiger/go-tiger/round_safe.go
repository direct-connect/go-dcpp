// +build safe

package tiger

// Does the round in the proper, mathy way.
// Overall benchmark 105.44 MB/s.
func tigerRound(a, b, c, x, mul uint64) (new_a, new_b, new_c uint64) {
	c ^= x
	cb := [8]byte{
		byte((c >> (0 * 8)) & 0xff), byte((c >> (1 * 8)) & 0xff),
		byte((c >> (2 * 8)) & 0xff), byte((c >> (3 * 8)) & 0xff),
		byte((c >> (4 * 8)) & 0xff), byte((c >> (5 * 8)) & 0xff),
		byte((c >> (6 * 8)) & 0xff), byte((c >> (7 * 8)) & 0xff),
	}

	a -= sBox[0][cb[0]] ^ sBox[1][cb[2]] ^ sBox[2][cb[4]] ^ sBox[3][cb[6]]
	b += sBox[3][cb[1]] ^ sBox[2][cb[3]] ^ sBox[1][cb[5]] ^ sBox[0][cb[7]]
	b *= mul

	return a, b, c
}
