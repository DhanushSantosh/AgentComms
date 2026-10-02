package model

import "sort"

// SortedIDsBySequence returns entity IDs in reverse signed-event order.
// The ID tie-break keeps legacy or same-sequence projections deterministic.
func SortedIDsBySequence[T any](values map[string]T, sequence func(T) uint64) []string {
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		left, right := sequence(values[ids[i]]), sequence(values[ids[j]])
		if left != right {
			return left > right
		}
		return ids[i] < ids[j]
	})
	return ids
}

// InboxOptions narrows a principal's inbox. Limit zero means unlimited.
type InboxOptions struct {
	Unread bool
	From   string
	Limit  int
}

// Inbox returns the messages addressed to actor, newest posted first by
// signed creation sequence (RFC 0041), filtered and then limited. It is
// shared by the CLI and MCP so both transports return the same order.
// addressed reports whether anything at all is addressed to actor, so a
// caller can tell "nothing yet" from "nothing matches this filter".
func Inbox(st State, actor string, options InboxOptions) (messages map[string]Message, order []string, addressed bool) {
	messages = map[string]Message{}
	for id, m := range st.Messages {
		toActor := false
		for _, to := range m.To {
			if to == actor {
				toActor = true
				break
			}
		}
		if !toActor {
			continue
		}
		addressed = true
		// Unread is this recipient's own pending obligation, not the
		// message's aggregate status: a two-recipient ACTION stays OPEN
		// until everyone responds. FYI never creates an obligation.
		if options.Unread {
			pending := false
			for _, recipient := range m.Recipients {
				if recipient.Principal == actor && recipient.Status == "PENDING" {
					pending = true
					break
				}
			}
			if !pending {
				continue
			}
		}
		if options.From != "" && m.From != options.From {
			continue
		}
		messages[id] = m
	}
	order = SortedIDsBySequence(messages, func(m Message) uint64 { return m.CreatedSequence })
	if options.Limit > 0 && len(order) > options.Limit {
		order = order[:options.Limit]
		limited := make(map[string]Message, len(order))
		for _, id := range order {
			limited[id] = messages[id]
		}
		messages = limited
	}
	return messages, order, addressed
}
