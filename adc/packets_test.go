package adc_test

import (
	"bytes"
	"reflect"
	"testing"

	. "github.com/dennwc/go-dcpp/adc"
	"github.com/dennwc/go-dcpp/adc/types"
)

var casesPackets = []struct {
	data   string
	packet Packet
}{
	{
		`BINF AAAB IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`,
		&BroadcastPacket{
			ID: types.SIDFromString("AAAB"),
			BasePacket: BasePacket{
				Name: MsgType{'I', 'N', 'F'},
				Data: []byte(`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`),
			},
		},
	},
	{
		`BINF AAAB`,
		&BroadcastPacket{
			BasePacket: BasePacket{
				Name: MsgType{'I', 'N', 'F'},
			},
			ID: types.SIDFromString("AAAB"),
		},
	},
	{
		`CINF IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`,
		&ClientPacket{
			BasePacket: BasePacket{
				Name: MsgType{'I', 'N', 'F'},
				Data: []byte(`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`),
			},
		},
	},
	{
		`IINF IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`,
		&InfoPacket{
			BasePacket: BasePacket{
				Name: MsgType{'I', 'N', 'F'},
				Data: []byte(`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`),
			},
		},
	},
	{
		`IINF`,
		&InfoPacket{
			BasePacket: BasePacket{
				Name: MsgType{'I', 'N', 'F'},
			},
		},
	},
	{
		`HINF IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`,
		&HubPacket{
			BasePacket: BasePacket{
				Name: MsgType{'I', 'N', 'F'},
				Data: []byte(`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`),
			},
		},
	},
	{
		`DCTM AAAA BBBB IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`,
		&DirectPacket{
			ID:   types.SIDFromString("AAAA"),
			Targ: types.SIDFromString("BBBB"),
			BasePacket: BasePacket{
				Name: MsgType{'C', 'T', 'M'},
				Data: []byte(`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`),
			},
		},
	},
	{
		`DCTM AAAA BBBB`,
		&DirectPacket{
			ID:   types.SIDFromString("AAAA"),
			Targ: types.SIDFromString("BBBB"),
			BasePacket: BasePacket{
				Name: MsgType{'C', 'T', 'M'},
			},
		},
	},
	{
		`EMSG AAAA BBBB IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`,
		&EchoPacket{
			ID:   types.SIDFromString("AAAA"),
			Targ: types.SIDFromString("BBBB"),
			BasePacket: BasePacket{
				Name: MsgType{'M', 'S', 'G'},
				Data: []byte(`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`),
			},
		},
	},
	{
		`UINF KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`,
		&UDPPacket{
			ID: types.MustParseCID(`KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI`),
			BasePacket: BasePacket{
				Name: MsgType{'I', 'N', 'F'},
				Data: []byte(`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`),
			},
		},
	},
	{
		`UINF KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI`,
		&UDPPacket{
			ID: types.MustParseCID(`KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI`),
			BasePacket: BasePacket{
				Name: MsgType{'I', 'N', 'F'},
			},
		},
	},
	{
		`FINF AAAB IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`,
		&FeaturePacket{
			ID: types.SIDFromString("AAAB"),
			BasePacket: BasePacket{
				Name: MsgType{'I', 'N', 'F'},
				Data: []byte(`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`),
			},
		},
	},
	{
		`FINF AAAB`,
		&FeaturePacket{
			ID: types.SIDFromString("AAAB"),
			BasePacket: BasePacket{
				Name: MsgType{'I', 'N', 'F'},
			},
		},
	},
	{
		`FINF AAAB +SEGA -NAT0 IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`,
		&FeaturePacket{
			ID:       types.SIDFromString("AAAB"),
			Features: map[Feature]bool{{'S', 'E', 'G', 'A'}: true, {'N', 'A', 'T', '0'}: false},
			BasePacket: BasePacket{
				Name: MsgType{'I', 'N', 'F'},
				Data: []byte(`IDKAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI NIdennnn`),
			},
		},
	},
	{
		`FINF AAAB +SEGA -NAT0`,
		&FeaturePacket{
			BasePacket: BasePacket{
				Name: MsgType{'I', 'N', 'F'},
			},
			ID:       types.SIDFromString("AAAB"),
			Features: map[Feature]bool{{'S', 'E', 'G', 'A'}: true, {'N', 'A', 'T', '0'}: false},
		},
	},
	{
		`DSCH ABCD BACA TO4171511714 ANsome ANdata GR32`,
		&DirectPacket{
			BasePacket: BasePacket{
				Name: MsgType{'S', 'C', 'H'},
				Data: []byte(`TO4171511714 ANsome ANdata GR32`),
			},
			ID: types.SIDFromString("ABCD"), Targ: types.SIDFromString("BACA"),
		},
	},
	{
		`DSCH ABCD BACA`,
		&DirectPacket{
			BasePacket: BasePacket{
				Name: MsgType{'S', 'C', 'H'},
			},
			ID: types.SIDFromString("ABCD"), Targ: types.SIDFromString("BACA"),
		},
	},
	{
		`USCH KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI ANsome ANdata GR32`,
		&UDPPacket{
			BasePacket: BasePacket{
				Name: MsgType{'S', 'C', 'H'},
				Data: []byte(`ANsome ANdata GR32`),
			},
			ID: types.MustParseCID(`KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI`),
		},
	},
	{
		`USCH KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI`,
		&UDPPacket{
			BasePacket: BasePacket{
				Name: MsgType{'S', 'C', 'H'},
			},
			ID: types.MustParseCID(`KAY6BI76T6XFIQXZNRYE4WXJ2Y3YGXJG7UM7XLI`),
		},
	},
}

func TestDecodePacket(t *testing.T) {
	for _, c := range casesPackets {
		t.Run("", func(t *testing.T) {
			cmd, err := DecodePacket([]byte(c.data))
			if err != nil {
				t.Fatal(err)
			} else if !reflect.DeepEqual(cmd, c.packet) {
				t.Fatalf("\n%#v\nvs\n%#v", cmd, c.packet)
			}
		})
	}
}

func TestEncodePacket(t *testing.T) {
	for _, c := range casesPackets {
		t.Run("", func(t *testing.T) {
			data, err := c.packet.MarshalPacket()
			if err != nil {
				t.Fatal(err)
			} else if !bytes.Equal(data, []byte(c.data)) {
				t.Fatalf("\n%#v\nvs\n%#v", string(data), string(c.data))
			}
		})
	}
}
