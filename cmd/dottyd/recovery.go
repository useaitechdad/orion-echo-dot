package main

import (
	"context"
	"dotty/internal/live"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"os/exec"
	"strings"
	"time"
)

func normalizeOptions(o *options) error {
	if o.mode != "live" {
		if o.continuous {
			return fmt.Errorf("continuous-listening requires live mode")
		}
		o.wakeEnabled = false
		return nil
	}
	if o.continuous {
		o.wakeEnabled = false
	}
	if !o.wakeEnabled && !o.continuous {
		return fmt.Errorf("disabling wake requires explicit -continuous-listening")
	}
	if o.wakeEnabled && (o.wakeThreshold <= 0 || o.wakeThreshold >= 1 || math.IsNaN(o.wakeThreshold) || math.IsInf(o.wakeThreshold, 0)) {
		return fmt.Errorf("set -wake-threshold explicitly for your validated helper/model")
	}
	return nil
}

func refreshDNS(ctx context.Context, configured string) (string, error) {
	if configured != "auto" {
		return configured, nil
	}
	return discoverDNS(func(name string) (string, error) {
		call, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		b, err := exec.CommandContext(call, "/system/bin/getprop", name).Output()
		return strings.TrimSpace(string(b)), err
	})
}

var errNetworkNotReady = errors.New("network DNS/gateway not ready")

func discoverDNS(read func(string) (string, error)) (string, error) {
	for _, property := range []string{"dhcp.wlan0.dns1", "dhcp.wlan0.gateway"} {
		value, err := read(property)
		if err == nil && net.ParseIP(value) != nil {
			return net.JoinHostPort(value, "53"), nil
		}
	}
	return "", errNetworkNotReady
}

type connectionOps struct {
	privacy func() (bool, error)
	dns     func(context.Context, string) (string, error)
	dial    func(context.Context, live.Config, *sessionState, <-chan uint16, <-chan error) (*live.Connection, error)
}

func connectWithRecovery(ctx context.Context, cfg live.Config, s *sessionState, events <-chan uint16, errs <-chan error, states chan string) (*live.Connection, error) {
	return connectUsing(ctx, cfg, s, events, errs, states, connectionOps{readPrivacyState, refreshDNS, dialWithControls})
}

func connectUsing(ctx context.Context, cfg live.Config, s *sessionState, events <-chan uint16, errs <-chan error, states chan string, ops connectionOps) (*live.Connection, error) {
	var last error
	// DHCP on this firmware can take ~30 seconds after the worker starts.
	// Missing network configuration is readiness, not a failed Gemini attempt.
	for attempt := 1; attempt <= 3; attempt++ {
		muted, err := ops.privacy()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errHardware, err)
		}
		if muted {
			s.muted = true
			return nil, nil
		}
		show(states, "connecting")
		current := cfg
		readinessWait := time.Minute
		if attempt > 1 {
			// Subsequent dials still refresh DHCP data, but do not restart
			// the initial boot-readiness allowance.
			readinessWait = 3 * time.Second
		}
		current.DNS, last = waitNetworkDNS(ctx, time.Now().Add(readinessWait), cfg.DNS, s, events, errs, ops)
		if s.muted || ctx.Err() != nil || errors.Is(last, errHardware) || errors.Is(last, errNetworkNotReady) {
			return nil, last
		}
		if last == nil {
			// Network readiness may have taken a while; reread the circuit
			// before allowing any cloud connection.
			muted, err = ops.privacy()
			if err != nil {
				return nil, fmt.Errorf("%w: %v", errHardware, err)
			}
			if muted {
				s.muted = true
				return nil, nil
			}
			var c *live.Connection
			c, last = ops.dial(ctx, current, s, events, errs)
			if last == nil {
				s.attention = false
				return c, nil
			}
		}
		if ctx.Err() != nil || s.muted {
			return nil, ctx.Err()
		}
		if errors.Is(last, errHardware) {
			return nil, last
		}
		log.Printf("Connection attempt %d/3 failed: %s", attempt, strings.ReplaceAll(last.Error(), cfg.Key, "[redacted]"))
		if attempt == 3 {
			break
		}
		timer := time.NewTimer(time.Duration(attempt) * time.Second)
		waiting := true
		for waiting {
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case e := <-errs:
				timer.Stop()
				return nil, fmt.Errorf("%w: %v", errHardware, e)
			case <-timer.C:
				waiting = false
			case key := <-events:
				if key == privacyMuted {
					s.muted = true
					timer.Stop()
					return nil, nil
				}
				if key == 114 || key == 115 {
					_ = s.adjustVolume(key)
				}
			}
		}
	}
	return nil, last
}

// Resolve readiness asynchronously so even a blocked getprop is cancellable
// by hardware mute. The caller shows amber and sends no microphone audio.
func waitNetworkDNS(ctx context.Context, deadline time.Time, configured string, s *sessionState, events <-chan uint16, errs <-chan error, ops connectionOps) (string, error) {
	waitCtx, cancel := context.WithDeadline(ctx, deadline)
	type result struct {
		dns string
		err error
	}
	resultCh := make(chan result, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		announced := false
		for {
			dns, err := ops.dns(waitCtx, configured)
			if !errors.Is(err, errNetworkNotReady) {
				resultCh <- result{dns, err}
				return
			}
			if !announced {
				log.Print("Waiting for network readiness; amber; microphone upload=off")
				announced = true
			}
			select {
			case <-waitCtx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}()
	defer func() { cancel(); <-finished }()
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			return "", fmt.Errorf("%w: readiness deadline exceeded", errNetworkNotReady)
		case <-tick.C:
			muted, err := ops.privacy()
			if err != nil {
				return "", fmt.Errorf("%w: %v", errHardware, err)
			}
			if muted {
				s.muted = true
				return "", nil
			}
		case err := <-errs:
			return "", fmt.Errorf("%w: %v", errHardware, err)
		case key := <-events:
			if key == privacyMuted {
				s.muted = true
				return "", nil
			}
			if key == 114 || key == 115 {
				_ = s.adjustVolume(key)
			}
		case r := <-resultCh:
			if waitCtx.Err() != nil {
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				return "", fmt.Errorf("%w: readiness deadline exceeded", errNetworkNotReady)
			}
			return r.dns, r.err
		}
	}
}
