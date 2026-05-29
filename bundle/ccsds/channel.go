package ccsds

import (
	"container/heap"
	"fmt"
	"sync"
	"time"
)

type ChannelID string

const (
	Conversation  ChannelID = "conversation"
	WorkingMemory ChannelID = "working_memory"
	Execution     ChannelID = "execution"
	Retrieval     ChannelID = "retrieval"
)

type ChannelPolicy struct {
	ID                 ChannelID `json:"id"`
	PriorityWeight     int       `json:"priority_weight"`
	CompressionProfile string    `json:"compression_profile"`
	EvictionPolicy     string    `json:"eviction_policy"`
}

type Frame struct {
	Channel   ChannelID `json:"channel"`
	Sequence  uint64    `json:"sequence"`
	Payload   []byte    `json:"payload"`
	Enqueued  int64     `json:"enqueued_unix_milli"`
	weight    int
	insertion uint64
}

type Scheduler struct {
	mu       sync.Mutex
	policies map[ChannelID]ChannelPolicy
	queue    frameHeap
	next     uint64
}

func DefaultPolicies() map[ChannelID]ChannelPolicy {
	return map[ChannelID]ChannelPolicy{
		Execution:     {ID: Execution, PriorityWeight: 100, CompressionProfile: "none", EvictionPolicy: "never-drop-running-tool-call"},
		Retrieval:     {ID: Retrieval, PriorityWeight: 70, CompressionProfile: "zstd-placeholder", EvictionPolicy: "drop-stale-results-first"},
		WorkingMemory: {ID: WorkingMemory, PriorityWeight: 50, CompressionProfile: "semantic-summary-placeholder", EvictionPolicy: "summarize-oldest"},
		Conversation:  {ID: Conversation, PriorityWeight: 40, CompressionProfile: "token-aware-placeholder", EvictionPolicy: "summarize-oldest"},
	}
}

func NewScheduler(policies map[ChannelID]ChannelPolicy) *Scheduler {
	if len(policies) == 0 {
		policies = DefaultPolicies()
	}
	return &Scheduler{policies: policies}
}

func (s *Scheduler) Enqueue(channel ChannelID, sequence uint64, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	policy, ok := s.policies[channel]
	if !ok {
		return fmt.Errorf("unknown virtual channel: %s", channel)
	}
	s.next++
	heap.Push(&s.queue, Frame{
		Channel:   channel,
		Sequence:  sequence,
		Payload:   append([]byte(nil), payload...),
		Enqueued:  time.Now().UnixMilli(),
		weight:    policy.PriorityWeight,
		insertion: s.next,
	})
	return nil
}

func (s *Scheduler) Next() (Frame, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return Frame{}, false
	}
	return heap.Pop(&s.queue).(Frame), true
}

func (s *Scheduler) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}

type frameHeap []Frame

func (h frameHeap) Len() int { return len(h) }
func (h frameHeap) Less(i, j int) bool {
	if h[i].weight == h[j].weight {
		return h[i].insertion < h[j].insertion
	}
	return h[i].weight > h[j].weight
}
func (h frameHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *frameHeap) Push(x any)   { *h = append(*h, x.(Frame)) }
func (h *frameHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}
