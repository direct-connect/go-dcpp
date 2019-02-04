package hub

import (
	"encoding/json"
	"log"
	"net"
	"net/http"

	"golang.org/x/net/http2"
)

const HTTPInfoPathV0 = "/api/v0/hubinfo.json"

func (h *Hub) initHTTP() {
	if h.tls == nil {
		return
	}
	h.tls.NextProtos = append(h.tls.NextProtos, "h2")
	h.h2 = &http2.Server{}
	htls := h.tls.Clone()
	htls.NextProtos = nil
	mux := http.NewServeMux()
	mux.HandleFunc(HTTPInfoPathV0, h.ServeHTTP)
	h.h2conf = &http2.ServeConnOpts{
		BaseConfig: &http.Server{
			TLSConfig: htls,
			Handler:   mux,
		},
	}
}

func (h *Hub) ServeHTTP2(conn net.Conn) error {
	log.Printf("%s: using HTTP2", conn.RemoteAddr())
	h.h2.ServeConn(conn, h.h2conf)
	return nil
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	st := h.Stats()
	hd := w.Header()
	hd.Set("Content-Type", "application/json")
	hd.Set("Server", st.Soft.Name+"/"+st.Soft.Vers)
	_ = json.NewEncoder(w).Encode(st)
}
