package channel

import (
	"bytes"
	"testing"
)

func testPolicies() []Policy {
	return []Policy{
		{ChannelID: TypeConversation, ChannelType: TypeConversation, PriorityWeight: 1, MaxItems: 10, MaxBytes: 1000, Compression: CompressionNoop, EvictionPolicy: EvictOldestFirst},
		{ChannelID: TypeExecution, ChannelType: TypeExecution, PriorityWeight: 3, MaxItems: 10, MaxBytes: 1000, Compression: CompressionNoop, EvictionPolicy: EvictOldestFirst},
		{ChannelID: TypeRetrieval, ChannelType: TypeRetrieval, PriorityWeight: 1, MaxItems: 10, MaxBytes: 1000, Compression: CompressionNoop, EvictionPolicy: EvictOldestFirst},
		{ChannelID: TypeWorkingMemory, ChannelType: TypeWorkingMemory, PriorityWeight: 1, MaxItems: 10, MaxBytes: 1000, Compression: CompressionNoop, EvictionPolicy: EvictOldestFirst},
	}
}

func TestChannelsCreatedDeterministically(t *testing.T) {
	m, err := NewManager(1000, testPolicies())
	if err != nil {
		t.Fatal(err)
	}
	policies := m.Policies()
	if got := policies[0].ChannelID; got != TypeConversation {
		t.Fatalf("policies should be sorted deterministically, got first %s", got)
	}
	item, err := m.Add(TypeConversation, "msg-1", []byte("hello"), 1)
	if err != nil {
		t.Fatal(err)
	}
	if item.Sequence != 1 {
		t.Fatalf("expected first sequence 1, got %d", item.Sequence)
	}
	next, ok := m.NextSequence(TypeConversation)
	if !ok || next != 2 {
		t.Fatalf("next sequence should be 2, got %d ok=%v", next, ok)
	}
}

func TestPriorityWeightsAffectSchedulingAndAvoidStarvation(t *testing.T) {
	m, err := NewManager(1000, testPolicies())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := m.Add(TypeExecution, "exec-"+string(rune('a'+i)), []byte("e"), uint64(i+1)); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Add(TypeRetrieval, "ret-"+string(rune('a'+i)), []byte("r"), uint64(i+1)); err != nil {
			t.Fatal(err)
		}
	}
	s := m.Scheduler()
	var firstFour []string
	for i := 0; i < 4; i++ {
		item, ok := s.Next()
		if !ok {
			t.Fatal("expected scheduled item")
		}
		firstFour = append(firstFour, item.ChannelID)
	}
	executionCount := 0
	for _, id := range firstFour {
		if id == TypeExecution {
			executionCount++
		}
	}
	if executionCount < 3 {
		t.Fatalf("execution should receive more weighted service, got %v", firstFour)
	}
	seenRetrieval := false
	for s.Len() > 0 {
		item, _ := s.Next()
		if item.ChannelID == TypeRetrieval {
			seenRetrieval = true
			break
		}
	}
	if !seenRetrieval {
		t.Fatal("lower priority channel starved")
	}
}

func TestEqualPrioritySchedulingIsFairAndStable(t *testing.T) {
	policies := []Policy{
		{ChannelID: "a", ChannelType: "a", PriorityWeight: 1, Compression: CompressionNoop},
		{ChannelID: "b", ChannelType: "b", PriorityWeight: 1, Compression: CompressionNoop},
	}
	m, err := NewManager(1000, policies)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = m.Add("b", "b-1", []byte("b"), 1)
	_, _ = m.Add("a", "a-1", []byte("a"), 1)
	s := m.Scheduler()
	first, ok := s.Next()
	if !ok {
		t.Fatal("expected first item")
	}
	second, ok := s.Next()
	if !ok {
		t.Fatal("expected second item")
	}
	if first.ChannelID != "a" || second.ChannelID != "b" {
		t.Fatalf("equal priority should follow stable channel order, got %s then %s", first.ChannelID, second.ChannelID)
	}
}

func TestBudgetEnforcementAndDeterministicEviction(t *testing.T) {
	policies := []Policy{
		{ChannelID: TypeExecution, ChannelType: TypeExecution, PriorityWeight: 100, MaxItems: 10, MaxBytes: 1000, Compression: CompressionNoop, EvictionPolicy: EvictOldestFirst},
		{ChannelID: TypeRetrieval, ChannelType: TypeRetrieval, PriorityWeight: 10, MaxItems: 10, MaxBytes: 1000, Compression: CompressionNoop, EvictionPolicy: EvictOldestFirst},
	}
	m, err := NewManager(10, policies)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Add(TypeRetrieval, "ret-1", []byte("123456"), 1); err != nil {
		t.Fatal(err)
	}
	exec, err := m.Add(TypeExecution, "exec-1", []byte("abcdef"), 2)
	if err != nil {
		t.Fatal(err)
	}
	if m.BudgetUsed() > 10 {
		t.Fatalf("budget not enforced, used %d", m.BudgetUsed())
	}
	items := m.SnapshotItems()
	if len(items) != 1 || items[0].ID != exec.ID {
		t.Fatalf("low-priority retrieval should be evicted first, retained %+v", items)
	}
	evictions := m.Evictions()
	if len(evictions) != 1 || evictions[0].ItemID != "ret-1" {
		t.Fatalf("unexpected eviction decisions: %+v", evictions)
	}
	next, _ := m.NextSequence(TypeRetrieval)
	if next != 2 {
		t.Fatalf("eviction should not reset sequence counter, got %d", next)
	}
}

func TestPerChannelBudgetRetainsConversationPolicy(t *testing.T) {
	policies := []Policy{
		{ChannelID: TypeConversation, ChannelType: TypeConversation, PriorityWeight: 1, MaxItems: 2, MaxBytes: 1000, Compression: CompressionNoop, EvictionPolicy: EvictOldestFirst},
	}
	m, err := NewManager(1000, policies)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = m.Add(TypeConversation, "msg-1", []byte("one"), 1)
	_, _ = m.Add(TypeConversation, "msg-2", []byte("two"), 2)
	_, _ = m.Add(TypeConversation, "msg-3", []byte("three"), 3)
	items := m.SnapshotItems()
	if len(items) != 2 || items[0].ID != "msg-2" || items[1].ID != "msg-3" {
		t.Fatalf("oldest conversation item should be evicted, got %+v", items)
	}
}

func TestCompressionRoundTripAndMalformedData(t *testing.T) {
	payload := bytes.Repeat([]byte("context "), 20)
	for _, compression := range []string{CompressionNoop, CompressionGzip} {
		compressed, err := Compress(compression, payload)
		if err != nil {
			t.Fatal(err)
		}
		roundTrip, err := Decompress(compression, compressed)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(roundTrip, payload) {
			t.Fatalf("%s round trip changed payload", compression)
		}
	}
	if _, err := Decompress(CompressionGzip, []byte("not gzip")); err == nil {
		t.Fatal("malformed gzip should fail safely")
	}
}

func TestSchedulerHandlesEmptyChannels(t *testing.T) {
	s := NewScheduler(testPolicies(), nil)
	if _, ok := s.Next(); ok {
		t.Fatal("empty scheduler should not return an item")
	}
}
