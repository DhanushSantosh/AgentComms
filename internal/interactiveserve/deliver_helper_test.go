package interactiveserve

import "time"

// deliverToPty writes message into w as terminal input: waits for the
// target to be idle (see isBusy) before sending anything — the direct
// replacement for the old cross-process delivery lock, since there's now
// only ever one process touching this pty, concurrent senders just make
// concurrent socket connections and this mutex-guarded function serializes
// them — then writes the text and a separate Enter only once tee has
// visibly reflected the text back, never blind. idleTO/echoTO are the real
// package constants in production; tests call this directly with short
// overrides rather than waiting out the real, deliberately generous values.
func deliverToPty(w writeFlusher, tee *outputTee, message string, idleTO, echoTO time.Duration) error {
	_, err := deliverToPtyWithEvidence(w, tee, message, idleTO, echoTO)
	return err
}
