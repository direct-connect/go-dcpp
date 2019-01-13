package hub

import (
	"encoding/json"
	"net"
	"net/http"

	"golang.org/x/net/http2"
)

func (h *Hub) initHTTP() {
	if h.tls == nil {
		return
	}
	h.tls.NextProtos = append(h.tls.NextProtos, "h2")
	h.h2 = &http2.Server{}
	htls := h.tls.Clone()
	htls.NextProtos = nil
	h.h2conf = &http2.ServeConnOpts{
		BaseConfig: &http.Server{
			TLSConfig: htls,
			Handler:   h,
		},
	}
}

func (h *Hub) ServeHTTP2(conn net.Conn) error {
	h.h2.ServeConn(conn, h.h2conf)
	return nil
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	st := h.Stats()
	_ = json.NewEncoder(w).Encode(st)
}
