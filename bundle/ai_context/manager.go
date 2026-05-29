package ai_context

import (
	"encoding/json"
	"fmt"
	"sort"

	bundlechannel "github.com/keithwegner/pq-fabric/bundle/channel"
	"github.com/keithwegner/pq-fabric/core/messages"
	"github.com/keithwegner/pq-fabric/core/storage"
)

const WorkingMemorySnapshotID = "ai-context/working-memory"

type ContextItem struct {
	ID           string `json:"id"`
	ChannelType  string `json:"channel_type"`
	Priority     int    `json:"priority"`
	SizeEstimate int    `json:"size_estimate"`
	Sequence     uint64 `json:"sequence"`
	Digest       string `json:"digest"`
	Content      string `json:"content"`
	CreatedTick  uint64 `json:"created_tick"`
}

type Frame struct {
	Items  []ContextItem `json:"items"`
	Digest string        `json:"digest"`
}

type Manager struct {
	channels *bundlechannel.Manager
}

func NewManager(totalBudgetBytes int) (*Manager, error) {
	policies := []bundlechannel.Policy{
		{ChannelID: bundlechannel.TypeConversation, ChannelType: bundlechannel.TypeConversation, PriorityWeight: 40, MaxItems: 16, MaxBytes: totalBudgetBytes, Compression: bundlechannel.CompressionNoop, EvictionPolicy: bundlechannel.EvictOldestFirst},
		{ChannelID: bundlechannel.TypeWorkingMemory, ChannelType: bundlechannel.TypeWorkingMemory, PriorityWeight: 60, MaxItems: 16, MaxBytes: totalBudgetBytes, Compression: bundlechannel.CompressionNoop, EvictionPolicy: bundlechannel.EvictOldestFirst},
		{ChannelID: bundlechannel.TypeExecution, ChannelType: bundlechannel.TypeExecution, PriorityWeight: 100, MaxItems: 16, MaxBytes: totalBudgetBytes, Compression: bundlechannel.CompressionNoop, EvictionPolicy: bundlechannel.EvictOldestFirst},
		{ChannelID: bundlechannel.TypeRetrieval, ChannelType: bundlechannel.TypeRetrieval, PriorityWeight: 20, MaxItems: 16, MaxBytes: totalBudgetBytes, Compression: bundlechannel.CompressionNoop, EvictionPolicy: bundlechannel.EvictOldestFirst},
	}
	manager, err := bundlechannel.NewManager(totalBudgetBytes, policies)
	if err != nil {
		return nil, err
	}
	return &Manager{channels: manager}, nil
}

func (m *Manager) AddItem(channelType, id, content string, tick uint64) (ContextItem, error) {
	item, err := m.channels.Add(channelType, id, []byte(content), tick)
	if err != nil {
		return ContextItem{}, err
	}
	return contextItemFromChannelItem(item)
}

func (m *Manager) AssembleFrame(maxBytes int) (Frame, error) {
	scheduler := m.channels.Scheduler()
	var items []ContextItem
	used := 0
	for scheduler.Len() > 0 {
		item, ok := scheduler.Next()
		if !ok {
			break
		}
		contextItem, err := contextItemFromChannelItem(item)
		if err != nil {
			return Frame{}, err
		}
		if maxBytes > 0 && used+contextItem.SizeEstimate > maxBytes {
			continue
		}
		used += contextItem.SizeEstimate
		items = append(items, contextItem)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority > items[j].Priority
		}
		if items[i].ChannelType != items[j].ChannelType {
			return items[i].ChannelType < items[j].ChannelType
		}
		return items[i].Sequence < items[j].Sequence
	})
	digest, err := messages.HashCanonical(items)
	if err != nil {
		return Frame{}, err
	}
	return Frame{Items: items, Digest: digest}, nil
}

func (m *Manager) Digest() (string, error) {
	frame, err := m.AssembleFrame(0)
	if err != nil {
		return "", err
	}
	return frame.Digest, nil
}

func (m *Manager) RecordMockResponse(responseText string, tick uint64) (ContextItem, error) {
	return m.AddItem(bundlechannel.TypeConversation, fmt.Sprintf("mock-response-%d", tick), responseText, tick)
}

func (m *Manager) BuildMockRequest(model string) (ChatCompletionRequest, error) {
	frame, err := m.AssembleFrame(0)
	if err != nil {
		return ChatCompletionRequest{}, err
	}
	req := ChatCompletionRequest{Model: model}
	for _, item := range frame.Items {
		role := "user"
		if item.ChannelType == bundlechannel.TypeExecution {
			role = "tool"
		}
		req.Messages = append(req.Messages, ChatMessage{Role: role, Content: item.Content})
	}
	return req, nil
}

func (m *Manager) Evictions() []bundlechannel.EvictionDecision {
	return m.channels.Evictions()
}

func (m *Manager) Policies() []bundlechannel.Policy {
	return m.channels.Policies()
}

func (m *Manager) SaveWorkingMemory(store storage.ValidatorStore) error {
	frame, err := m.AssembleFrame(0)
	if err != nil {
		return err
	}
	var memory []ContextItem
	for _, item := range frame.Items {
		if item.ChannelType == bundlechannel.TypeWorkingMemory {
			memory = append(memory, item)
		}
	}
	data, err := json.Marshal(memory)
	if err != nil {
		return err
	}
	digest := messages.HashBytes(data)
	return store.SaveSnapshot(storage.SnapshotRecord{ID: WorkingMemorySnapshotID, LastHash: digest, SnapshotJSON: data})
}

func LoadWorkingMemory(store storage.ValidatorStore, totalBudgetBytes int) (*Manager, bool, error) {
	snapshots, err := store.ListSnapshots()
	if err != nil {
		return nil, false, err
	}
	manager, err := NewManager(totalBudgetBytes)
	if err != nil {
		return nil, false, err
	}
	for i := len(snapshots) - 1; i >= 0; i-- {
		if snapshots[i].ID != WorkingMemorySnapshotID {
			continue
		}
		var items []ContextItem
		if err := json.Unmarshal(snapshots[i].SnapshotJSON, &items); err != nil {
			return nil, false, err
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Sequence < items[j].Sequence })
		for _, item := range items {
			if _, err := manager.AddItem(bundlechannel.TypeWorkingMemory, item.ID, item.Content, item.CreatedTick); err != nil {
				return nil, false, err
			}
		}
		return manager, true, nil
	}
	return manager, false, nil
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
}

type ChatCompletionResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Choices []ChatChoice `json:"choices"`
}

type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

type MockProvider struct{}

func (MockProvider) Complete(req ChatCompletionRequest) (ChatCompletionResponse, error) {
	digest, err := messages.HashCanonical(req)
	if err != nil {
		return ChatCompletionResponse{}, err
	}
	return ChatCompletionResponse{
		ID:     "mockcmpl-" + digest[:16],
		Object: "chat.completion",
		Choices: []ChatChoice{{
			Index:        0,
			Message:      ChatMessage{Role: "assistant", Content: "deterministic mock response " + digest[:12]},
			FinishReason: "stop",
		}},
	}, nil
}

func contextItemFromChannelItem(item bundlechannel.Item) (ContextItem, error) {
	payload, err := bundlechannel.Decompress(item.Compression, item.Payload)
	if err != nil {
		return ContextItem{}, err
	}
	return ContextItem{
		ID:           item.ID,
		ChannelType:  item.ChannelType,
		Priority:     item.Priority,
		SizeEstimate: len(payload),
		Sequence:     item.Sequence,
		Digest:       item.Digest,
		Content:      string(payload),
		CreatedTick:  item.CreatedTick,
	}, nil
}
