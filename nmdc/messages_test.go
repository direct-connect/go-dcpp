package nmdc

import (
	"bytes"
	"reflect"
	"testing"
)

var casesUnmarshal = []struct {
	name string
	data string
	msg  Message
}{
	{
		name: "MyINFO",
		data: `$ALL johndoe <++ V:0.673,M:P,H:0/1/0,S:2>$ $LAN(T3)0x31$example@example.com$1234$`,
		msg: &MyInfo{
			Name:      "johndoe",
			Client:    "++",
			Version:   "0.673",
			Mode:      UserModePassive,
			Hubs:      [3]int{0, 1, 0},
			Slots:     2,
			OpenSlots: "",
			Info:      "$LAN(T3)0x31$example@example.com$1234$",
		},
	},
}

func TestUnmarshal(t *testing.T) {
	for _, c := range casesUnmarshal {
		t.Run(c.name, func(t *testing.T) {
			m, err := UnmarshalMessage(c.name, []byte(c.data))
			if err != nil {
				t.Fatal(err)
			} else if !reflect.DeepEqual(m, c.msg) {
				t.Fatalf("failed: %#v", m)
			}
		})
	}
}

func TestMarshal(t *testing.T) {
	for _, c := range casesUnmarshal {
		t.Run(c.name, func(t *testing.T) {
			data, err := c.msg.MarshalNMDC()
			if err != nil {
				t.Fatal(err)
			} else if !bytes.Equal(data, []byte(c.data)) {
				t.Fatalf("failed: %#v vs %#v", string(data), string(c.data))
			}
		})
	}
}
