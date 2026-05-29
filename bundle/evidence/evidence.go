package evidence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	aicontext "github.com/keithwegner/pq-fabric/bundle/ai_context"
	bundlechannel "github.com/keithwegner/pq-fabric/bundle/channel"
	"github.com/keithwegner/pq-fabric/bundle/custody"
	bundleprotocol "github.com/keithwegner/pq-fabric/bundle/protocol"
	"github.com/keithwegner/pq-fabric/bundle/reconcile"
	"github.com/keithwegner/pq-fabric/bundle/retransmit"
	cryptosuite "github.com/keithwegner/pq-fabric/core/crypto/suite"
	"github.com/keithwegner/pq-fabric/core/identity"
	"github.com/keithwegner/pq-fabric/core/messages"
	"github.com/keithwegner/pq-fabric/core/storage"
)

type Evidence struct {
	ChannelDefinitions        []bundlechannel.Policy `json:"channel_definitions"`
	SchedulingDecisions       []SchedulingDecision   `json:"scheduling_decisions"`
	BundleIDs                 []string               `json:"bundle_ids"`
	SequenceNumbers           []uint64               `json:"sequence_numbers"`
	TransactionIDs            []string               `json:"transaction_ids"`
	CustodyEvents             []CustodyEventEvidence `json:"custody_events"`
	QuorumSize                int                    `json:"quorum_size"`
	CustodyConfirmationStatus string                 `json:"custody_confirmation_status"`
	RetransmissionCount       int                    `json:"retransmission_count"`
	DuplicateTransactionCount int                    `json:"duplicate_transaction_count"`
	ReconciledBundleCount     int                    `json:"reconciled_bundle_count"`
	EvictedContextItemCount   int                    `json:"evicted_context_item_count"`
	FinalStateDigest          string                 `json:"final_state_digest"`
	MockAIRequestDigest       string                 `json:"mock_ai_request_digest"`
	MockAIResponseDigest      string                 `json:"mock_ai_response_digest"`
	Limitations               string                 `json:"limitations"`
	OutputJSON                string                 `json:"output_json,omitempty"`
	OutputText                string                 `json:"output_text,omitempty"`
}

type SchedulingDecision struct {
	Order       int    `json:"order"`
	ChannelID   string `json:"channel_id"`
	ItemID      string `json:"item_id"`
	Sequence    uint64 `json:"sequence"`
	Priority    int    `json:"priority"`
	BundleID    string `json:"bundle_id"`
	Transaction string `json:"transaction_id"`
}

type CustodyEventEvidence struct {
	BundleID      string `json:"bundle_id"`
	TransactionID string `json:"transaction_id"`
	EventDigest   string `json:"event_digest"`
	QuorumSize    int    `json:"quorum_size"`
	Confirmed     bool   `json:"confirmed"`
}

