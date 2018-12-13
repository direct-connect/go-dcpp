package adc_test

import (
	"reflect"
	"testing"

	"github.com/dennwc/go-dcpp/adc"
)

var casesDecode = []struct {
	name   string
	data   string
	targ   interface{}
	expect interface{}
}{
	{
		"user",
		`NIgopher SF34 FS0 SUGCON,ADC0 SS39542721391 VEGoConn\s0.01 SL3 IDHVBNEMDCTKCD4V3N54X4MMOVLJLJL6PSKVHFXHI I4172.17.42.1`,
		&adc.User{},
		&adc.User{
			Id:         adc.MustParseCID(`HVBNEMDCTKCD4V3N54X4MMOVLJLJL6PSKVHFXHI`),
			Name:       "gopher",
			Ip4:        "172.17.42.1",
			ShareFiles: 34, ShareSize: 39542721391,
			Version:  `GoConn 0.01`,
			Slots:    3,
			Features: adc.ExtFeatures{"GCON", "ADC0"},
		},
	},
	{
		"user id",
		`PDHVBNEMDCTKCD4V3N54X4MMOVLJLJL6PSKVHFXHI NIgopher SF34 FS0 SUGCON,ADC0 SS39542721391 VEGoConn\s0.01 SL3 IDHVBNEMDCTKCD4V3N54X4MMOVLJLJL6PSKVHFXHI I4172.17.42.1`,
		&adc.User{},
		&adc.User{
			Id:         adc.MustParseCID(`HVBNEMDCTKCD4V3N54X4MMOVLJLJL6PSKVHFXHI`),
			Pid:        adc.MustParseCIDP(`HVBNEMDCTKCD4V3N54X4MMOVLJLJL6PSKVHFXHI`),
			Name:       "gopher",
			Ip4:        "172.17.42.1",
			ShareFiles: 34, ShareSize: 39542721391,
			Version:  `GoConn 0.01`,
			Slots:    3,
			Features: adc.ExtFeatures{"GCON", "ADC0"},
		},
	},
	{
		"user pid",
		`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn SL3 FS3 SS25146919163 SF23 HR0 HO1 VEEiskaltDC++\s2.2.9 US1310720 KPSHA256/C44JWX62IN6JBAVH7NIHEZIQ6WSNQ2LHTOWYWP7ADGAYTCPZVWRQ U43000 SUSEGA,ADC0,TCP4,UDP4 I4172.17.42.1 HN11`,
		&adc.User{},
		&adc.User{
			Id:         adc.MustParseCID(`KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI`),
			Name:       "dennnn",
			Ip4:        "172.17.42.1",
			ShareFiles: 23, ShareSize: 25146919163,
			Version:   `EiskaltDC++ 2.2.9`,
			Udp4:      3000,
			MaxUpload: 1310720, Slots: 3, SlotsFree: 3,
			HubsNormal: 11, HubsOperator: 1,
			Features: adc.ExtFeatures{"SEGA", "ADC0", "TCP4", "UDP4"},
			KP:       "SHA256/C44JWX62IN6JBAVH7NIHEZIQ6WSNQ2LHTOWYWP7ADGAYTCPZVWRQ",
		},
	},
	{
		"user no name",
		`SF8416 HN18 FS1 U4 SS34815324082 HO2 SUNAT0,ADC0,SEGA`,
		&adc.User{},
		&adc.User{
			ShareFiles: 8416, ShareSize: 34815324082,
			HubsNormal: 18, HubsOperator: 2,
			SlotsFree: 1, Udp4: 0,
			Features: adc.ExtFeatures{`NAT0`, `ADC0`, `SEGA`},
		},
	},
	{
		"search res",
		`TOtok FNfilepath SI1234567 SL3`,
		&adc.SearchResult{},
		&adc.SearchResult{Path: "filepath", Size: 1234567, Slots: 3, Token: "tok"},
	},
	{
		"search par",
		`TO4171511714 ANsome ANdata GR32`,
		&adc.SearchRequest{},
		&adc.SearchRequest{And: []string{"some", "data"}, Token: "4171511714", Group: adc.ExtVideo},
	},
	{
		"search par2",
		`TO4171511714 GR32`,
		&adc.SearchRequest{},
		&adc.SearchRequest{Token: "4171511714", Group: adc.ExtVideo},
	},
	{
		"search par3",
		`TO4171511714 ANsome ANdata`,
		&adc.SearchRequest{},
		&adc.SearchRequest{And: []string{"some", "data"}, Token: "4171511714"},
	},
	{
		"rcm resp",
		`ADC/1.0 3000 1298498081`,
		&adc.ConnectRequest{},
		&adc.ConnectRequest{Proto: "ADC/1.0", Port: 3000, Token: "1298498081"},
	},
}

func TestDecode(t *testing.T) {
	for _, c := range casesDecode {
		t.Run(c.name, func(t *testing.T) {
			err := adc.Unmarshal([]byte(c.data), c.targ)
			if err != nil {
				t.Fatal(err)
			} else if !reflect.DeepEqual(c.targ, c.expect) {
				t.Fatalf("\n%#v\nvs\n%#v", c.targ, c.expect)
			}
		})
	}
}

