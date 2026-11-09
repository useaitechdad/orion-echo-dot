package main

import (
	"context"
	"dotty/internal/live"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestContinuousModeRequiresExplicitConsent(t *testing.T) {
	for _, tc := range []struct {
		o    options
		fail bool
		wake bool
	}{
		{options{mode: "live", wakeEnabled: true}, true, true},
		{options{mode: "live", wakeEnabled: true, wakeThreshold: -1}, true, true},
		{options{mode: "live", wakeEnabled: true, wakeThreshold: 1}, true, true},
		{options{mode: "live", wakeEnabled: true, wakeThreshold: .98}, false, true},
		{options{mode: "live"}, true, false},
		{options{mode: "live", wakeEnabled: true, continuous: true}, false, false},
		{options{mode: "speaker-test", wakeEnabled: true}, false, false},
	} {
		err := normalizeOptions(&tc.o)
		if (err != nil) != tc.fail || tc.o.wakeEnabled != tc.wake {
			t.Fatalf("options=%+v err=%v", tc.o, err)
		}
	}
}

func TestConnectionRetriesAreBoundedAndMuteCancelsBackoff(t *testing.T) {
	for _, mute := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			var calls atomic.Int32
			keys := make(chan uint16, 1)
			state := &sessionState{}
			ops := connectionOps{
				privacy: func() (bool, error) { return false, nil },
				dns:     func(context.Context, string) (string, error) { return "", nil },
				dial: func(context.Context, live.Config, *sessionState, <-chan uint16, <-chan error) (*live.Connection, error) {
					calls.Add(1)
					return nil, errors.New("offline")
				},
			}
			done := make(chan error, 1)
			go func() {
				_, err := connectUsing(context.Background(), live.Config{Key: "test-key"}, state, keys, make(chan error), make(chan string, 1), ops)
				done <- err
			}()
			synctest.Wait()
			if mute {
				keys <- privacyMuted
			}
			time.Sleep(5 * time.Second)
			synctest.Wait()
			err := <-done
			if mute {
				if calls.Load() != 1 || !state.muted || err != nil {
					t.Fatalf("mute retry calls=%d err=%v", calls.Load(), err)
				}
			} else if calls.Load() != 3 || err == nil {
				t.Fatalf("retry count=%d err=%v", calls.Load(), err)
			}
		})
	}
}

func TestUnknownPrivacyNeverStartsConnection(t *testing.T) {
	ops := connectionOps{privacy: func() (bool, error) { return false, errors.New("invalid") }}
	_, err := connectUsing(context.Background(), live.Config{}, &sessionState{}, nil, nil, make(chan string, 1), ops)
	if !errors.Is(err, errHardware) {
		t.Fatalf("%v", err)
	}
}

func TestNetworkReadinessDoesNotConsumeConnectionAttempts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		dials := 0
		ops := connectionOps{
			privacy: func() (bool, error) { return false, nil },
			dns: func(context.Context, string) (string, error) {
				if time.Since(started) < 35*time.Second {
					return "", errNetworkNotReady
				}
				return "192.0.2.1:53", nil
			},
			dial: func(_ context.Context, c live.Config, _ *sessionState, _ <-chan uint16, _ <-chan error) (*live.Connection, error) {
				dials++
				if c.DNS != "192.0.2.1:53" {
					t.Errorf("missing refreshed DNS: %q", c.DNS)
				}
				return nil, nil
			},
		}
		states := make(chan string, 1)
		_, err := connectUsing(context.Background(), live.Config{DNS: "auto"}, &sessionState{}, nil, nil, states, ops)
		if err != nil || dials != 1 || time.Since(started) != 35*time.Second || <-states != "connecting" {
			t.Fatalf("readiness err=%v dials=%d elapsed=%v", err, dials, time.Since(started))
		}
	})
}

func TestNetworkReadinessTimeoutIsBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		started := time.Now()
		ops := connectionOps{
			privacy: func() (bool, error) { return false, nil },
			dns:     func(context.Context, string) (string, error) { return "", errNetworkNotReady },
			dial: func(context.Context, live.Config, *sessionState, <-chan uint16, <-chan error) (*live.Connection, error) {
				t.Error("dial before network readiness")
				return nil, nil
			},
		}
		_, err := connectUsing(context.Background(), live.Config{DNS: "auto"}, &sessionState{}, nil, nil, make(chan string, 1), ops)
		if !errors.Is(err, errNetworkNotReady) || time.Since(started) != time.Minute {
			t.Fatalf("readiness deadline err=%v elapsed=%v", err, time.Since(started))
		}
	})
}

func TestMuteCancelsBlockedNetworkReadiness(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		keys := make(chan uint16, 1)
		cancelled := make(chan struct{})
		state := &sessionState{}
		ops := connectionOps{
			privacy: func() (bool, error) { return false, nil },
			dns: func(ctx context.Context, _ string) (string, error) {
				<-ctx.Done()
				close(cancelled)
				return "", ctx.Err()
			},
			dial: func(context.Context, live.Config, *sessionState, <-chan uint16, <-chan error) (*live.Connection, error) {
				t.Error("dial after mute")
				return nil, nil
			},
		}
		done := make(chan error, 1)
		go func() {
			_, err := connectUsing(context.Background(), live.Config{}, state, keys, nil, make(chan string, 1), ops)
			done <- err
		}()
		synctest.Wait()
		keys <- privacyMuted
		synctest.Wait()
		if err := <-done; err != nil || !state.muted {
			t.Fatalf("mute state=%t err=%v", state.muted, err)
		}
		select {
		case <-cancelled:
		default:
			t.Fatal("network child not cancelled/reaped")
		}
	})
}

func TestDynamicDNSAcceptsIPv6AndWaitsForNetwork(t *testing.T) {
	dns, err := discoverDNS(func(p string) (string, error) {
		if strings.HasSuffix(p, "dns1") {
			return "", nil
		}
		return "2001:db8::1", nil
	})
	if err != nil || dns != "[2001:db8::1]:53" {
		t.Fatalf("%s %v", dns, err)
	}
	if _, err = discoverDNS(func(string) (string, error) { return "", nil }); err == nil {
		t.Fatal("missing network accepted")
	}
}

func TestAttentionRetryCannotOverrideMute(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var muted atomic.Bool
		muted.Store(true)
		events := make(chan uint16, 4)
		states := make(chan string, 1)
		done := make(chan error, 1)
		go func() {
			done <- recoveryControls(ctx, time.Second, true, func() (bool, error) { return muted.Load(), nil }, events, make(chan error), states)
		}()
		synctest.Wait()
		if <-states != "muted" {
			t.Fatal("mute did not take priority")
		}
		events <- 138
		time.Sleep(2 * time.Second)
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("Action bypassed mute")
		default:
		}
		muted.Store(false)
		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		if <-states != "error" {
			t.Fatal("attention not restored on unmute")
		}
		events <- 138
		synctest.Wait()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

func TestUnknownPrivacyDoesNotPermitActionRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		events := make(chan uint16, 1)
		states := make(chan string, 1)
		done := make(chan error, 1)
		go func() {
			done <- recoveryControls(ctx, time.Second, true, func() (bool, error) { return false, errors.New("unreadable") }, events, make(chan error), states)
		}()
		synctest.Wait()
		if <-states != "error" {
			t.Fatal("missing failure cue")
		}
		events <- 138
		time.Sleep(2 * time.Second)
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("unknown privacy permitted retry")
		default:
		}
		cancel()
		synctest.Wait()
		<-done
	})
}

func TestSupervisorReapsChildOnCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := runChild(ctx, "/bin/sh", []string{"-c", "sleep 60"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("%v", err)
	}
	if err := runChild(context.Background(), "/bin/sh", []string{"-c", "exit 7"}); err == nil {
		t.Fatal("child failure hidden")
	}
}
