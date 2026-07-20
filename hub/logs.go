package hub

import "log"

func (h *Hub) Log(args ...any) {
	log.Println(args...)
}

func (h *Hub) Logf(format string, args ...any) {
	log.Printf(format, args...)
}

// TODO(dennwc): support op chat

func (h *Hub) OpLog(args ...any) {
	h.Log(args...)
}

func (h *Hub) OpLogf(format string, args ...any) {
	h.Logf(format, args...)
}
