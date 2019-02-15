package hublist

import (
	"compress/bzip2"
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
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
