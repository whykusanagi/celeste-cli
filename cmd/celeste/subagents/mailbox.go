package subagents

import "sync"

// Message is a single inter-agent message.
type Message struct {
	From string
	To   string
	Body string
}

// Mailbox is a concurrency-safe per-address message store keyed by an agent's
// address (its element name). Drain is destructive.
type Mailbox struct {
	mu    sync.Mutex
	boxes map[string][]Message
}

// NewMailbox creates a new empty Mailbox.
func NewMailbox() *Mailbox {
	return &Mailbox{boxes: make(map[string][]Message)}
}

// Post appends a message to the recipient's mailbox.
func (m *Mailbox) Post(to, from, body string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.boxes[to] = append(m.boxes[to], Message{From: from, To: to, Body: body})
}

// Drain returns and clears all messages for an address.
func (m *Mailbox) Drain(to string) []Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	msgs := m.boxes[to]
	delete(m.boxes, to)
	return msgs
}
