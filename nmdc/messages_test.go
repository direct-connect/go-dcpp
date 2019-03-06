package nmdc

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/direct-connect/go-dcpp/tiger"
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
		data: `johndoe 192.168.1.2$$`,
		msg: &UserIP{
			Name: "johndoe",
			IP:   "192.168.1.2",
		},
	},
	{
		typ:  "Lock",
		name: "no pk no ref",
		data: `EXTENDEDPROTOCOLABCABCABCABCABCABC`,
		msg: &Lock{
			Lock: "EXTENDEDPROTOCOLABCABCABCABCABCABC",
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
			Name:           "johndoe",
			Desc:           "RU",
			Client:         "ApexDC++",
			Version:        "0.4.0",
			Mode:           UserModePassive,
			HubsNormal:     27,
			HubsRegistered: 1,
			HubsOperator:   3,
			Slots:          92,
			Other:          map[string]string{"L": "512"},
			Conn:           "LAN(T3)",
			Flag:           'K',
			Email:          "example@example.com",
			ShareSize:      1234,
		},
	},
	{
		typ:     "MyINFO",
		name:    "no share & no tag",
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
		name:    "nil field in tag",
		data:    `$ALL whist RU [29]some desc<GreylynkDC++ v:2.3.5,$ $LAN(T1)A$$65075277005$`,
		expData: `$ALL whist RU [29]some desc<GreylynkDC++ V:2.3.5,M:,H:0/0/0,S:0>$ $LAN(T1)A$$65075277005$`,
		msg: &MyInfo{
			Name:      "whist",
			Desc:      "RU [29]some desc",
			Client:    "GreylynkDC++",
			Version:   "2.3.5",
			Mode:      UserModeUnknown,
			Conn:      "LAN(T1)",
			Flag:      'A',
			ShareSize: 65075277005,
		},
	},
	{
		typ:     "MyINFO",
		name:    "no vers & hub space",
		data:    `$ALL vespa9347q1 <StrgDC++,M:A,H:1 /0/0,S:2>$ $0.01.$$37038592310$`,
		expData: `$ALL vespa9347q1 <StrgDC++ V:,M:A,H:1/0/0,S:2>$ $0.01.$$37038592310$`,
		msg: &MyInfo{
			Name:           "vespa9347q1",
			Client:         "StrgDC++",
			Mode:           UserModeActive,
			HubsNormal:     1,
			HubsRegistered: 0,
			HubsOperator:   0,
			Slots:          2,
			Conn:           "0.01",
			Flag:           '.',
			ShareSize:      37038592310,
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
		data: `john 192.168.1.2:412`,
		msg: &ConnectToMe{
			Targ:    "john",
			Address: "192.168.1.2:412",
			Secure:  false,
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
		data: `john From: room $<peter> dogs are more cute`,
		msg: &PrivateMessage{
			To:   "john",
			From: "room",
			Name: "peter",
			Text: "dogs are more cute",
		},
	},
	{
		typ:  "Error",
		data: `message`,
		msg: &Error{
			Err: errors.New("message"),
		},
	},
	{
		typ:  "Search",
		data: `192.168.1.5:412 T?T?500000?1?Gentoo$2005`,
		msg: &Search{
			Address:        "192.168.1.5:412",
			SizeRestricted: true,
			IsMaxSize:      true,
			Size:           500000,
			DataType:       DataTypeAny,
			Pattern:        "Gentoo 2005",
		},
	},
	{
		typ:  "Search",
		name: "TTH",
		data: `Hub:SomeNick F?T?0?9?TTH:TO32WPD6AQE7VA7654HEAM5GKFQGIL7F2BEKFNA`,
		msg: &Search{
			Nick:           "SomeNick",
			SizeRestricted: false,
			IsMaxSize:      true,
			Size:           0,
			DataType:       DataTypeTTH,
			TTH:            getTHPointer("TO32WPD6AQE7VA7654HEAM5GKFQGIL7F2BEKFNA"),
		},
	},
	{
		typ:  "SR",
		name: "dir result",
		data: "User6 dir1\\dir2\\pictures 0/4\x05Testhub (192.168.1.1)",
		msg: &SR{
			Source:     "User6",
			DirName:    "dir1/dir2/pictures",
			TotalSlots: 4,
			HubName:    "Testhub",
			HubAddress: "192.168.1.1",
		},
	},
	{
		typ:  "SR",
		name: "file result",
		data: "User1 dir\\ponies.txt\x05437 3/4\x05Testhub (192.168.1.1:411)\x05User2",
		msg: &SR{
			Source:     "User1",
			FileName:   "dir/ponies.txt",
			FileSize:   437,
			FreeSlots:  3,
			TotalSlots: 4,
			HubName:    "Testhub",
			HubAddress: "192.168.1.1:411",
			Target:     "User2",
		},
	},
	{
		typ:  "SR",
		name: "tth result",
		data: "User1 Linux\\kubuntu-18.04-desktop-amd64.iso\x051868038144 3/3\x05TTH:BNQGWMXKUIAFAU3TV32I5U6SKNYMQBBNH4FELNQ (192.168.1.1:411)\x05User2",
		msg: &SR{
			Source:     "User1",
			FileName:   "Linux/kubuntu-18.04-desktop-amd64.iso",
			FileSize:   1868038144,
			FreeSlots:  3,
			TotalSlots: 3,
			TTH:        getTHPointer("BNQGWMXKUIAFAU3TV32I5U6SKNYMQBBNH4FELNQ"),
			HubAddress: "192.168.1.1:411",
			Target:     "User2",
		},
	},
	{
		typ:  "MCTo",
		data: `target $sender some message`,
		msg: &MCTo{
			Target: "target",
			Sender: "sender",
			Text:   "some message",
		},
	},
	{
		typ:  "UserCommand",
		name: "raw",
		data: `1 3 # Ledokol Menu\.:: Ranks\All time user location statistics $<%[mynick]> +cchist`,
		msg: &UserCommand{
			Type:    TypeRaw,
			Context: ContextHub | ContextUser,
			Path:    []String{"# Ledokol Menu", ".:: Ranks", "All time user location statistics"},
			Command: "<%[mynick]> +cchist",
		},
	},
	{
		typ:  "UserCommand",
		name: "erase",
		data: `255 1`,
		msg: &UserCommand{
			Type:    TypeErase,
			Context: ContextHub,
		},
	},
}

func getTHPointer(s string) *tiger.Hash {
	pointer := tiger.MustParseBase32(s)
	return &pointer
}

func TestUnmarshal(t *testing.T) {
	for _, c := range casesUnmarshal {
		name := c.typ
		if c.name != "" {
			name += " " + c.name
		}
		t.Run(name, func(t *testing.T) {
			m, err := (&RawCommand{Name: c.typ, Data: []byte(c.data)}).Decode(nil)
			require.NoError(t, err)
			require.Equal(t, c.msg, m)
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
			data, err := c.msg.MarshalNMDC(nil)
			exp := c.expData
			if exp == "" {
				exp = c.data
			}
			require.NoError(t, err)
			require.Equal(t, []byte(exp), data)
		})
	}
}
