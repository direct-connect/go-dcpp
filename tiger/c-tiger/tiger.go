package tiger

//typedef unsigned long long int word64;
//typedef unsigned char byte;
//void tiger(byte*, word64, word64*);
//void tigerh(word64 p, word64 n, word64 res) { tiger((byte*)p,n,(word64*)res); }
import "C"
import "unsafe"

type TH [24]byte

func Tiger(data []byte) (out TH) {
	res := out[:]
	C.tigerh(
		C.word64(uintptr(unsafe.Pointer(&data[0]))),
		C.word64(uintptr(len(data))),
		C.word64(uintptr(unsafe.Pointer(&res[0]))),
	)
	return
}
