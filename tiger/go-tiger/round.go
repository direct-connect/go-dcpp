package tiger

func tigerPass(a, b, c uint64, x [8]uint64, mul uint64) (new_a, new_b, new_c uint64) {
	a, b, c = tigerRound(a, b, c, x[0], mul)
	b, c, a = tigerRound(b, c, a, x[1], mul)
	c, a, b = tigerRound(c, a, b, x[2], mul)
	a, b, c = tigerRound(a, b, c, x[3], mul)
	b, c, a = tigerRound(b, c, a, x[4], mul)
	c, a, b = tigerRound(c, a, b, x[5], mul)
	a, b, c = tigerRound(a, b, c, x[6], mul)
	b, c, a = tigerRound(b, c, a, x[7], mul)

	return a, b, c
}

func (t *tiger) tigerBlock(x [8]uint64) {
	// no need to save abc

	// first pass
	a, b, c := tigerPass(t.a, t.b, t.c, x, 5)

	// second pass
	x = keySchedule(x)
	c, a, b = tigerPass(c, a, b, x, 7)

	// third pass
	x = keySchedule(x)
	b, c, a = tigerPass(b, c, a, x, 9)

	// feed_forward
	t.a = a ^ t.a
	t.b = b - t.b
	t.c = c + t.c
}
