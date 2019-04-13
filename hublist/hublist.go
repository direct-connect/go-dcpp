package hublist

import (
	"compress/bzip2"
	"context"
	"encoding"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

// Lists well-known hub lists.
var Lists = []string{
	"http://dchublist.com/hublist.xml.bz2",
	"http://dchublist.org/hublist.xml.bz2",
	"http://dchublist.biz/hublist.xml.bz2",
	"http://dchublist.ru/hublist.xml.bz2",
}

// Get fetches and decodes a hub list.
func Get(ctx context.Context, url string) ([]Hub, error) {
	var resp struct {
		List []Hub `xml:"Hubs>Hub"`
	}
	err := getRaw(ctx, url, &resp)
	return resp.List, err
}

// DecodeBZip2 decodes a .xml.bz2 files list.
func DecodeBZip2(r io.Reader) ([]Hub, error) {
	var list struct {
		List []Hub `xml:"Hubs>Hub"`
	}
	zr := bzip2.NewReader(r)
	if err := xml.NewDecoder(zr).Decode(&list); err != nil {
		return nil, err
	}
	return list.List, nil
}

var _ encoding.TextUnmarshaler = (*Size)(nil)

type Size uint64

func (s *Size) UnmarshalText(text []byte) error {
	str := string(text)
	if str == "" {
		*s = 0
		return nil
	}
	v, err := strconv.ParseUint(str, 10, 64)
	if err == nil {
		*s = Size(v)
		return nil
	}
	f, err2 := strconv.ParseFloat(str, 64)
	if err2 != nil {
		return err
	}
	*s = Size(f)
	return nil
}

func getRaw(ctx context.Context, url string, dst interface{}) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("http status: %v", resp.Status)
	}
	zr := bzip2.NewReader(resp.Body)
	return xml.NewDecoder(zr).Decode(dst)
}

// GetAll fetches and decodes all hub lists.
func GetAll(ctx context.Context) ([]Hub, error) {
	seen := make(map[string]struct{})
	var (
		list []Hub
		last error
	)
	for _, url := range Lists {
		hubs, err := Get(ctx, url)
		for _, h := range hubs {
			if _, ok := seen[h.Address]; ok {
				continue
			}
			seen[h.Address] = struct{}{}
			list = append(list, h)
		}
		if err != nil {
			last = err
		}
	}
	for _, url := range TELists {
		hubs, err := GetTE(ctx, url)
		for _, h := range hubs {
			if _, ok := seen[h.Address]; ok {
				continue
			}
			seen[h.Address] = struct{}{}
			list = append(list, h)
		}
		if err != nil {
			last = err
		}
	}
	return list, last
}

type XMLListWriter struct {
	w   io.Writer
	enc *xml.Encoder

	name    string
	addr    string
	headers bool

	started []string
}

func NewXMLWriter(w io.Writer) *XMLListWriter {
	return &XMLListWriter{w: w, headers: true}
}

func (w *XMLListWriter) SetHublistName(name string) {
	w.name = name
}

func (w *XMLListWriter) SetHublistURL(url string) {
	w.addr = url
}

func (w *XMLListWriter) Headers(v bool) {
	w.headers = v
}

func (w *XMLListWriter) startToken(name string, attrs []xml.Attr) error {
	w.started = append(w.started, name)
	return w.enc.EncodeToken(xml.StartElement{
		Name: xml.Name{Local: name},
		Attr: attrs,
	})
}

func (w *XMLListWriter) writeHeader() error {
	if w.headers {
		_, err := w.w.Write([]byte(xml.Header))
		if err != nil {
			return err
		}
	}
	w.enc = xml.NewEncoder(w.w)
	w.enc.Indent("", "\t")
	if !w.headers {
		return nil
	}
	var attr []xml.Attr
	if w.name != "" {
		attr = append(attr, xml.Attr{
			Name:  xml.Name{Local: "Name"},
			Value: w.name,
		})
	}
	if w.addr != "" {
		attr = append(attr, xml.Attr{
			Name:  xml.Name{Local: "Address"},
			Value: w.addr,
		})
	}
	err := w.startToken("Hublist", attr)
	if err != nil {
		return err
	}
	err = w.startToken("Hubs", attr)
	if err != nil {
		return err
	}
	err = w.enc.Flush()
	if err != nil {
		return err
	}
	// TODO: use reflection
	_, err = w.w.Write([]byte(`
		<Columns>
			<Column Name="Name" Type="string" />
			<Column Name="Address" Type="string" />
			<Column Name="Description" Type="string" />
			<Column Name="Country" Type="string" />
			<Column Name="Encoding" Type="string" />
			<Column Name="Users" Type="int" />
			<Column Name="Operators" Type="int" />
			<Column Name="Bots" Type="int" />
			<Column Name="Infected" Type="int" />
			<Column Name="Shared" Type="bytes" />
			<Column Name="Minshare" Type="bytes" />
			<Column Name="Minslots" Type="int" />
			<Column Name="Maxhubs" Type="int" />
			<Column Name="Maxusers" Type="int" />
			<Column Name="Status" Type="string" />
			<Column Name="Reliability" Type="string" />
			<Column Name="Rating" Type="string" />
			<Column Name="Software" Type="string" />
			<Column Name="Website" Type="string" />
			<Column Name="Email" Type="string" />
			<Column Name="ASN" Type="string" />
			<Column Name="Network" Type="string" />
			<Column Name="Failover" Type="string" />
			<Column Name="Logo" Type="string" />
			<Column Name="Icon" Type="string" />
			<Column Name="ErrCode" Type="int" />
		</Columns>`))
	return err
}

func (w *XMLListWriter) WriteHub(h Hub) error {
	if w.enc == nil {
		if err := w.writeHeader(); err != nil {
			return err
		}
	}
	return w.enc.Encode(h)
}

func (w *XMLListWriter) Close() error {
	if w.w == nil {
		return nil
	}
	if w.enc == nil {
		if err := w.writeHeader(); err != nil {
			return err
		}
	}
	for i := len(w.started) - 1; i >= 0; i-- {
		err := w.enc.EncodeToken(xml.EndElement{
			Name: xml.Name{Local: w.started[i]},
		})
		if err != nil {
			return err
		}
	}
	if err := w.enc.Flush(); err != nil {
		return err
	}
	_, err := w.w.Write([]byte("\n"))
	w.w = nil
	return err
}
