package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"

	"dotty/internal/audio"
)

// A separate process owns recovery, so a worker crash cannot erase its error cue.
// It never opens microphones, reads credentials, or connects to Gemini.
func supervise(ctx context.Context, executable string, args []string, wifiNetwork string) error {
	lock, err := os.OpenFile("/data/local/tmp/dotty-supervisor.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("supervisor already running: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	networkDone := make(chan struct{})
	go func() {
		defer close(networkDone)
		if wifiNetwork != "" {
			configureWiFi(ctx, wifiNetwork)
		}
	}()
	defer func() { cancel(); <-networkDone }()
	failures := 0
	for ctx.Err() == nil {
		started := time.Now()
		err = runChild(ctx, executable, args)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Since(started) >= 5*time.Minute {
			failures = 0
		}
		failures++
		log.Printf("Voice worker exited (%v); recovery failure count=%d", err, failures)
		// Defensive amp shutdown after even SIGKILL, before claiming mute/error.
		if e := amp(false); e != nil {
			log.Printf("recovery amp shutdown: %v", e)
		}
		delay := time.Duration(1<<min(failures, 3)) * time.Second
		attention := failures >= 3
		if attention {
			log.Print("Recovery exhausted; flashing red when unmuted; Action retries")
		}
		if err = waitRecovery(ctx, delay, attention); err != nil {
			return err
		}
		if attention {
			failures = 0
		}
	}
	return ctx.Err()
}

func runChild(ctx context.Context, executable string, args []string) error {
	cmd := exec.Command(executable, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.SysProcAttr = workerProcessAttributes()
	if err := cmd.Start(); err != nil {
		return err
	}
	// Kill any orphaned player/helper in the worker's private process group.
	defer syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
		return ctx.Err()
	}
}

func waitRecovery(ctx context.Context, delay time.Duration, attention bool) error {
	ring, err := audio.OpenRing()
	if err != nil {
		log.Printf("Recovery LED unavailable: %v", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	states := make(chan string, 1)
	ledDone := make(chan struct{})
	go func() {
		defer close(ledDone)
		if ring != nil {
			runLEDStates(ctx, states, ring.Set, 2*time.Minute)
		}
	}()
	events, errs := make(chan uint16, 8), make(chan error, 2)
	keyDone := make(chan struct{})
	go func() { defer close(keyDone); keys(ctx, "/dev/input/event1", true, events, errs) }()
	defer func() {
		cancel()
		<-keyDone
		<-ledDone
		if ring != nil {
			ring.Close()
		}
	}()
	return recoveryControls(ctx, delay, attention, readPrivacyState, events, errs, states)
}

func recoveryControls(ctx context.Context, delay time.Duration, attention bool, read func() (bool, error), events <-chan uint16, errs <-chan error, states chan string) error {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	timer := time.NewTimer(delay)
	defer timer.Stop()
	var retry <-chan time.Time
	if !attention {
		retry = timer.C
	}
	refresh := func() {
		muted, err := read()
		if err == nil && muted {
			show(states, "muted")
		} else if err != nil || attention {
			show(states, "error")
		} else {
			show(states, "connecting")
		}
	}
	refresh()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			refresh()
		case <-retry:
			return nil // Restart remains safe while muted; worker rereads privacy.
		case err := <-errs:
			log.Printf("Recovery controls: %v; restart device if Action unavailable", err)
		case key := <-events:
			if key == 138 {
				muted, err := read()
				if err == nil && !muted {
					return nil
				}
			}
		}
	}
}
