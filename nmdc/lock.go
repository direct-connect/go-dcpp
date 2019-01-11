package nmdc

import "bytes"

var keyReplace = map[byte]string{
	0:   "/%DCN000%/",
	5:   "/%DCN005%/",
	36:  "/%DCN036%/",
	96:  "/%DCN096%/",
	124: "/%DCN124%/",
	126: "/%DCN126%/",
}

func Unlock(str string) string {
	lock := []byte(str)

	n := len(lock)
	key := make([]byte, n)

	for i := 1; i < n; i++ {
		key[i] = (lock[i] ^ lock[i-1]) & 0xFF
	}
	key[0] = byte((((lock[0] ^ lock[n-1]) ^ lock[n-2]) ^ 5) & 0xFF)
	for i := 0; i < n; i++ {
		key[i] = byte((((key[i] << 4) & 0xF0) | ((key[i] >> 4) & 0x0F)) & 0xFF)
	}
	buf := bytes.NewBuffer(nil)
	buf.Grow(len(key))
	for _, v := range key {
		if esc, ok := keyReplace[v]; ok {
			buf.WriteString(esc)
		} else {
			buf.WriteByte(v)
		}
	}
	return buf.String()
}
