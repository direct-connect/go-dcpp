package adc_test

import (
	"reflect"
	"testing"

	"github.com/direct-connect/go-dcpp/adc"
	"github.com/direct-connect/go-dcpp/adc/types"
	"github.com/direct-connect/go-dcpp/tiger"
)

var casesDecode = []struct {
	name   string
	data   string
	expect interface{}
}{
	{
		"user",
		`NIgopher SF34 FS0 SUGCON,ADC0 SS39542721391 VEGoConn\s0.01 SL3 IDHVBNEMDCTKCD4V3N54X4MMOVLJLJL6PSKVHFXHI I4172.17.42.1`,
		&adc.User{
			Id:         types.MustParseCID(`HVBNEMDCTKCD4V3N54X4MMOVLJLJL6PSKVHFXHI`),
			Name:       "gopher",
			Ip4:        "172.17.42.1",
			ShareFiles: 34, ShareSize: 39542721391,
			Version:  `GoConn 0.01`,
			Slots:    3,
			Features: adc.ExtFeatures{{'G', 'C', 'O', 'N'}, {'A', 'D', 'C', '0'}},
		},
	},
	{
		"user id",
		`PDHVBNEMDCTKCD4V3N54X4MMOVLJLJL6PSKVHFXHI NIgopher SF34 FS0 SUGCON,ADC0 SS39542721391 VEGoConn\s0.01 SL3 IDHVBNEMDCTKCD4V3N54X4MMOVLJLJL6PSKVHFXHI I4172.17.42.1`,
		&adc.User{
			Id:         types.MustParseCID(`HVBNEMDCTKCD4V3N54X4MMOVLJLJL6PSKVHFXHI`),
			Pid:        types.MustParseCIDP(`HVBNEMDCTKCD4V3N54X4MMOVLJLJL6PSKVHFXHI`),
			Name:       "gopher",
			Ip4:        "172.17.42.1",
			ShareFiles: 34, ShareSize: 39542721391,
			Version:  `GoConn 0.01`,
			Slots:    3,
			Features: adc.ExtFeatures{{'G', 'C', 'O', 'N'}, {'A', 'D', 'C', '0'}},
		},
	},
	{
		"user pid",
		`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn SL3 FS3 SS25146919163 SF23 HR0 HO1 VEEiskaltDC++\s2.2.9 US1310720 KPSHA256/C44JWX62IN6JBAVH7NIHEZIQ6WSNQ2LHTOWYWP7ADGAYTCPZVWRQ U43000 SUSEGA,ADC0,TCP4,UDP4 I4172.17.42.1 HN11`,
		&adc.User{
			Id:         types.MustParseCID(`KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI`),
			Name:       "dennnn",
			Ip4:        "172.17.42.1",
			ShareFiles: 23, ShareSize: 25146919163,
			Version:   `EiskaltDC++ 2.2.9`,
			Udp4:      3000,
			MaxUpload: "1310720", Slots: 3, SlotsFree: 3,
			HubsNormal: 11, HubsOperator: 1,
			Features: adc.ExtFeatures{{'S', 'E', 'G', 'A'}, {'A', 'D', 'C', '0'}, {'T', 'C', 'P', '4'}, {'U', 'D', 'P', '4'}},
			KP:       "SHA256/C44JWX62IN6JBAVH7NIHEZIQ6WSNQ2LHTOWYWP7ADGAYTCPZVWRQ",
		},
	},
	{
		"user no name",
		`SF8416 HN18 FS1 U4 SS34815324082 HO2 SUNAT0,ADC0,SEGA`,
		&adc.User{
			ShareFiles: 8416, ShareSize: 34815324082,
			HubsNormal: 18, HubsOperator: 2,
			SlotsFree: 1, Udp4: 0,
			Features: adc.ExtFeatures{{'N', 'A', 'T', '0'}, {'A', 'D', 'C', '0'}, {'S', 'E', 'G', 'A'}},
		},
	},
	{
		"get password",
		`AAAQEAYEAUDAOCAJAAAQEAYCAMCAKBQHBAEQAAI`,
		&adc.GetPassword{Salt: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0, 1, 2, 3, 2, 3, 4, 5, 6, 7, 8, 9, 0, 1}},
	},
	{
		"password",
		`ABZCJESSJKVMIL2BDERHSJ7RF5IYI6ZX2QAOQGI`,
		&adc.Password{Hash: tiger.HashBytes([]byte("qwerty"))},
	},
	{
		"search res",
		`TOtok FNfilepath SI1234567 SL3`,
		&adc.SearchResult{Path: "filepath", Size: 1234567, Slots: 3, Token: "tok"},
	},
	{
		"search par",
		`TO4171511714 ANsome ANdata GR32`,
		&adc.SearchRequest{And: []string{"some", "data"}, Token: "4171511714", Group: adc.ExtVideo},
	},
	{
		"search par2",
		`TO4171511714 GR32`,
		&adc.SearchRequest{Token: "4171511714", Group: adc.ExtVideo},
	},
	{
		"search par3",
		`TO4171511714 ANsome ANdata`,
		&adc.SearchRequest{And: []string{"some", "data"}, Token: "4171511714"},
	},
	{
		"rcm resp",
		`ADC/1.0 3000 1298498081`,
		&adc.ConnectRequest{Proto: "ADC/1.0", Port: 3000, Token: "1298498081"},
	},
	{
		"msg",
		`some\stext`,
		&adc.ChatMessage{Text: "some text"},
	},
	{
		"pm",
		`some\stext PMAAAB`,
		&adc.ChatMessage{Text: "some text", PM: sidp("AAAB")},
	},
	{
		"user command",
		`ADCH++/Hub\smanagement/Reload\sscripts TTHMSG\s+reload\n CT3`,
		&adc.UserCommand{
			Name:     "ADCH++/Hub management/Reload scripts",
			Command:  "HMSG +reload",
			Category: 3,
		},
	},
}

