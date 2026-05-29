package channel

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/keithwegner/pq-fabric/core/messages"
)

const (
	TypeConversation  = "conversation"
	TypeWorkingMemory = "working_memory"
	TypeExecution     = "execution"
	TypeRetrieval     = "retrieval"

	CompressionNoop = "noop"
	CompressionGzip = "gzip"

	EvictOldestFirst         = "oldest_first"
	EvictLowestPriorityFirst = "lowest_priority_first"
)

type Policy struct {
	ChannelID      string `json:"channel_id"`
	ChannelType    string `json:"channel_type"`
	PriorityWeight int    `json:"priority_weight"`
	MaxItems       int    `json:"max_items"`
	MaxBytes       int    `json:"max_bytes"`
	Compression    string `json:"compression"`
	EvictionPolicy string `json:"eviction_policy"`
}

type Item struct {
	ID           string `json:"id"`
	ChannelID    string `json:"channel_id"`
	ChannelType  string `json:"channel_type"`
	Priority     int    `json:"priority"`
	Sequence     uint64 `json:"sequence"`
	Payload      []byte `json:"payload"`
	SizeEstimate int    `json:"size_estimate"`
	Digest       string `json:"digest"`
	Compression  string `json:"compression"`
	CreatedTick  uint64 `json:"created_tick"`
}

type EvictionDecision struct {
	ChannelID string `json:"channel_id"`
	ItemID    string `json:"item_id"`
	Sequence  uint64 `json:"sequence"`
	Reason    string `json:"reason"`
}

type Channel struct {
	Policy      Policy `json:"policy"`
	nextSeq     uint64
	pending     []Item
	delivered   map[string]Item
	retainedLen int
}

type Manager struct {
	channels      map[string]*Channel
	order         []string
	totalMaxBytes int
	evictions     []EvictionDecision
}

func DefaultPolicies() []Policy {
	return []Policy{
		{ChannelID: TypeConversation, ChannelType: TypeConversation, PriorityWeight: 40, MaxItems: 8, MaxBytes: 4096, Compression: CompressionNoop, EvictionPolicy: EvictOldestFirst},
		{ChannelID: TypeWorkingMemory, ChannelType: TypeWorkingMemory, PriorityWeight: 50, MaxItems: 8, MaxBytes: 4096, Compression: CompressionNoop, EvictionPolicy: EvictOldestFirst},
		{ChannelID: TypeExecution, ChannelType: TypeExecution, PriorityWeight: 100, MaxItems: 8, MaxBytes: 4096, Compression: CompressionNoop, EvictionPolicy: EvictOldestFirst},
		{ChannelID: TypeRetrieval, ChannelType: TypeRetrieval, PriorityWeight: 70, MaxItems: 8, MaxBytes: 4096, Compression: CompressionNoop, EvictionPolicy: EvictOldestFirst},
	}
}

func NewManager(totalMaxBytes int, policies []Policy) (*Manager, error) {
	if len(policies) == 0 {
		policies = DefaultPolicies()
	}
	m := &Manager{
		channels:      make(map[string]*Channel, len(policies)),
		totalMaxBytes: totalMaxBytes,
	}
	for _, policy := range policies {
		if policy.ChannelID == "" || policy.ChannelType == "" {
			return nil, errors.New("channel policy requires channel id and type")
		}
		if policy.PriorityWeight <= 0 {
			return nil, fmt.Errorf("channel %s priority weight must be positive", policy.ChannelID)
		}
		if policy.Compression == "" {
			policy.Compression = CompressionNoop
		}
		if policy.EvictionPolicy == "" {
			policy.EvictionPolicy = EvictOldestFirst
		}
		if _, exists := m.channels[policy.ChannelID]; exists {
			return nil, fmt.Errorf("duplicate channel policy: %s", policy.ChannelID)
		}
		m.channels[policy.ChannelID] = &Channel{Policy: policy, nextSeq: 1, delivered: make(map[string]Item)}
		m.order = append(m.order, policy.ChannelID)
	}
	sort.Strings(m.order)
	return m, nil
}

