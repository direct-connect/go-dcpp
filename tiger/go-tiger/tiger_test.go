package tiger

import (
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"testing"
)

type testVector struct {
	data string
	hash string
}

// Test vectors from the tigermain.c in http://klondike.es/freetiger/
func TestTiger(T *testing.T) {
	T.Parallel()

	t := New()
	for i, vec := range []testVector{
		{"",
			"3293ac630c13f0245f92bbb1766e16167a4e58492dde73f3"},
		{"abc",
			"2aab1484e8c158f2bfb8c5ff41b57a525129131c957b5f93"},
		{"Tiger",
			"dd00230799f5009fec6debc838bb6a27df2b9d6f110c7937"},
		{"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+-",
			"f71c8583902afb879edfe610f82c0d4786a3a534504486b5"},
		{"ABCDEFGHIJKLMNOPQRSTUVWXYZ=abcdefghijklmnopqrstuvwxyz+0123456789",
			"48ceeb6308b87d46e95d656112cdf18d97915f9765658957"},
		{"Tiger - A Fast New Hash Function, by Ross Anderson and Eli Biham",
			"8a866829040a410c729ad23f5ada711603b3cdd357e4c15e"},
		{"Tiger - A Fast New Hash Function, by Ross Anderson and Eli Biham, proceedings of Fast Software Encryption 3, Cambridge.",
			"ce55a6afd591f5ebac547ff84f89227f9331dab0b611c889"},
		{"Tiger - A Fast New Hash Function, by Ross Anderson and Eli Biham, proceedings of Fast Software Encryption 3, Cambridge, 1996.",
			"631abdd103eb9a3d245b6dfd4d77b257fc7439501d1568dd"},
		{"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+-ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+-",
			"c54034e5b43eb8005848a7e0ae6aac76e4ff590ae715fd25"},
	} {
		t.Reset()
		io.Copy(t, strings.NewReader(vec.data))
		switch sum := hex.EncodeToString(t.Sum(nil)); sum {
		case vec.hash:
			// OK
		default:
			T.Errorf("Vector %d: Wanted %q, got %q", i+1, vec.hash, sum)
		}
	}
}

// Checks if write-after-sum works
func TestTiger_sum(T *testing.T) {
	T.Parallel()

	t := New()
	for i, vec := range []testVector{
		{"Tiger - A Fast New Hash Function, by Ross Anderson and Eli Biham",
			"8a866829040a410c729ad23f5ada711603b3cdd357e4c15e"},
		{", proceedings of Fast Software Encryption 3, Cambridge.",
			"ce55a6afd591f5ebac547ff84f89227f9331dab0b611c889"},
	} {
		io.Copy(t, strings.NewReader(vec.data))
		switch sum := hex.EncodeToString(t.Sum(nil)); sum {
		case vec.hash:
			// OK
		default:
			T.Errorf("Vector %d: Wanted %q, got %q", i+1, vec.hash, sum)
		}
	}
}

// Some arbitrarily large block
func TestTiger_64k(T *testing.T) {
	T.Parallel()

	t := New()

	buf := [65536]byte{}
	for i := range buf {
		buf[i] = byte(i & 0xff)
	}
	t.Write(buf[:])

	switch sum := hex.EncodeToString(t.Sum(nil)); sum {
	case "fdf4f5b35139f48e710e421be5af411de1a8aac333f26204":
		// OK
	default:
		T.Errorf("Wanted %#x, got %#x",
			"fdf4f5b35139f48e710e421be5af411de1a8aac333f26204",
			sum)
	}
}

func ExampleTiger() {
	t := New()
	t.Write([]byte("The quick brown fox jumps over the lazy dog"))
	fmt.Println("Hash:", hex.EncodeToString(t.Sum(nil)))

	// Output: Hash: 6d12a41e72e644f017b6f0e2f7b44c6285f06dd5d2c5b075
}

func ExampleTiger2() {
	t := New2()
	t.Write([]byte("The quick brown fox jumps over the lazy dog"))
	fmt.Println("Hash:", hex.EncodeToString(t.Sum(nil)))

	// Output: Hash: 976abff8062a2e9dcea3a1ace966ed9c19cb85558b4976d8
}

type FakeReader struct{}

func (_ *FakeReader) Read(b []byte) (n int, err error) {
	return len(b), nil
}

func BenchmarkTiger_1000MB(B *testing.B) {
	sz := int64(1000 * 1024 * 1024)
	B.SetBytes(sz)

	buf := &FakeReader{}
	t := New()

	B.ResetTimer()
	for i := 0; i < B.N; i++ {
		io.CopyN(t, buf, sz)
		t.Sum(nil)
	}
}
