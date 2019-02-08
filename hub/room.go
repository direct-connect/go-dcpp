package hub

import (
	"time"
)

type Message struct {
	Time time.Time
	Name string
	Text string
}

type chatBuffer struct {
	start int
	limit int
	ring  []Message
}

func (c *chatBuffer) Get(n int) []Message {
	if n <= 0 || n > len(c.ring) {
		n = len(c.ring)
	}
	out := make([]Message, n)
	// index in the dst array where new messages (head) start
	i := n - c.start
	// index in the src (head) for an amount of messages that fit into dst
	j := 0
	if i < 0 {
		j = -i
		i = 0
	}
	copy(out[i:], c.ring[j:c.start])
	if i == 0 {
		return out
	}
	// index in the src (tail) for an amount of messages that fit into dst
	j = len(c.ring) - i
	copy(out[:i], c.ring[j:j+i])
	return out
}

func (c *chatBuffer) Append(m Message) {
	if len(c.ring) < c.limit {
		c.ring = append(c.ring, m)
		return
	}
	// ring buffer - overwrite an oldest item and advance the pointer
	c.ring[c.start] = m
	c.start++
	if c.start >= len(c.ring) {
		c.start = 0
	}
}
