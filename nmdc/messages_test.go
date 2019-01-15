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
		data: `$ALL johndoe <ApexDC++ V:0.4.0,M:P,H:27/1/3,S:92,L:512>$ $LAN(T3)0x31$example@example.com$1234$`,
		msg: &MyInfo{
			Name:    "johndoe",
			Client:  "ApexDC++",
			Version: "0.4.0",
			Mode:    UserModePassive,
			Hubs:    [3]int{27, 1, 3},
			Slots:   92,
			Other:   map[string]string{"L": "512"},
			Info:    "$LAN(T3)0x31$example@example.com$1234$",
		},
	},
	{
		name: "ConnectToMe",
		data: `john 192.168.1.2:412S`,
		msg: &ConnectToMe{
			From:    "john",
			Address: "192.168.1.2:412",
			Secure:  true,
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
