package main

import (
	"context"
	"dotty/internal/live"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"time"
)

// conversationWindow caps accidental activation as well as post-reply silence.
// It is evaluated locally; no cloud transcription is needed to stop uploading.
type conversationWindow struct {
	started, lastActivity time.Time
	idle, maximum         time.Duration
	noiseFloor            float64
}

func newConversationWindow(now time.Time) conversationWindow {
	return conversationWindow{started: now, lastActivity: now, idle: 20 * time.Second, maximum: 5 * time.Minute, noiseFloor: 50}
}
func (w *conversationWindow) observe(now time.Time, speaking bool, pcm []byte) {
	if speaking {
		w.lastActivity = now
		return
	}
	if len(pcm) == 0 {
		return
	}
	rms := speechEnergy(pcm)
	// Adapt to sustained noise instead of treating a loud fan as speech forever.
	// This is an energy heuristic, not a claim of speech recognition/VAD.
	alpha := 0.02
	if rms < w.noiseFloor {
		alpha = 0.05
	}
	w.noiseFloor += alpha * (math.Min(rms, math.Max(50, w.noiseFloor*3)) - w.noiseFloor)
	if rms > math.Max(150, w.noiseFloor*3) {
		w.lastActivity = now
	}
}
func (w conversationWindow) expired(now time.Time) bool {
	return now.Sub(w.started) >= w.maximum || now.Sub(w.lastActivity) >= w.idle
}
func speechEnergy(pcm []byte) float64 {
	if len(pcm) < 2 {
		return 0
	}
	var squares float64
	for i := 0; i+1 < len(pcm); i += 2 {
		v := float64(int16(binary.LittleEndian.Uint16(pcm[i:])))
		squares += v * v
	}
	// Compare RMS without introducing a shared stateful noise estimator.
	return math.Sqrt(squares / float64(len(pcm)/2))
}

type keywordDetector interface {
	Reset() error
	Process([]byte) (bool, error)
}

func waitForWake(ctx context.Context, d keywordDetector, p *player, s *sessionState, events <-chan uint16, mic <-chan []byte, errs <-chan error, states chan string) error {
	if err := d.Reset(); err != nil {
		return err
	}
	for len(mic) > 0 {
		<-mic
	}
	if s.attention {
		show(states, "error")
	} else {
		show(states, "waiting")
	}
	log.Print("Waiting for local Hey Orion or Action; cloud session closed; microphone upload=off")
	lastDiagnostic := time.Now()
	var packets, skipped int
	var maxRMS float64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errs:
			return fmt.Errorf("%w: %v", errHardware, err)
		case key := <-events:
			switch key {
			case privacyMuted:
				s.muted = true
				log.Print("Muted while waiting; microphone upload=off")
				return nil
			case 114, 115:
				if err := s.adjustVolume(key); err != nil {
					return err
				}
			case 138:
				s.attention = false
				s.awake = true
				s.firstWake = true
				log.Print("Action activated conversation")
				return nil
			}
		case pcm := <-mic:
			busy := p.busy()
			if s.diagnostics {
				packets++
				maxRMS = math.Max(maxRMS, speechEnergy(pcm))
				if busy {
					skipped++
				}
				if time.Since(lastDiagnostic) >= 5*time.Second {
					log.Printf("wake diagnostic: mic_packets=%d skipped_playback=%d max_packet_rms=%.1f microphone_upload=off", packets, skipped, maxRMS)
					packets, skipped, maxRMS = 0, 0, 0
					lastDiagnostic = time.Now()
				}
			}
			if busy {
				continue
			}
			found, err := d.Process(pcm)
			if err != nil {
				return err
			}
			if found {
				s.attention = false
				s.awake = true
				s.firstWake = true
				log.Print("Local Hey Orion detected; activating conversation")
				return nil
			}
		}
	}
}

// Keep mute responsive while DNS, TLS and setup are in flight. No microphone
// reaches Gemini until setup succeeds and this control loop has returned.
func dialWithControls(ctx context.Context, cfg live.Config, s *sessionState, keys <-chan uint16, errs <-chan error) (*live.Connection, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	type result struct {
		conn *live.Connection
		err  error
	}
	done := make(chan result, 1)
	go func() { c, e := live.Dial(dialCtx, cfg); done <- result{c, e} }()
	abort := func(err error) (*live.Connection, error) {
		cancel()
		r := <-done
		if r.conn != nil {
			r.conn.Close()
		}
		return nil, err
	}
	for {
		select {
		case <-ctx.Done():
			return abort(ctx.Err())
		case err := <-errs:
			return abort(fmt.Errorf("%w: %v", errHardware, err))
		case key := <-keys:
			if key == privacyMuted {
				s.muted = true
				log.Print("Muted during connection setup; microphone upload=off")
				return abort(nil)
			}
			if key == 114 || key == 115 {
				if err := s.adjustVolume(key); err != nil {
					return abort(fmt.Errorf("%w: %v", errHardware, err))
				}
			}
		case r := <-done:
			return r.conn, r.err
		}
	}
}