func (m *Manager) Add(channelID, itemID string, payload []byte, createdTick uint64) (Item, error) {
	ch, ok := m.channels[channelID]
	if !ok {
		return Item{}, fmt.Errorf("unknown channel: %s", channelID)
	}
	compressed, err := Compress(ch.Policy.Compression, payload)
	if err != nil {
		return Item{}, err
	}
	item := Item{
		ID:           itemID,
		ChannelID:    ch.Policy.ChannelID,
		ChannelType:  ch.Policy.ChannelType,
		Priority:     ch.Policy.PriorityWeight,
		Sequence:     ch.nextSeq,
		Payload:      compressed,
		SizeEstimate: len(compressed),
		Compression:  ch.Policy.Compression,
		CreatedTick:  createdTick,
	}
	digest, err := messages.HashCanonical(struct {
		ID          string `json:"id"`
		ChannelID   string `json:"channel_id"`
		ChannelType string `json:"channel_type"`
		Sequence    uint64 `json:"sequence"`
		Payload     []byte `json:"payload"`
	}{
		ID: item.ID, ChannelID: item.ChannelID, ChannelType: item.ChannelType, Sequence: item.Sequence, Payload: item.Payload,
	})
	if err != nil {
		return Item{}, err
	}
	item.Digest = digest
	ch.nextSeq++
	ch.pending = append(ch.pending, item)
	m.enforceChannelBudget(ch)
	m.enforceTotalBudget()
	return item, nil
}

func (m *Manager) Commit(channelID, itemID string) bool {
	ch, ok := m.channels[channelID]
	if !ok {
		return false
	}
	for i, item := range ch.pending {
		if item.ID == itemID {
			ch.delivered[itemID] = item
			ch.pending = append(ch.pending[:i], ch.pending[i+1:]...)
			return true
		}
	}
	return false
}

func (m *Manager) SnapshotItems() []Item {
	var out []Item
	for _, id := range m.order {
		out = append(out, m.channels[id].pending...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ChannelID != out[j].ChannelID {
			return out[i].ChannelID < out[j].ChannelID
		}
		return out[i].Sequence < out[j].Sequence
	})
	return out
}

func (m *Manager) Policies() []Policy {
	out := make([]Policy, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.channels[id].Policy)
	}
	return out
}

func (m *Manager) BudgetUsed() int {
	total := 0
	for _, ch := range m.channels {
		total += channelBytes(ch.pending)
	}
	return total
}

func (m *Manager) Evictions() []EvictionDecision {
	return append([]EvictionDecision(nil), m.evictions...)
}

func (m *Manager) NextSequence(channelID string) (uint64, bool) {
	ch, ok := m.channels[channelID]
	if !ok {
		return 0, false
	}
	return ch.nextSeq, true
}

func (m *Manager) Scheduler() *Scheduler {
	policies := m.Policies()
	items := m.SnapshotItems()
	return NewScheduler(policies, items)
}

func (m *Manager) enforceChannelBudget(ch *Channel) {
	for {
		if ch.Policy.MaxItems > 0 && len(ch.pending) > ch.Policy.MaxItems {
			m.evictFromChannel(ch, "channel max items")
			continue
		}
		if ch.Policy.MaxBytes > 0 && channelBytes(ch.pending) > ch.Policy.MaxBytes {
			m.evictFromChannel(ch, "channel max bytes")
			continue
		}
		return
	}
}

func (m *Manager) enforceTotalBudget() {
	for m.totalMaxBytes > 0 && m.BudgetUsed() > m.totalMaxBytes {
		ch := m.lowestPriorityNonEmptyChannel()
		if ch == nil {
			return
		}
		m.evictFromChannel(ch, "total context budget")
	}
}

