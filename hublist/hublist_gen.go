package hublist

type Hub struct {
	Name        string `xml:"Name,attr"`
	Address     string `xml:"Address,attr"`
	Bots        int    `xml:"Bots,attr,omitempty"`
	Country     string `xml:"Country,attr,omitempty"`
	Description string `xml:"Description,attr,omitempty"`
	Email       string `xml:"Email,attr,omitempty"`
	Encoding    string `xml:"Encoding,attr,omitempty"`
	Failover    string `xml:"Failover,attr,omitempty"`
	Icon        string `xml:"Icon,attr,omitempty"`
	Infected    int    `xml:"Infected,attr,omitempty"`
	Logo        string `xml:"Logo,attr,omitempty"`
	Maxhubs     int    `xml:"Maxhubs,attr,omitempty"`
	Maxusers    int    `xml:"Maxusers,attr,omitempty"`
	Minshare    Size   `xml:"Minshare,attr,omitempty"`
	Minslots    int    `xml:"Minslots,attr,omitempty"`
	Network     string `xml:"Network,attr,omitempty"`
	Operators   int    `xml:"Operators,attr,omitempty"`
	Rating      string `xml:"Rating,attr,omitempty"`
	Reliability string `xml:"Reliability,attr,omitempty"`
	Shared      Size   `xml:"Shared,attr,omitempty"`
	Software    string `xml:"Software,attr,omitempty"`
	Status      string `xml:"Status,attr,omitempty"`
	Users       int    `xml:"Users,attr,omitempty"`
	Website     string `xml:"Website,attr,omitempty"`

	// our extensions

	ErrCode   int      `xml:"ErrCode,attr,omitempty"`
	KeyPrints []string `xml:"KP,attr,omitempty"`
}
