package main

import (
	"context"
	"dotty/internal/privacy"
	"fmt"
	"log"
	"time"
)

// The physical privacy circuit, including the button light, is authoritative.
// Never synthesize KEY_MUTE to restore state: that changes only the application.

// Explicit levels, distinct from Linux key codes. Duplicate events cannot toggle.
const (
	privacyMuted uint16 = 0xff01 + iota
	privacyUnmuted
)

func readPrivacyState() (bool, error) {
	return privacy.Read()
}

func parsePrivacyState(value string) (bool, error) {
	return privacy.Parse(value)
}

func watchPrivacy(ctx context.Context, read func() (bool, error), previous bool, events chan<- uint16) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		muted, err := read()
		if err != nil {
			return fmt.Errorf("%w: %v", errHardware, err)
		}
		if muted != previous {
			event := privacyUnmuted
			if muted {
				event = privacyMuted
			}
			select {
			case events <- event:
				previous = muted
				log.Printf("Hardware privacy changed: muted=%t", muted)
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