func sidp(s string) *types.SID {
	v := types.SIDFromString(s)
	return &v
}

func TestDecode(t *testing.T) {
	for _, c := range casesDecode {
		t.Run(c.name, func(t *testing.T) {
			targ := reflect.New(reflect.TypeOf(c.expect).Elem()).Interface()
			err := adc.Unmarshal([]byte(c.data), targ)
			if err != nil {
				t.Fatal(err)
			} else if !reflect.DeepEqual(targ, c.expect) {
				t.Fatalf("\n%#v\nvs\n%#v", targ, c.expect)
			}
		})
	}
}

var casesEncode = []struct {
	input  interface{}
	expect string
}{
	{
		types.SIDFromString("AAAA"),
		"AAAA",
	},
	{
		types.SIDFromInt(2),
		"AAAC",
	},
	{
		types.SIDFromInt(34),
		"AABC",
	},
	{
		adc.User{
			Id:         types.MustParseCID(`KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI`),
			Name:       "dennnn",
			Ip4:        "172.17.42.1",
			ShareFiles: 23, ShareSize: 25146919163,
			Version:  `EiskaltDC++ 2.2.9`,
			Features: adc.ExtFeatures{{'S', 'E', 'G', 'A'}, {'A', 'D', 'C', '0'}, {'T', 'C', 'P', '4'}, {'U', 'D', 'P', '4'}},
		},
		`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn I4172.17.42.1 SS25146919163 SF23 VEEiskaltDC++\s2.2.9 SL0 FS0 HN0 HR0 HO0 SUSEGA,ADC0,TCP4,UDP4`,
	},
	{
		adc.GetPassword{Salt: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0, 1, 2, 3, 2, 3, 4, 5, 6, 7, 8, 9, 0, 1}},
		`AAAQEAYEAUDAOCAJAAAQEAYCAMCAKBQHBAEQAAI`,
	},
	{
		adc.Password{Hash: tiger.HashBytes([]byte("qwerty"))},
		`ABZCJESSJKVMIL2BDERHSJ7RF5IYI6ZX2QAOQGI`,
	},
	{
		adc.SearchRequest{And: []string{"some", "data"}, Token: "4171511714", Group: adc.ExtVideo},
		`TO4171511714 ANsome ANdata GR32`,
	},
	{
		adc.SearchResult{Path: "filepath", Size: 1234567, Slots: 3, Token: "tok"},
		`TOtok FNfilepath SI1234567 SL3`,
	},
	{
		adc.SearchRequest{And: []string{"some", "data"}, Token: "4171511714", Group: adc.ExtVideo},
		`TO4171511714 ANsome ANdata GR32`,
	},
	{
		adc.SearchRequest{Token: "4171511714", Group: adc.ExtVideo},
		`TO4171511714 GR32`,
	},
	{
		adc.SearchRequest{And: []string{"some", "data"}, Token: "4171511714"},
		`TO4171511714 ANsome ANdata`,
	},
	{
		adc.RevConnectRequest{Proto: `ADC/1.0`, Token: `12345678`},
		`ADC/1.0 12345678`,
	},
	{
		adc.UserCommand{
			Name:     "ADCH++/Hub management/Reload scripts",
			Command:  "HMSG +reload",
			Category: 3,
		},
		`ADCH++/Hub\smanagement/Reload\sscripts TTHMSG\s+reload\n CT3`,
	},
}

func TestEncode(t *testing.T) {
	for i, c := range casesEncode {
		data, err := adc.Marshal(c.input)
		if err != nil {
			t.Fatalf("case %d: %v", i+1, err)
		} else if string(data) != c.expect {
			t.Fatalf("case %d: failed:\n%#v\nvs\n%#v", i+1, string(data), c.expect)
		}
	}
}