var casesDecodeCmd = []struct {
	data   string
	expect interface{}
}{
	{
		`BINF AAAB IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`,
		adc.BroadcastPacket{
			Name: adc.MsgType{'I', 'N', 'F'},
			ID:   adc.SIDFromString("AAAB"),
			Data: []byte(`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`),
		},
	},
	{
		`BINF AAAB`,
		adc.BroadcastPacket{
			Name: adc.MsgType{'I', 'N', 'F'},
			ID:   adc.SIDFromString("AAAB"),
		},
	},
	{
		`CINF IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`,
		adc.clientCmd{
			Name: adc.MsgType{'I', 'N', 'F'},
			Raw:  []byte(`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`),
		},
	},
	{
		`IINF IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`,
		adc.InfoPacket{
			Name: adc.MsgType{'I', 'N', 'F'},
			Data: []byte(`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`),
		},
	},
	{
		`IINF`,
		adc.InfoPacket{
			Name: adc.MsgType{'I', 'N', 'F'},
		},
	},
	{
		`HINF IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`,
		adc.hubCmd{
			Name: adc.MsgType{'I', 'N', 'F'},
			Raw:  []byte(`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`),
		},
	},
	{
		`DCTM AAAA BBBB IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`,
		adc.DirectPacket{
			Name: adc.MsgType{'C', 'T', 'M'},
			ID:   adc.SIDFromString("AAAA"),
			Targ: adc.SIDFromString("BBBB"),
			Data: []byte(`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`),
		},
	},
	{
		`DCTM AAAA BBBB`,
		adc.DirectPacket{
			Name: adc.MsgType{'C', 'T', 'M'},
			ID:   adc.SIDFromString("AAAA"),
			Targ: adc.SIDFromString("BBBB"),
		},
	},
	{
		`EMSG AAAA BBBB IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`,
		adc.EchoPacket{
			Name: adc.MsgType{'M', 'S', 'G'},
			ID:   adc.SIDFromString("AAAA"),
			Targ: adc.SIDFromString("BBBB"),
			Data: []byte(`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`),
		},
	},
	{
		`UINF KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`,
		adc.UDPPacket{
			Name: adc.MsgType{'I', 'N', 'F'},
			ID:   adc.MustParseCID(`KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI`),
			Data: []byte(`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`),
		},
	},
	{
		`UINF KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI`,
		adc.UDPPacket{
			Name: adc.MsgType{'I', 'N', 'F'},
			ID:   adc.MustParseCID(`KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI`),
		},
	},
	{
		`FINF AAAB IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`,
		adc.FeaturePacket{
			Name: adc.MsgType{'I', 'N', 'F'},
			ID:   adc.SIDFromString("AAAB"),
			Data: []byte(`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`),
		},
	},
	{
		`FINF AAAB`,
		adc.FeaturePacket{
			Name: adc.MsgType{'I', 'N', 'F'},
			ID:   adc.SIDFromString("AAAB"),
		},
	},
	{
		`FINF AAAB +SEGA -NAT0 IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`,
		adc.FeaturePacket{
			Name:     adc.MsgType{'I', 'N', 'F'},
			ID:       adc.SIDFromString("AAAB"),
			Features: map[string]bool{`SEGA`: true, `NAT0`: false},
			Data:     []byte(`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`),
		},
	},
	{
		`FINF AAAB +SEGA -NAT0`,
		adc.FeaturePacket{
			Name:     adc.MsgType{'I', 'N', 'F'},
			ID:       adc.SIDFromString("AAAB"),
			Features: map[string]bool{`SEGA`: true, `NAT0`: false},
		},
	},
	{
		`DSCH ABCD BACA TO4171511714 ANsome ANdata GR32`,
		adc.DirectPacket{
			Name: adc.MsgType{'S', 'C', 'H'},
			ID:   adc.SIDFromString("ABCD"), Targ: adc.SIDFromString("BACA"),
			Data: []byte(`TO4171511714 ANsome ANdata GR32`),
		},
	},
	{
		`DSCH ABCD BACA`,
		adc.DirectPacket{
			Name: adc.MsgType{'S', 'C', 'H'},
			ID:   adc.SIDFromString("ABCD"), Targ: adc.SIDFromString("BACA"),
		},
	},
	{
		`USCH KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI ANsome ANdata GR32`,
		adc.UDPPacket{
			Name: adc.MsgType{'S', 'C', 'H'},
			ID:   adc.MustParseCID(`KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI`),
			Data: []byte(`ANsome ANdata GR32`),
		},
	},
	{
		`USCH KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI`,
		adc.UDPPacket{
			Name: adc.MsgType{'S', 'C', 'H'},
			ID:   adc.MustParseCID(`KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI`),
		},
	},
}

func TestDecodeCmd(t *testing.T) {
	for i, c := range casesDecodeCmd {
		cmd, err := adc.DecodePacket([]byte(c.data))
		if err != nil {
			t.Fatalf("case %d: %v", i+1, err)
		} else if !reflect.DeepEqual(cmd, c.expect) {
			t.Fatalf("case %d: failed:\n%#v\nvs\n%#v", i+1, cmd, c.expect)
		}
	}
}

