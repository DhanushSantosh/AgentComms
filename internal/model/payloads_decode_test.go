package model

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestDecodePayloadValueCoversEveryRegisteredType proves DecodePayloadValue
// resolves every event type payloadFactories knows about into a concrete,
// non-pointer value. It replaces a hand-maintained type switch in
// internal/authority that silently lacked a case for agent.rename for
// several releases -- the reflect-based helper cannot have that bug, and
// this test guards that it stays generic.
// RegisteredEventTypes returns every event type payloadFactories knows how
// to encode/decode. Exported so tests outside this package can cross-check
// their own per-type registries (e.g. internal/projection/apply.go's
// ApplyEvent switch, internal/authority/postgres.go's own decodePayload
// switch) against this one, authoritative list -- catching a type that's
// missing from one of those the moment it's added here, rather than only
// when someone happens to exercise it against that specific backend. Order
// is unspecified.
func registeredEventTypes() []string {
	types := make([]string, 0, len(payloadFactories))
	for typ := range payloadFactories {
		types = append(types, typ)
	}
	return types
}

func TestDecodePayloadValueCoversEveryRegisteredType(t *testing.T) {
	for _, typ := range registeredEventTypes() {
		typ := typ
		t.Run(typ, func(t *testing.T) {
			v, err := DecodePayloadValue(typ, json.RawMessage("{}"))
			if err != nil {
				t.Fatalf("DecodePayloadValue(%q): %v", typ, err)
			}
			if reflect.ValueOf(v).Kind() == reflect.Pointer {
				t.Fatalf("DecodePayloadValue(%q) returned a pointer, want a value", typ)
			}
		})
	}
}
