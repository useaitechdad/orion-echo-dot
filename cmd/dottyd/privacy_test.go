package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestPrivacyStateRejectsUnknownValues(t *testing.T) {
	for _, value := range []string{"", "-1", "2", "unknown", "0\n1"} {
		if _, err := parsePrivacyState(value); err == nil {
			t.Fatalf("accepted invalid privacy state %q", value)
		}
	}
	for value, want := range map[string]bool{"0\n": false, "1\n": true} {
		got, err := parsePrivacyState(value)
		if err != nil || got != want {
			t.Fatalf("%q = %t, %v", value, got, err)
		}
	}
}

func TestPrivacyWatcherReportsLevelsAndFailsClosed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var hardware atomic.Int32
		hardware.Store(1)
		events := make(chan uint16, 8)
		done := make(chan error, 1)
		go func() {
			done <- watchPrivacy(ctx, func() (bool, error) {
				v := hardware.Load()
				if v == 2 {
					return false, errors.New("unreadable privacy state")
				}
				return v == 1, nil
			}, true, events)
		}()
		expect := func(want uint16) {
			t.Helper()
			synctest.Wait()
			select {
			case got := <-events:
				if got != want {
					t.Fatalf("event %x want %x", got, want)
				}
			default:
				t.Fatalf("missing event %x", want)
			}
		}
		none := func() {
			t.Helper()
			synctest.Wait()
			select {
			case got := <-events:
				t.Fatalf("duplicate event %x", got)
			default:
			}
		}
		time.Sleep(time.Second)
		none() // Startup already adopted hardware mute; polling cannot toggle it.
		hardware.Store(0)
		time.Sleep(100 * time.Millisecond)
		expect(privacyUnmuted)
		time.Sleep(time.Second)
		none()
		hardware.Store(1)
		time.Sleep(100 * time.Millisecond)
		expect(privacyMuted)
		hardware.Store(2)
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		if err := <-done; !errors.Is(err, errHardware) {
			t.Fatalf("not a fatal hardware error: %v", err)
		}
		none() // Read failure must not become an unmute event.
	})
}