func RunScenario(outputDir string) (Evidence, error) {
	selected, err := cryptosuite.FromEnv()
	if err != nil {
		return Evidence{}, err
	}
	contextManager, err := aicontext.NewManager(4096)
	if err != nil {
		return Evidence{}, err
	}
	inputs := []struct {
		channel string
		id      string
		content string
		tick    uint64
	}{
		{bundlechannel.TypeConversation, "conv-1", "user: summarize the validator state", 1},
		{bundlechannel.TypeWorkingMemory, "mem-1", "remember quorum threshold is 5-of-7", 2},
		{bundlechannel.TypeExecution, "exec-1", "tool: inspect local bundle queue", 3},
		{bundlechannel.TypeRetrieval, "ret-1", "retrieved: private testbed routing notes", 4},
	}
	for _, in := range inputs {
		if _, err := contextManager.AddItem(in.channel, in.id, in.content, in.tick); err != nil {
			return Evidence{}, err
		}
	}
	frame, err := contextManager.AssembleFrame(0)
	if err != nil {
		return Evidence{}, err
	}
	var envelopes []bundleprotocol.Envelope
	evidence := Evidence{
		ChannelDefinitions:        contextManager.Policies(),
		QuorumSize:                5,
		CustodyConfirmationStatus: "unconfirmed",
		EvictedContextItemCount:   len(contextManager.Evictions()),
		Limitations:               "Local deterministic prototype only: no live AI API calls, no production transport reliability, no production BFT safety, no data-sovereignty claim, and no FIPS/ACVTS/anonymity claim.",
	}
	for i, item := range frame.Items {
		env, err := bundleprotocol.NewEnvelope(bundleprotocol.NewEnvelopeInput{
			SourceNodeID:      "ai-client",
			DestinationNodeID: "ai-agent",
			ChannelID:         item.ChannelType,
			ChannelType:       item.ChannelType,
			SequenceNumber:    item.Sequence,
			TransactionID:     "tx-" + item.ID,
			CreationTick:      item.CreatedTick,
			ExpirationTick:    item.CreatedTick + 100,
			Priority:          item.Priority,
			PayloadBytes:      []byte(item.Content),
			CustodyRequested:  true,
		})
		if err != nil {
			return Evidence{}, err
		}
		envelopes = append(envelopes, env)
		evidence.BundleIDs = append(evidence.BundleIDs, env.BundleID)
		evidence.SequenceNumbers = append(evidence.SequenceNumbers, env.SequenceNumber)
		evidence.TransactionIDs = append(evidence.TransactionIDs, env.TransactionID)
		evidence.SchedulingDecisions = append(evidence.SchedulingDecisions, SchedulingDecision{
			Order:       i + 1,
			ChannelID:   item.ChannelType,
			ItemID:      item.ID,
			Sequence:    item.Sequence,
			Priority:    item.Priority,
			BundleID:    env.BundleID,
			Transaction: env.TransactionID,
		})
	}

	identities, err := identity.ValidatorIdentitiesForSuite(nil, selected)
	if err != nil {
		return Evidence{}, err
	}
	custodyStore := storage.NewMemoryStore()
	for _, env := range envelopes {
		bundleDigest, err := env.Digest()
		if err != nil {
			return Evidence{}, err
		}
		event := custody.Event{
			BundleID:        env.BundleID,
			TransactionID:   env.TransactionID,
			SourceNode:      env.SourceNodeID,
			CustodyHolder:   "validator-1",
			DestinationNode: env.DestinationNodeID,
			BundleDigest:    bundleDigest,
			SequenceNumber:  env.SequenceNumber,
			LogicalTick:     env.CreationTick + 10,
		}
		votes, err := custody.SignEventVotes(event, identity.DefaultValidatorIDs()[:5], selected)
		if err != nil {
			return Evidence{}, err
		}
		confirmation, err := custody.Confirm(event, votes, identities, selected.NewVerifier(), 5)
		if err != nil {
			return Evidence{}, err
		}
		if _, err := custody.ApplyConfirmation(custodyStore, confirmation); err != nil {
			return Evidence{}, err
		}
		evidence.CustodyEvents = append(evidence.CustodyEvents, CustodyEventEvidence{
			BundleID:      env.BundleID,
			TransactionID: env.TransactionID,
			EventDigest:   confirmation.EventDigest,
			QuorumSize:    confirmation.QuorumSize,
			Confirmed:     confirmation.Confirmed,
		})
	}
	evidence.CustodyConfirmationStatus = "confirmed"

	queue := retransmit.NewQueue(retransmit.NewLedger())
	for i, env := range envelopes {
		result := queue.Submit(env, env.CreationTick)
		if result.Duplicate {
			evidence.DuplicateTransactionCount++
		}
		if i == 0 {
			queue.Confirm(env.BundleID)
			duplicate := queue.Submit(env, env.CreationTick)
			if duplicate.Duplicate {
				evidence.DuplicateTransactionCount++
			}
		}
	}
	pendingBeforeReconnect := queue.PendingForRetransmit()
	evidence.RetransmissionCount = len(pendingBeforeReconnect)
	for _, env := range pendingBeforeReconnect {
		retry := queue.Submit(env, env.CreationTick)
		if retry.Duplicate {
			evidence.DuplicateTransactionCount++
		}
		queue.Confirm(env.BundleID)
	}

	localState := reconcile.NewBundleState()
	remoteState := reconcile.NewBundleState()
	for i, env := range envelopes {
		digest, err := env.Digest()
		if err != nil {
			return Evidence{}, err
		}
		record := reconcile.BundleRecord{BundleID: env.BundleID, Digest: digest, TransactionID: env.TransactionID, ChannelID: env.ChannelID, Sequence: env.SequenceNumber}
		remoteState.Bundles[env.BundleID] = record
		remoteState.CommittedTransactionIDs[env.TransactionID] = digest
		remoteState.CustodyStatusByBundle[env.BundleID] = "confirmed"
		remoteState.LatestSequenceByChannel[env.ChannelID] = env.SequenceNumber
		if i == 0 {
			localState.Bundles[env.BundleID] = record
			localState.CommittedTransactionIDs[env.TransactionID] = digest
			localState.CustodyStatusByBundle[env.BundleID] = "confirmed"
			localState.LatestSequenceByChannel[env.ChannelID] = env.SequenceNumber
		} else {
			localState.PendingBundleIDs = append(localState.PendingBundleIDs, env.BundleID)
		}
	}
	plan := reconcile.Compare(localState, remoteState)
	reconciled, err := reconcile.Apply(localState, remoteState, plan)
	if err != nil {
		return Evidence{}, err
	}
	evidence.ReconciledBundleCount = len(plan.MissingBundleIDs)

	req, err := contextManager.BuildMockRequest("mock-openai-compatible-local")
	if err != nil {
		return Evidence{}, err
	}
	reqDigest, err := messages.HashCanonical(req)
	if err != nil {
		return Evidence{}, err
	}
	response, err := (aicontext.MockProvider{}).Complete(req)
	if err != nil {
		return Evidence{}, err
	}
	respDigest, err := messages.HashCanonical(response)
	if err != nil {
		return Evidence{}, err
	}
	if _, err := contextManager.RecordMockResponse(response.Choices[0].Message.Content, 99); err != nil {
		return Evidence{}, err
	}
	finalDigest, err := contextManager.Digest()
	if err != nil {
		return Evidence{}, err
	}
	reconciled.ContextStateDigest = finalDigest
	evidence.FinalStateDigest = finalDigest
	evidence.MockAIRequestDigest = reqDigest
	evidence.MockAIResponseDigest = respDigest

	if outputDir != "" {
		if err := WriteArtifacts(outputDir, evidence); err != nil {
			return Evidence{}, err
		}
		evidence.OutputJSON = filepath.Join(outputDir, "bundle-evidence.json")
		evidence.OutputText = filepath.Join(outputDir, "bundle-evidence.txt")
	}
	return evidence, nil
}

