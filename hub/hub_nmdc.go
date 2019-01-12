package hub

import "net"

func (h *Hub) ServeNMDC(conn net.Conn) error {
	return h.nmdc.Serve(conn)
}