var casesEncode = []struct {
	input  interface{}
	expect string
}{
	{
		adc.SIDFromString("AAAA"),
		"AAAA",
	},
	{
		adc.SIDFromInt(2),
		"AAAC",
	},
	{
		adc.SIDFromInt(34),
		"AABC",
	},
	{
		adc.User{
			Id:         adc.MustParseCID(`KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI`),
			Name:       "dennnn",
			Ip4:        "172.17.42.1",
			ShareFiles: 23, ShareSize: 25146919163,
			Version:  `EiskaltDC++ 2.2.9`,
			Features: adc.ExtFeatures{"SEGA", "ADC0", "TCP4", "UDP4"},
		},
		`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn I4172.17.42.1 SS25146919163 SF23 VEEiskaltDC++\s2.2.9 SUSEGA,ADC0,TCP4,UDP4`,
	},
	{
		adc.NewBroadcast(adc.MsgType{'I', 'N', 'F'}, adc.SIDFromString("ABCD"), adc.User{
			Id:   adc.MustParseCID(`KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI`),
			Pid:  adc.MustParseCIDP(`NIFFCWI5C2L5FEQYXOVECKBAMM5CFP54JHZRSWI`),
			Name: "dennnn",
			Ip4:  "172.17.42.1",
		}),
		`BINF ABCD IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI PDNIFFCWI5C2L5FEQYXOVECKBAMM5CFP54JHZRSWI NIdennnn I4172.17.42.1`,
	},
	//	{ // TODO: random map ordering issue, not critical for protocol
	//		adc.NewHubCmd(adc.MsgType{'S', 'U', 'P'}, adc.ModFeatures{"BASE": true, "TIGR": true}),
	//		`HSUP ADBASE ADTIGR`,
	//	},
	{
		adc.NewFeatureCmd(adc.MsgType{'S', 'C', 'H'}, adc.SIDFromString("ABCD"),
			map[string]bool{"SEGA": true},
			adc.SearchRequest{And: []string{"some", "data"}, Token: "4171511714", Group: adc.ExtVideo},
		),
		`FSCH ABCD +SEGA TO4171511714 ANsome ANdata GR32`,
	},
	//	{ // TODO: random map ordering issue, not critical for protocol
	//		adc.NewFeatureCmd(adc.MsgType{'S','C','H'}, adc.SIDFromString("ABCD"),
	//			map[string]bool{"SEGA":true,"NAT0":false},
	//			adc.SearchRequest{And:[]string{"some","data"}, Token:"4171511714", Group: adc.ExtVideo},
	//		),
	//		`FSCH ABCD +SEGA -NAT0 TO4171511714 ANsome ANdata GR32`,
	//	},
	{
		adc.NewFeatureCmd(adc.MsgType{'S', 'C', 'H'}, adc.SIDFromString("ABCD"),
			map[string]bool{"SEGA": true},
			nil,
		),
		`FSCH ABCD +SEGA`,
	},
	{
		adc.NewFeatureCmd(adc.MsgType{'S', 'C', 'H'}, adc.SIDFromString("ABCD"),
			nil, nil,
		),
		`FSCH ABCD`,
	},
	{
		adc.NewFeatureCmd(adc.MsgType{'S', 'C', 'H'}, adc.SIDFromString("ABCD"),
			nil,
			adc.SearchRequest{And: []string{"some", "data"}, Token: "4171511714", Group: adc.ExtVideo},
		),
		`FSCH ABCD TO4171511714 ANsome ANdata GR32`,
	},
	{
		adc.NewDirectCmd(adc.MsgType{'S', 'C', 'H'},
			adc.SIDFromString("ABCD"), adc.SIDFromString("BACA"),
			adc.SearchRequest{And: []string{"some", "data"}, Token: "4171511714", Group: adc.ExtVideo},
		),
		`DSCH ABCD BACA TO4171511714 ANsome ANdata GR32`,
	},
	{
		adc.NewDirectCmd(adc.MsgType{'S', 'C', 'H'},
			adc.SIDFromString("ABCD"), adc.SIDFromString("BACA"),
			nil,
		),
		`DSCH ABCD BACA`,
	},
	{
		adc.UDPPacket{
			Name: adc.MsgType{'S', 'C', 'H'},
			ID:   adc.MustParseCID(`KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI`),
			Data: []byte(`ANsome ANdata GR32`),
		},
		`USCH KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI ANsome ANdata GR32`,
	},
	{
		adc.UDPPacket{
			Name: adc.MsgType{'S', 'C', 'H'},
			ID:   adc.MustParseCID(`KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI`),
		},
		`USCH KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI`,
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
		adc.NewDirectCmd(
			adc.MsgType{'R', 'C', 'M'},
			adc.SIDFromString("ABCD"),
			adc.SIDFromString("BACA"),
			adc.RevConnectRequest{`ADC/1.0`, `12345678`},
		),
		`DRCM ABCD BACA ADC/1.0 12345678`,
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