func WriteArtifacts(outputDir string, evidence Evidence) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	jsonPath := filepath.Join(outputDir, "bundle-evidence.json")
	textPath := filepath.Join(outputDir, "bundle-evidence.txt")
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(jsonPath, data, 0o644); err != nil {
		return err
	}
	return os.WriteFile(textPath, []byte(TextSummary(evidence)), 0o644)
}

func TextSummary(e Evidence) string {
	var b strings.Builder
	fmt.Fprintf(&b, "pq-fabric bundle protocol evidence\n")
	fmt.Fprintf(&b, "channels=%d bundles=%d custody=%s quorum=%d\n", len(e.ChannelDefinitions), len(e.BundleIDs), e.CustodyConfirmationStatus, e.QuorumSize)
	fmt.Fprintf(&b, "scheduled=%d retransmissions=%d duplicates=%d reconciled=%d evicted=%d\n", len(e.SchedulingDecisions), e.RetransmissionCount, e.DuplicateTransactionCount, e.ReconciledBundleCount, e.EvictedContextItemCount)
	fmt.Fprintf(&b, "mock_request_digest=%s\n", short(e.MockAIRequestDigest))
	fmt.Fprintf(&b, "mock_response_digest=%s\n", short(e.MockAIResponseDigest))
	fmt.Fprintf(&b, "final_state_digest=%s\n", short(e.FinalStateDigest))
	fmt.Fprintf(&b, "limitations=%s\n", e.Limitations)
	return b.String()
}

func short(value string) string {
	if len(value) <= 16 {
		return value
	}
	return value[:16]
}
