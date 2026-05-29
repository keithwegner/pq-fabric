package ai_context

import "github.com/keithwegner/pq-fabric/bundle/ccsds"

type ContextWindow struct {
	MaxTokens int                                     `json:"max_tokens"`
	Policies  map[ccsds.ChannelID]ccsds.ChannelPolicy `json:"policies"`
}

func DefaultContextWindow() ContextWindow {
	return ContextWindow{MaxTokens: 128000, Policies: ccsds.DefaultPolicies()}
}
