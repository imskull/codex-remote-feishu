package agentproto

import "testing"

func TestSideSessionsRequireExplicitCapability(t *testing.T) {
	for _, backend := range []Backend{BackendCodex, BackendClaude, BackendOpenCode} {
		if EffectiveCapabilitiesForBackend(backend, Capabilities{}).SideSession {
			t.Fatalf("legacy %s wrapper gained side support", backend)
		}
		if !EffectiveCapabilitiesForBackend(backend, Capabilities{SideSession: true}).SideSession {
			t.Fatalf("declared capability lost for %s", backend)
		}
	}
}

func TestSideSessionProtocolPreservesIdentity(t *testing.T) {
	envelopes := []Envelope{
		{Type: EnvelopeCommand, Command: &Command{Kind: CommandSideSessionOpen, CommandID: "open", SideSessionID: "side", Target: Target{ThreadID: "parent"}}},
		{Type: EnvelopeEventBatch, EventBatch: &EventBatch{Events: []Event{{Kind: EventSideSessionUpdated, SideSessionID: "side", ThreadID: "fork", CommandID: "open", Status: "open"}}}},
	}
	for _, envelope := range envelopes {
		raw, err := MarshalEnvelope(envelope)
		if err != nil {
			t.Fatal(err)
		}
		got, err := UnmarshalEnvelope(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got.Command != nil && got.Command.SideSessionID != "side" {
			t.Fatal("command lost side identity")
		}
		if got.EventBatch != nil && got.EventBatch.Events[0].SideSessionID != "side" {
			t.Fatal("event lost side identity")
		}
	}
}
