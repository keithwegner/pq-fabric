package ai_context

import (
	"testing"

	bundlechannel "github.com/keithwegner/pq-fabric/bundle/channel"
	"github.com/keithwegner/pq-fabric/core/storage"
)

func TestContextFrameAssemblyDeterministicAndDigestChanges(t *testing.T) {
	m, err := NewManager(1000)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = m.AddItem(bundlechannel.TypeConversation, "conv-1", "hello", 1)
	first, err := m.AssembleFrame(0)
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.AssembleFrame(0)
	if err != nil {
		t.Fatal(err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("frame assembly should be deterministic: %s != %s", first.Digest, second.Digest)
	}
	_, _ = m.AddItem(bundlechannel.TypeWorkingMemory, "mem-1", "remember this", 2)
	changed, err := m.AssembleFrame(0)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Digest == first.Digest {
		t.Fatal("context digest should change when relevant context changes")
	}
}

func TestContextBudgetPrioritizesExecutionOverRetrieval(t *testing.T) {
	m, err := NewManager(20)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = m.AddItem(bundlechannel.TypeRetrieval, "ret-1", "retrieval payload that is larger", 1)
	exec, _ := m.AddItem(bundlechannel.TypeExecution, "exec-1", "run", 2)
	frame, err := m.AssembleFrame(20)
	if err != nil {
		t.Fatal(err)
	}
	if len(frame.Items) == 0 || frame.Items[0].ID != exec.ID {
		t.Fatalf("execution item should be prioritized in constrained frame, got %+v", frame.Items)
	}
	if len(m.Evictions()) == 0 {
		t.Fatal("retrieval item should have been evicted under total budget")
	}
}

func TestWorkingMemorySurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(1000)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = m.AddItem(bundlechannel.TypeWorkingMemory, "mem-1", "persist me", 1)
	if err := m.SaveWorkingMemory(store); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.OpenFileStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	loaded, ok, err := LoadWorkingMemory(reopened, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected working memory snapshot")
	}
	frame, err := loaded.AssembleFrame(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(frame.Items) != 1 || frame.Items[0].Content != "persist me" {
		t.Fatalf("working memory did not survive restart: %+v", frame.Items)
	}
}

func TestMockProviderDeterministicAndRecordsResponse(t *testing.T) {
	m, err := NewManager(1000)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = m.AddItem(bundlechannel.TypeConversation, "conv-1", "question", 1)
	req, err := m.BuildMockRequest("mock-local")
	if err != nil {
		t.Fatal(err)
	}
	provider := MockProvider{}
	first, err := provider.Complete(req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := provider.Complete(req)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || first.Choices[0].Message.Content != second.Choices[0].Message.Content {
		t.Fatalf("mock provider should be deterministic: %+v %+v", first, second)
	}
	if _, err := m.RecordMockResponse(first.Choices[0].Message.Content, 2); err != nil {
		t.Fatal(err)
	}
	frame, err := m.AssembleFrame(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(frame.Items) < 2 {
		t.Fatalf("mock response was not recorded in context: %+v", frame.Items)
	}
}
