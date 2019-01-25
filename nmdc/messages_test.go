package nmdc

import (
	"bytes"
	"reflect"
	"testing"
)

var casesUnmarshal = []struct {
	name    string
	data    string
	expData string
	msg     Message
}{
	// TODO: $HubINFO Angels vs Demons$dc.milenahub.ru$Cogitationis poenam nemo patitur.$20480$0$0$0$Verlihub 1.1.0.12$=FAUST= & KCAHDEP$Public HUB$CP1251|
	{
		name: "MyINFO",
		data: `$ALL johndoe RU<ApexDC++ V:0.4.0,M:P,H:27/1/3,S:92,L:512>$ $LAN(T3)K$example@example.com$1234$`,
		msg: &MyInfo{
			Name:      "johndoe",
			Desc:      "RU",
			Client:    "ApexDC++",
			Version:   "0.4.0",
			Mode:      UserModePassive,
			Hubs:      [3]int{27, 1, 3},
			Slots:     92,
			Other:     map[string]string{"L": "512"},
			Conn:      "LAN(T3)",
			Flag:      'K',
			Email:     "example@example.com",
			ShareSize: 1234,
		},
	},
	{
		name:    "MyINFO",
		data:    `$ALL verg P verg$P$0.005A$$114616804986$`,
		expData: `$ALL verg P verg< V:,M:P,H:0/0/0,S:0>$ $0.005A$$114616804986$`,
		msg: &MyInfo{
			Name:      "verg",
			Desc:      "P verg",
			Mode:      UserModePassive,
			Conn:      "0.005",
			Flag:      'A',
			ShareSize: 114616804986,
		},
	},
	{
		name:    "MyINFO",
		data:    `$ALL elmaars1 LV [5]<elmaars1 DC++,M:A,H:1/0/0,S:5>$ $100A$$1294368450291$`,
		expData: `$ALL elmaars1 LV [5]<elmaars1 DC++ V:,M:A,H:1/0/0,S:5>$ $100A$$1294368450291$`,
		msg: &MyInfo{
			Name:      "elmaars1",
			Desc:      "LV [5]",
			Client:    "elmaars1 DC++",
			Mode:      UserModeActive,
			Hubs:      [3]int{1, 0, 0},
			Slots:     5,
			Conn:      "100",
			Flag:      'A',
			ShareSize: 1294368450291,
		},
	},
	{
		name: "ConnectToMe",
		data: `john 192.168.1.2:412S`,
		msg: &ConnectToMe{
			Targ:    "john",
			Address: "192.168.1.2:412",
			Secure:  true,
		},
	},
	{
		name: "To:",
		data: `john From: peter $<peter> dogs are more cute`,
		msg: &PrivateMessage{
			To:   "john",
			From: "peter",
			Text: "dogs are more cute",
		},
	},
	{
		name: "Error",
		data: `message`,
		msg: &Error{
			Text: "message",
		},
	},
}

func TestUnmarshal(t *testing.T) {
	for _, c := range casesUnmarshal {
		t.Run(c.name, func(t *testing.T) {
			m, err := (&RawCommand{Name: c.name, Data: []byte(c.data)}).Decode()
			if err != nil {
				t.Fatal(err)
			} else if !reflect.DeepEqual(m, c.msg) {
				t.Fatalf("failed: %#v vs %#v", m, c.msg)
			}
		})
	}
}

func TestMarshal(t *testing.T) {
	for _, c := range casesUnmarshal {
		t.Run(c.name, func(t *testing.T) {
			data, err := c.msg.MarshalNMDC()
			exp := c.expData
			if exp == "" {
				exp = c.data
			}
			if err != nil {
				t.Fatal(err)
			} else if !bytes.Equal(data, []byte(exp)) {
				t.Fatalf("failed: %#v vs %#v", string(data), string(c.data))
			}
		})
	}
}
