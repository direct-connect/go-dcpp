package hublist

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

const teURL = `http://www.te-home.net/?do=hublist&get=hublist.json`

func teGetRaw(ctx context.Context, dst interface{}) error {
	req, err := http.NewRequest("GET", teURL, nil)
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

func teGet(ctx context.Context) ([]teHub, error) {
	var resp struct {
		List []teHub `json:"hublist"`
	}
	err := teGetRaw(ctx, &resp)
	return resp.List, err
}

func (h teHub) toHub() Hub {
	return Hub{
		Address:     h.Address,
		ASN:         h.Asn,
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
		Minshare:    uint64(h.Minshare),
		Minslots:    h.Minslots,
		Name:        h.Name,
		Network:     h.Network,
		Operators:   h.Operators,
		Rating:      strconv.Itoa(h.Rating),
		Reliability: strconv.FormatFloat(h.Reliability, 'g', -1, 64),
		Shared:      uint64(h.Shared),
		Software:    h.Software,
		Status:      h.Status,
		Users:       h.Users,
		Website:     h.Website,
	}
}
