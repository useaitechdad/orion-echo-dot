package main

import (
	"context"
	"testing"
	"testing/synctest"
	"time"
)

// Fake time verifies that an old mute timer cannot overwrite a later state.
func TestMuteDimmingTransitions(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		states := make(chan string)
		colors := make(chan [3]byte, 16)
		go runLEDStates(ctx, states, func(rgb [3]byte) error {
			select {
			case colors <- rgb:
			case <-ctx.Done():
			}
			return nil
		}, 2*time.Minute)
		expect := func(want [3]byte) {
			t.Helper()
			synctest.Wait()
			select {
			case got := <-colors:
				if got != want {
					t.Fatalf("color=%v want=%v", got, want)
				}
			default:
				t.Fatalf("missing color %v", want)
			}
		}
		unchanged := func() {
			t.Helper()
			synctest.Wait()
			select {
			case got := <-colors:
				t.Fatalf("unexpected color %v", got)
			default:
			}
		}
		states <- "muted"
		expect([3]byte{40, 0, 0})
		time.Sleep(90 * time.Second)
		unchanged()
		states <- "muted" // Repeated status after a volume event must not reset age.
		time.Sleep(31 * time.Second)
		expect([3]byte{3, 0, 0})
		states <- "waiting"
		expect([3]byte{0, 0, 3})
		states <- "muted"
		expect([3]byte{40, 0, 0})
		time.Sleep(time.Minute)
		states <- "listening" // Unmute before expiry cancels the dim update.
		expect([3]byte{32, 32, 32})
		time.Sleep(2 * time.Minute)
		unchanged()
		states <- "muted"
		expect([3]byte{40, 0, 0})
		states <- "error"
		expect([3]byte{40, 0, 0})
		time.Sleep(time.Second)
		expect([3]byte{})
		time.Sleep(time.Second)
		expect([3]byte{40, 0, 0})
		states <- "muted" // Mute overrides flashing and keeps its own dim timer.
		expect([3]byte{40, 0, 0})
		time.Sleep(3 * time.Second)
		unchanged()
		time.Sleep(2 * time.Minute)
		expect([3]byte{3, 0, 0})
		states <- "waiting"
		expect([3]byte{0, 0, 3})
		time.Sleep(3 * time.Second)
		unchanged()
	})
}
