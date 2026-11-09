package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeKeyword struct {
	ready     chan struct{}
	processed chan struct{}
	found     bool
	err       error
}

func (d *fakeKeyword) Reset() error                 { close(d.ready); return nil }
func (d *fakeKeyword) Process([]byte) (bool, error) { d.processed <- struct{}{}; return d.found, d.err }
func TestLocalWaitingOnlyActivatesOnKeywordOrAction(t *testing.T) {
	for _, scenario := range []string{"unrelated", "keyword", "action", "mute", "raw-mute", "failed-detector"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			detector := &fakeKeyword{ready: make(chan struct{}), processed: make(chan struct{}, 1), found: scenario == "keyword"}
			if scenario == "failed-detector" {
				detector.err = errors.New("helper failed")
			}
			state := sessionState{wakeEnabled: true}
			mic := make(chan []byte)
			events := make(chan uint16)
			done := make(chan error, 1)
			go func() {
				done <- waitForWake(ctx, detector, newPlayer(), &state, events, mic, make(chan error), make(chan string, 1))
			}()
			<-detector.ready
			switch scenario {
			case "action":
				events <- 138
			case "mute":
				events <- privacyMuted
			case "raw-mute":
				events <- 113 // Synthetic Linux key events cannot alter hardware privacy.
			default:
				mic <- make([]byte, 1024)
				<-detector.processed
			}
			if scenario == "unrelated" || scenario == "raw-mute" {
				select {
				case err := <-done:
					t.Fatalf("unrelated audio exited waiting: %v", err)
				default:
				}
				cancel()
			}
			select {
			case err := <-done:
				if scenario == "failed-detector" && err == nil {
					t.Fatal("helper failure did not fail closed")
				}
				want := scenario == "keyword" || scenario == "action"
				if state.awake != want || state.firstWake != want {
					t.Fatalf("activation=%t preroll=%t, want %t", state.awake, state.firstWake, want)
				}
				if state.muted != (scenario == "mute") {
					t.Fatal("mute state")
				}
			case <-time.After(time.Second):
				t.Fatal("waiting did not finish")
			}
		})
	}
}
func TestConversationEndsAfterIdleAndCannotStayOpenForever(t *testing.T) {
	start := time.Unix(100, 0)
	w := newConversationWindow(start)
	if w.expired(start.Add(19 * time.Second)) {
		t.Fatal("closed before follow-up window")
	}
	if !w.expired(start.Add(20 * time.Second)) {
		t.Fatal("idle upload window did not expire")
	}
	w.observe(start.Add(18*time.Second), true, nil)
	if w.expired(start.Add(30 * time.Second)) {
		t.Fatal("playback did not refresh follow-up window")
	}
	w.observe(start.Add(299*time.Second), true, nil)
	if !w.expired(start.Add(300 * time.Second)) {
		t.Fatal("maximum session length bypassed by constant sound")
	}
}
