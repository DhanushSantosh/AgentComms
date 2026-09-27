package model

import (
	"fmt"
	"sort"
	"strings"
)

// ResolvePrincipal turns a human-typed reference into a canonical actor ID.
//
// RFC 0039: a principal can be named by its actor ID or by its display
// name. Actor ID wins outright -- it is unique by construction -- and
// display names are matched case-insensitively only when nothing matched by
// ID, so adding a display name can never shadow someone else's ID.
//
// Resolution lives at the CLI/service boundary and never in the protocol:
// events always record the canonical actor ID. A signed record naming
// "Atlas" would become ambiguous the moment that display name is reused or
// changed, and agent rename already allows both.
func ResolvePrincipal(agents map[string]Agent, reference string) (string, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return "", fmt.Errorf("a principal is required")
	}
	if _, ok := agents[reference]; ok {
		return reference, nil
	}

	matches := make([]string, 0, 2)
	for id, agent := range agents {
		if strings.EqualFold(strings.TrimSpace(agent.DisplayName), reference) {
			matches = append(matches, id)
		}
	}
	sort.Strings(matches)

	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf("no principal %q: not an actor ID, and no display name matches", reference)
	default:
		// Ambiguity is reported, never guessed: picking one would attribute
		// a signed event to a principal the caller did not choose.
		return "", fmt.Errorf("%q is the display name of %d principals (%s); use the actor ID",
			reference, len(matches), strings.Join(matches, ", "))
	}
}
