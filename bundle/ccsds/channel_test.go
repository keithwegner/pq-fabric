package ccsds

import "testing"

func TestSchedulerPrioritizesExecution(t *testing.T) {
	s := NewScheduler(DefaultPolicies())
	if err := s.Enqueue(Conversation, 1, []byte("chat")); err != nil {
		t.Fatal(err)
	}
	if err := s.Enqueue(Execution, 1, []byte("tool call")); err != nil {
		t.Fatal(err)
	}
	frame, ok := s.Next()
	if !ok {
		t.Fatal("expected frame")
	}
	if frame.Channel != Execution {
		t.Fatalf("expected execution first, got %s", frame.Channel)
	}
}

func TestSchedulerRejectsUnknownChannelAndReportsLength(t *testing.T) {
	s := NewScheduler(nil)
	if _, ok := s.Next(); ok {
		t.Fatal("empty scheduler should not return a frame")
	}
	if err := s.Enqueue(ChannelID("unknown"), 1, []byte("payload")); err == nil {
		t.Fatal("expected unknown channel error")
	}
	if err := s.Enqueue(Conversation, 1, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	if s.Len() != 1 {
		t.Fatalf("expected length 1, got %d", s.Len())
	}
}

func TestSchedulerKeepsFIFOWithinSamePriorityAndCopiesPayload(t *testing.T) {
	s := NewScheduler(map[ChannelID]ChannelPolicy{
		Conversation:  {ID: Conversation, PriorityWeight: 10},
		WorkingMemory: {ID: WorkingMemory, PriorityWeight: 10},
	})
	payload := []byte("first")
	if err := s.Enqueue(Conversation, 1, payload); err != nil {
		t.Fatal(err)
	}
	payload[0] = 'x'
	if err := s.Enqueue(WorkingMemory, 2, []byte("second")); err != nil {
		t.Fatal(err)
	}
	first, ok := s.Next()
	if !ok {
		t.Fatal("expected first frame")
	}
	if first.Channel != Conversation || string(first.Payload) != "first" {
		t.Fatalf("expected copied FIFO first frame, got %+v payload=%s", first, first.Payload)
	}
	second, ok := s.Next()
	if !ok {
		t.Fatal("expected second frame")
	}
	if second.Channel != WorkingMemory {
		t.Fatalf("expected working memory second, got %s", second.Channel)
	}
}
