package tiger

func keySchedule(x [8]uint64) (new_x [8]uint64) {
	x[0] -= x[7] ^ 0xA5A5A5A5A5A5A5A5
	x[1] ^= x[0]
	x[2] += x[1]
	x[3] -= x[2] ^ ((^x[1]) << 19)
	x[4] ^= x[3]
	x[5] += x[4]
	x[6] -= x[5] ^ ((^x[4]) >> 23)
	x[7] ^= x[6]
	x[0] += x[7]
	x[1] -= x[0] ^ ((^x[7]) << 19)
	x[2] ^= x[1]
	x[3] += x[2]
	x[4] -= x[3] ^ ((^x[2]) >> 23)
	x[5] ^= x[4]
	x[6] += x[5]
	x[7] -= x[6] ^ 0x123456789ABCDEF
	return x
}
