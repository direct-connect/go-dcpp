package hub

import "testing"

func TestChatBuffer(t *testing.T) {
	b := chatBuffer{limit: 3}
	expect := func(n int, exp ...string) {
		t.Helper()
		out := b.Get(n)
		got := make([]string, 0, len(out))
		for _, m := range out {
			got = append(got, m.Text)
		}
		if len(exp) != len(got) {
			t.Errorf("unexpected log:\n%q vs %q", exp, got)
			return
		}
		for i := range exp {
			if exp[i] != got[i] {
				t.Errorf("unexpected log:\n%q vs %q", exp, got)
				return
			}
		}
	}

	expect(0)
	expect(1)

	b.Append(Message{Text: "A"})

	expect(0, "A")
	expect(1, "A")
	expect(2, "A")

	b.Append(Message{Text: "B"})

	expect(0, "A", "B")
	expect(1, "B")
	expect(3, "A", "B")

	b.Append(Message{Text: "C"})

	expect(0, "A", "B", "C")
	expect(2, "B", "C")

	// overflow
	b.Append(Message{Text: "D"})

	expect(0, "B", "C", "D")
	expect(1, "D")
	expect(2, "C", "D")
	expect(3, "B", "C", "D")
	expect(4, "B", "C", "D")

	b.Append(Message{Text: "E"})

	expect(0, "C", "D", "E")
	expect(1, "E")
	expect(2, "D", "E")
	expect(3, "C", "D", "E")
	expect(4, "C", "D", "E")
}
