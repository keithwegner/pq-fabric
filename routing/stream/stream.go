package stream

import (
	"errors"
	"fmt"
	"sync"
)

type Manager struct {
	mu     sync.Mutex
	next   uint64
	open   map[uint64]struct{}
	seen   map[uint64]struct{}
	closed bool
}

func NewManager() *Manager {
	return &Manager{
		next: 1,
		open: make(map[uint64]struct{}),
		seen: make(map[uint64]struct{}),
	}
}

func (m *Manager) Open() (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return 0, errors.New("stream manager is closed")
	}
	id := m.next
	m.next++
	m.open[id] = struct{}{}
	m.seen[id] = struct{}{}
	return id, nil
}

func (m *Manager) OpenWithID(id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("stream manager is closed")
	}
	if id == 0 {
		return errors.New("stream id must be positive")
	}
	if _, ok := m.seen[id]; ok {
		return fmt.Errorf("duplicate stream id %d", id)
	}
	m.open[id] = struct{}{}
	m.seen[id] = struct{}{}
	if id >= m.next {
		m.next = id + 1
	}
	return nil
}

func (m *Manager) EnsureOpen(id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.open[id]; !ok {
		return fmt.Errorf("unknown or closed stream id %d", id)
	}
	return nil
}

func (m *Manager) Close(id uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.open[id]; !ok {
		return fmt.Errorf("unknown or closed stream id %d", id)
	}
	delete(m.open, id)
	return nil
}

func (m *Manager) OpenCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.open)
}

func (m *Manager) SeenCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.seen)
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.open = make(map[uint64]struct{})
	m.closed = true
}
