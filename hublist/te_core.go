package hublist

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// TELists is a list of well-known TE-compatible hub lists.
var TELists = []string{
	"http://www.te-home.net/",
}

func teGetRaw(ctx context.Context, addr string, dst interface{}) error {
	if !strings.Contains(addr, "?") {
		addr += `?do=hublist&get=hublist.json`
	}
	req, err := http.NewRequest("GET", addr, nil)
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
	return json.NewDecoder(resp.Body).Decode(dst)
}

// GetTE fetches and decodes a TE-compatible hub list.
func GetTE(ctx context.Context, addr string) ([]Hub, error) {
	var resp struct {
		List []teHub `json:"hublist"`
	}
	err := teGetRaw(ctx, addr, &resp)
	if err != nil {
		return nil, err
	}
	list := make([]Hub, 0, len(resp.List))
	for _, h := range resp.List {
		list = append(list, h.toHub())
	}
	return list, err
}

func (h teHub) toHub() Hub {
	return Hub{
		Address:     h.Address,
		Bots:        h.Bots,
		Country:     h.Country,
		Description: h.Description,
		Email:       h.Email,
		Encoding:    h.Encoding,
		Failover:    h.Failover,
		Icon:        h.Icon,
		Infected:    h.Infected,
		Logo:        h.Logo,
		Maxhubs:     h.Maxhubs,
		Maxusers:    h.Maxusers,
		Minshare:    Size(h.Minshare),
		Minslots:    h.Minslots,
		Name:        h.Name,
		Network:     h.Network,
		Operators:   h.Operators,
		Rating:      strconv.Itoa(h.Rating),
		Reliability: strconv.FormatFloat(h.Reliability, 'g', -1, 64),
		Shared:      Size(h.Shared),
		Software:    h.Software,
		Status:      h.Status,
		Users:       h.Users,
		Website:     h.Website,
	}
}
