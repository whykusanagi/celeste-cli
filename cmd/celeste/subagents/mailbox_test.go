package subagents

import "testing"

func TestMailbox_PostDrain(t *testing.T) {
	mb := NewMailbox()
	mb.Post("fire", "water", "found the config at /etc/app")
	mb.Post("fire", "earth", "schema is v3")
	msgs := mb.Drain("fire")
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].From != "water" || msgs[0].Body != "found the config at /etc/app" {
		t.Fatalf("unexpected first message: %+v", msgs[0])
	}
	// Drain is destructive: second drain is empty.
	if len(mb.Drain("fire")) != 0 {
		t.Fatal("drain should empty the mailbox")
	}
}

func TestMailbox_UnknownAddressEmpty(t *testing.T) {
	mb := NewMailbox()
	if len(mb.Drain("nobody")) != 0 {
		t.Fatal("draining unknown mailbox should return empty, not panic")
	}
}

func TestMailbox_ConcurrentPost(t *testing.T) {
	mb := NewMailbox()
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func() { mb.Post("fire", "x", "msg"); done <- true }()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	if len(mb.Drain("fire")) != 10 {
		t.Fatal("concurrent posts lost messages")
	}
}