func (m *Manager) evictFromChannel(ch *Channel, reason string) {
	if len(ch.pending) == 0 {
		return
	}
	idx := 0
	if ch.Policy.EvictionPolicy == EvictLowestPriorityFirst {
		for i := range ch.pending {
			if ch.pending[i].Priority < ch.pending[idx].Priority || (ch.pending[i].Priority == ch.pending[idx].Priority && ch.pending[i].Sequence < ch.pending[idx].Sequence) {
				idx = i
			}
		}
	}
	item := ch.pending[idx]
	ch.pending = append(ch.pending[:idx], ch.pending[idx+1:]...)
	m.evictions = append(m.evictions, EvictionDecision{ChannelID: item.ChannelID, ItemID: item.ID, Sequence: item.Sequence, Reason: reason})
}

func (m *Manager) lowestPriorityNonEmptyChannel() *Channel {
	var best *Channel
	for _, id := range m.order {
		ch := m.channels[id]
		if len(ch.pending) == 0 {
			continue
		}
		if best == nil || ch.Policy.PriorityWeight < best.Policy.PriorityWeight || (ch.Policy.PriorityWeight == best.Policy.PriorityWeight && ch.pending[0].Sequence < best.pending[0].Sequence) {
			best = ch
		}
	}
	return best
}

func channelBytes(items []Item) int {
	total := 0
	for _, item := range items {
		total += item.SizeEstimate
	}
	return total
}

type Scheduler struct {
	queues   map[string][]Item
	weights  map[string]int
	deficits map[string]int
	order    []string
	cursor   int
}

func NewScheduler(policies []Policy, items []Item) *Scheduler {
	s := &Scheduler{
		queues:   make(map[string][]Item),
		weights:  make(map[string]int),
		deficits: make(map[string]int),
	}
	for _, policy := range policies {
		s.weights[policy.ChannelID] = policy.PriorityWeight
		s.order = append(s.order, policy.ChannelID)
	}
	sort.Strings(s.order)
	for _, item := range items {
		copied := item
		copied.Payload = append([]byte(nil), item.Payload...)
		s.queues[item.ChannelID] = append(s.queues[item.ChannelID], copied)
	}
	for id := range s.queues {
		sort.Slice(s.queues[id], func(i, j int) bool {
			return s.queues[id][i].Sequence < s.queues[id][j].Sequence
		})
		if _, ok := s.weights[id]; !ok {
			s.weights[id] = 1
			s.order = append(s.order, id)
			sort.Strings(s.order)
		}
	}
	return s
}

func (s *Scheduler) Next() (Item, bool) {
	if len(s.order) == 0 || s.Len() == 0 {
		return Item{}, false
	}
	emptyPasses := 0
	for emptyPasses < len(s.order)*2 {
		id := s.order[s.cursor]
		if len(s.queues[id]) == 0 {
			s.deficits[id] = 0
			s.advance()
			emptyPasses++
			continue
		}
		if s.deficits[id] < 1 {
			weight := s.weights[id]
			if weight <= 0 {
				weight = 1
			}
			s.deficits[id] += weight
		}
		if s.deficits[id] >= 1 {
			item := s.queues[id][0]
			s.queues[id] = s.queues[id][1:]
			s.deficits[id]--
			if s.deficits[id] == 0 {
				s.advance()
			}
			return item, true
		}
		s.advance()
		emptyPasses++
	}
	return Item{}, false
}

func (s *Scheduler) Len() int {
	total := 0
	for _, queue := range s.queues {
		total += len(queue)
	}
	return total
}

func (s *Scheduler) advance() {
	s.cursor = (s.cursor + 1) % len(s.order)
}

func Compress(name string, payload []byte) ([]byte, error) {
	switch name {
	case "", CompressionNoop:
		return append([]byte(nil), payload...), nil
	case CompressionGzip:
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write(payload); err != nil {
			_ = zw.Close()
			return nil, err
		}
		if err := zw.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	default:
		return nil, fmt.Errorf("unsupported compression profile: %s", name)
	}
}

func Decompress(name string, payload []byte) ([]byte, error) {
	switch name {
	case "", CompressionNoop:
		return append([]byte(nil), payload...), nil
	case CompressionGzip:
		zr, err := gzip.NewReader(bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		return io.ReadAll(zr)
	default:
		return nil, fmt.Errorf("unsupported compression profile: %s", name)
	}
}
