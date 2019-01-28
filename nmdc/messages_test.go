package nmdc

import (
	"bytes"
	"reflect"
	"testing"
)

var casesUnmarshal = []struct {
	typ     string
	name    string
	data    string
	expData string
	msg     Message
}{
	{
		typ:  "FailOver",
		data: `example.com,example.org:5555,adc://example.net:6666`,
		msg: &FailOver{
			Host: []string{
				"example.com",
				"example.org:5555",
				"adc://example.net:6666",
			},
		},
	},
	{
		typ:  "UserIP",
		data: `johndoe 192.168.1.2`,
		msg: &UserIP{
			Name: "johndoe",
			IP:   "192.168.1.2",
		},
	},
	{
		typ:  "Lock",
		name: "without Pk",
		data: `EXTENDEDPROTOCOLABCABCABCABCABCABC Ref=dchub://example.org:411`,
		msg: &Lock{
			Lock: "EXTENDEDPROTOCOLABCABCABCABCABCABC",
			Ref:  "dchub://example.org:411",
		},
	},
	{
		typ:  "Lock",
		name: "without Ref",
		data: `EXTENDEDPROTOCOLABCABCABCABCABCABC Pk=DCPLUSPLUS0.777`,
		msg: &Lock{
			Lock: "EXTENDEDPROTOCOLABCABCABCABCABCABC",
			PK:   "DCPLUSPLUS0.777",
		},
	},
	{
		typ:  "Lock",
		name: "with Ref",
		data: `EXTENDEDPROTOCOLABCABCABCABCABCABC Pk=DCPLUSPLUS0.777Ref=dchub://example.org:411`,
		msg: &Lock{
			Lock: "EXTENDEDPROTOCOLABCABCABCABCABCABC",
			PK:   "DCPLUSPLUS0.777",
			Ref:  "dchub://example.org:411",
		},
	},
	{
		typ:  "Lock",
		name: "with Ref",
		data: `EXTENDEDPROTOCOLABCABCABCABCABCABC Pk=DCPLUSPLUS0.777Ref=dchub://example.org:411`,
		msg: &Lock{
			Lock: "EXTENDEDPROTOCOLABCABCABCABCABCABC",
			PK:   "DCPLUSPLUS0.777",
			Ref:  "dchub://example.org:411",
		},
	},
	{
		typ:     "HubINFO",
		name:    "9 fields",
		data:    `OZERKI$dc.ozerki.pro$Main Russian D�++ Hub$5000$0$1$2721$PtokaX$`,
		expData: `OZERKI$dc.ozerki.pro$Main Russian D�++ Hub$5000$0$1$2721$PtokaX$$$`,
		msg: &HubINFO{
			Name: "OZERKI",
			Host: "dc.ozerki.pro",
			Desc: "Main Russian D�++ Hub",
			I1:   5000,
			I2:   0,
			I3:   1,
			I4:   2721,
			Soft: "PtokaX",
		},
	},
	{
		typ:     "HubINFO",
		name:    "10 fields",
		data:    `Free$localhost:411$Online!$900$0$0$1000$VerliHub$$`,
		expData: `Free$localhost:411$Online!$900$0$0$1000$VerliHub$$$`,
		msg: &HubINFO{
			Name: "Free",
			Host: "localhost:411",
			Desc: "Online!",
			I1:   900,
			I2:   0,
			I3:   0,
			I4:   1000,
			Soft: "VerliHub",
		},
	},
	{
		typ:  "HubINFO",
		name: "all fields",
		data: `Angels vs Demons$dc.milenahub.ru$Cogitationis poenam nemo patitur.$20480$0$0$0$Verlihub 1.1.0.12$=FAUST= & KCAHDEP$Public HUB$CP1251`,
		msg: &HubINFO{
			Name:     "Angels vs Demons",
			Host:     "dc.milenahub.ru",
			Desc:     "Cogitationis poenam nemo patitur.",
			I1:       20480,
			I2:       0,
			I3:       0,
			I4:       0,
			Soft:     "Verlihub 1.1.0.12",
			Owner:    "=FAUST= & KCAHDEP",
			State:    "Public HUB",
			Encoding: "CP1251",
		},
	},
	{
		typ:  "MyINFO",
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
		typ:     "MyINFO",
		name:    "no tag",
		data:    `$ALL verg P verg$ $0.005A$$114616804986$`,
		expData: `$ALL verg P verg< V:,M:,H:0/0/0,S:0>$ $0.005A$$114616804986$`,
		msg: &MyInfo{
			Name:      "verg",
			Desc:      "P verg",
			Mode:      UserModeUnknown,
			Conn:      "0.005",
			Flag:      'A',
			ShareSize: 114616804986,
		},
	},
	{
		typ:     "MyINFO",
		name:    "no share",
		data:    `$ALL verg P verg$ $0.005A$$$`,
		expData: `$ALL verg P verg< V:,M:,H:0/0/0,S:0>$ $0.005A$$0$`,
		msg: &MyInfo{
			Name: "verg",
			Desc: "P verg",
			Mode: UserModeUnknown,
			Conn: "0.005",
			Flag: 'A',
		},
	},
	{
		typ:     "MyINFO",
		name:    "no vers",
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
		typ:     "MyINFO",
		name:    "hub space",
		data:    `$ALL vespa9347q1 <StrgDC++ V:2.42,M:A,H:1 /0/0,S:2>$ $0.01.$$37038592310$`,
		expData: `$ALL vespa9347q1 <StrgDC++ V:2.42,M:A,H:1/0/0,S:2>$ $0.01.$$37038592310$`,
		msg: &MyInfo{
			Name:      "vespa9347q1",
			Client:    "StrgDC++",
			Version:   "2.42",
			Mode:      UserModeActive,
			Hubs:      [3]int{1, 0, 0},
			Slots:     2,
			Conn:      "0.01",
			Flag:      '.',
			ShareSize: 37038592310,
		},
	},
	{
		typ:     "MyINFO",
		name:    "only name",
		data:    `$ALL #GlobalOpChat $ $$$0$`,
		expData: "$ALL #GlobalOpChat < V:,M:,H:0/0/0,S:0>$ $\x00$$0$",
		msg: &MyInfo{
			Name: "#GlobalOpChat",
			Mode: UserModeUnknown,
		},
	},
	{
		typ:  "ConnectToMe",
		data: `john 192.168.1.2:412S`,
		msg: &ConnectToMe{
			Targ:    "john",
			Address: "192.168.1.2:412",
			Secure:  true,
		},
	},
	{
		typ:  "To:",
		data: `john From: peter $<peter> dogs are more cute`,
		msg: &PrivateMessage{
			To:   "john",
			From: "peter",
			Text: "dogs are more cute",
		},
	},
	{
		typ:  "Error",
		data: `message`,
		msg: &Error{
			Text: "message",
		},
	},
}

func TestUnmarshal(t *testing.T) {
	for _, c := range casesUnmarshal {
		name := c.typ
		if c.name != "" {
			name += " " + c.name
		}
		t.Run(name, func(t *testing.T) {
			m, err := (&RawCommand{Name: c.typ, Data: []byte(c.data)}).Decode()
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
		name := c.typ
		if c.name != "" {
			name += " " + c.name
		}
		t.Run(name, func(t *testing.T) {
			data, err := c.msg.MarshalNMDC()
			exp := c.expData
			if exp == "" {
				exp = c.data
			}
			if err != nil {
				t.Fatal(err)
			} else if !bytes.Equal(data, []byte(exp)) {
				t.Fatalf("failed: %#v vs %#v", string(data), string(exp))
			}
		})
	}
}
