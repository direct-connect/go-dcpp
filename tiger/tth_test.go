package tiger_test

import (
	"bytes"
	"testing"

	"github.com/dennwc/go-dcpp/tiger"
)

var tthCases = []struct {
	data []byte
	hash string
}{
	{
		[]byte{},
		`LWPNACQDBZRYXW3VHJVCJ64QBZNGHOHHHZWCLNQ`,
	},
	{
		bytes.Repeat([]byte{'a'}, 1),
		`CZQUWH3IYXBF5L3BGYUGZHASSMXU647IP2IKE4Y`,
	},
	{
		bytes.Repeat([]byte{'a'}, 5),
		`ELXBTR33AWAAEKEVWRXEQ3446IL7KGCTXMWA4AA`,
	},
	{
		bytes.Repeat([]byte{'a'}, 24),
		`K56WCQPI62DYXXDY4AZ7LRUFDQOTIZRAPEKRTRI`,
	},
	{
		bytes.Repeat([]byte{'a'}, 25),
		`BNCXPH7SJ5Z4HTKEYMJXFL7QJUXLZFZM4JDRQYY`,
	},
	{
		bytes.Repeat([]byte{'a'}, 64),
		`LKOML52BOHG43N2P5MNZ3BDIAKNYO3C22WQMJGI`,
	},
	{
		bytes.Repeat([]byte{'a'}, 100),
		`MI3GUSIV63KCZS4IL3PEZD6AQADVO6CMKPITPTA`,
	},
	{
		bytes.Repeat([]byte{'a'}, 127),
		`YKSDLGFJM7HNVU3ESUVCOT4JGPB2NWL3WIMPLZA`,
	},
	{
		bytes.Repeat([]byte{'a'}, 128),
		`3ZTFBW4Y65OGGNXCM776DYN5WJ6SZLWR7WMC4NA`,
	},
	{
		bytes.Repeat([]byte{'a'}, 256),
		`ZZK5ZBTLKGLY7SFWEHY5VGYYDQHZG56NIUQ6IXI`,
	},
	{
		bytes.Repeat([]byte{'a'}, 1022),
		`PT2BL57H4JJ5LHXBDA6CJ5KEOO5XEKNIFYINE7I`,
	},
	{
		bytes.Repeat([]byte{'a'}, 1023),
		`YBJDV4HQU6LDJZMP36DEUZ7MMNXA6TBLMOX55PI`,
	},
	{
		bytes.Repeat([]byte{'a'}, 1024),
		`BR4BVJBMHDFVCFI4WBPSL63W5TWXWVBSC574BLI`,
	},
	{
		bytes.Repeat([]byte{'a'}, 1025),
		`CDYY2OW6F6DTGCH3Q6NMSDLSRV7PNMAL3CED3DA`,
	},
}

func TestTTH(t *testing.T) {
	for i, c := range tthCases {
		tr, err := tiger.TreeHash(bytes.NewReader(c.data))
		if err != nil {
			t.Fatal(err)
		} else if c.hash != tr.String() {
			t.Errorf("wrong hash on %d: %s vs %s", i+1, c.hash, tr)
		}
	}
}
