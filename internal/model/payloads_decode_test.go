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
func TestDecodePayloadValueCoversEveryRegisteredType(t *testing.T) {
	for _, typ := range RegisteredEventTypes() {
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
