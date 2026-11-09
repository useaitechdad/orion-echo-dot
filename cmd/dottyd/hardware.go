package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"dotty/internal/audio"
)

func command(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %w (%s)", args, err, out)
	}
	return nil
}
func mix(name string, values ...string) error {
	return command(append([]string{"/system/bin/tinymix", name}, values...)...)
}
func initHardware() error {
	for _, svc := range []string{"mixer", "uxeventd", "neo-coordinator", "puffinmrmd", "ace_eventmgr", "boot2_anim_start", "ledcontroller"} {
		if err := command("/system/bin/stop", svc); err != nil {
			log.Printf("service stop: %v", err)
		}
	}
	controls := [][]string{
		{"MFP Gpio Mute", "Off"}, {"Ext_Speaker_Amp_Switch", "Off"},
		{"Audio_I2S1_Setting", "On"}, {"Audio_DacMux_Setting", "Off"},
		{"Headphone_Speaker_Mux", "Speaker"}, {"Ext_Amp_Gain", "6dB"},
		{"HP DAC Playback Switch", "1", "1"},
		{"HPR Output Mixer R_DAC Switch", "1"}, {"HPL Output Mixer L_DAC Switch", "1"},
		{"Right Channel Only", "On"}, {"Ignore Ramp Up", "On"}, {"Auto-mute Switch", "3"},
	}
	for _, c := range controls {
		if err := mix(c[0], c[1:]...); err != nil {
			return fmt.Errorf("audio initialization: %w", err)
		}
	}
	return nil
}
func initMicrophones() error { return audio.ConfigureMicrophones(context.Background()) }
func amp(on bool) error {
	if on {
		if err := mix("Ext_Speaker_Amp_Switch", "On"); err != nil {
			return err
		}
		if err := mix("MFP Gpio Mute", "On"); err != nil {
			_ = mix("Ext_Speaker_Amp_Switch", "Off")
			return err
		}
		return nil
	}
	return errors.Join(mix("MFP Gpio Mute", "Off"), mix("Ext_Speaker_Amp_Switch", "Off"))
}

var volumeTable = [][2]int{{0, 0}, {50, 0}, {70, 1}, {90, 1}, {110, 2}, {127, 6}, {140, 3}, {150, 4}, {160, 5}, {168, 6}, {175, 8}}

func setVolume(level int) error {
	v := volumeTable[level]
	if err := mix("PCM Playback Volume", fmt.Sprint(v[0]), fmt.Sprint(v[0])); err != nil {
		return err
	}
	if err := mix("HP Driver Gain Volume", fmt.Sprint(v[1]), fmt.Sprint(v[1])); err != nil {
		return err
	}
	log.Printf("volume=%d/10", level)
	return nil
}

// LED changes are coalesced and processed away from the real-time audio loop.
func ledWorker(ctx context.Context, states <-chan string) {
	ring, err := audio.OpenRing()
	if err != nil {
		log.Printf("LED open: %v", err)
		return
	}
	defer ring.Close()
	runLEDStates(ctx, states, ring.Set, 2*time.Minute)
}

func runLEDStates(ctx context.Context, states <-chan string, set func([3]byte) error, dimAfter time.Duration) {
	last := ""
	var dimTimer *time.Timer
	var dim <-chan time.Time
	var flashTimer *time.Ticker
	var flash <-chan time.Time
	flashOn := true
	defer func() {
		if flashTimer != nil {
			flashTimer.Stop()
		}
		if dimTimer != nil {
			dimTimer.Stop()
		}
	}()
	apply := func(rgb [3]byte) {
		if err := set(rgb); err != nil {
			log.Printf("LED: %v", err)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-flash:
			flashOn = !flashOn
			if flashOn {
				apply([3]byte{40, 0, 0})
			} else {
				apply([3]byte{})
			}
		case <-dim:
			dim = nil
			apply([3]byte{3, 0, 0})
			log.Print("Mute indicator dimmed; still muted")
		case state, ok := <-states:
			if !ok {
				return
			}
			if state == last {
				continue
			}
			if flashTimer != nil {
				flashTimer.Stop()
				flashTimer = nil
				flash = nil
			}
			if dimTimer != nil {
				dimTimer.Stop()
			}
			dim = nil
			last = state
			rgb := [3]byte{}
			switch state {
			case "waiting":
				rgb = [3]byte{0, 0, 3}
			case "listening":
				rgb = [3]byte{32, 32, 32}
			case "speaking":
				rgb = [3]byte{0, 40, 0}
			case "muted":
				rgb = [3]byte{40, 0, 0}
				dimTimer = time.NewTimer(dimAfter)
				dim = dimTimer.C
			case "error":
				rgb = [3]byte{40, 0, 0}
				flashOn = true
				flashTimer = time.NewTicker(time.Second)
				flash = flashTimer.C
			case "connecting":
				rgb = [3]byte{30, 15, 0}
			}
			apply(rgb)
		}
	}
}
func show(states chan string, state string) {
	select {
	case states <- state:
	default:
		select {
		case <-states:
		default:
		}
		select {
		case states <- state:
		default:
		}
	}
}

func keys(ctx context.Context, path string, hardwarePrivacy bool, out chan<- uint16, errs chan<- error) {
	f, err := os.Open(path)
	if err != nil {
		select {
		case errs <- fmt.Errorf("input %s: %w", path, err):
		case <-ctx.Done():
		}
		return
	}
	defer f.Close()
	stop := context.AfterFunc(ctx, func() { f.Close() })
	defer stop()
	// linux/arm input_event = 8-byte timeval, type u16, code u16, value i32.
	b := make([]byte, 16)
	for {
		if _, err = io.ReadFull(f, b); err != nil {
			if ctx.Err() == nil {
				select {
				case errs <- fmt.Errorf("input %s: %w", path, err):
				case <-ctx.Done():
				}
			}
			return
		}
		if binary.LittleEndian.Uint16(b[8:]) == 1 && binary.LittleEndian.Uint32(b[12:]) == 1 {
			key := binary.LittleEndian.Uint16(b[10:])
			if hardwarePrivacy && key == 113 {
				continue // Privacy watcher reports the actual level after the circuit changes.
			}
			select {
			case out <- key:
			case <-ctx.Done():
				return
			}
		}
	}
}

type player struct {
	queue       *audio.Queue
	mu          sync.Mutex
	activeUntil time.Time
}

func newPlayer() *player { return &player{queue: audio.NewQueue(48000 * 4 * 30)} }
func (p *player) busy() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.queue.Len() > 0 || time.Now().Before(p.activeUntil)
}
func (p *player) clear() { p.queue.Clear() }
func (p *player) run(ctx context.Context, d speakerPCM) error {
	defer d.Close()
	defer func() {
		if err := amp(false); err != nil {
			log.Printf("amp shutdown: %v", err)
		}
	}()
	// Enable the amp before starting DMA so its hardware transition cannot
	// underrun an already running stream. It stays enabled between replies.
	if err := amp(true); err != nil {
		return err
	}
	b := make([]byte, 1024*4)
	for ctx.Err() == nil {
		n, reset := p.queue.Take(b)
		if reset {
			// Clear software audio immediately, letting the short hardware/pipe
			// buffer drain without stopping clocks or cycling the amplifier.
			p.mu.Lock()
			p.activeUntil = time.Now().Add(300 * time.Millisecond)
			p.mu.Unlock()
		}
		if n > 0 {
			// Account for up to 128 ms of queued kernel/pipe audio, plus acoustic tail.
			p.mu.Lock()
			p.activeUntil = time.Now().Add(500 * time.Millisecond)
			p.mu.Unlock()
		}
		for off := 0; off < len(b); {
			written, err := d.Transfer(ctx, b[off:])
			if errors.Is(err, syscall.EPIPE) {
				log.Print("speaker underrun; preparing PCM again")
				if err = d.Reset(); err != nil {
					return err
				}
				// Drop this short period rather than replay a partly played word.
				break
			}
			if err != nil {
				return fmt.Errorf("speaker: %w", err)
			}
			off += written
		}
	}
	return ctx.Err()
}
func capture(ctx context.Context, d *audio.Device, gain float64, out chan<- []byte, diagnostics bool) error {
	defer d.Close()
	raw := make([]byte, 256*27)
	var pending []byte
	var captured, dropped int
	lastDiagnostic := time.Now()
	for ctx.Err() == nil {
		n, err := d.Transfer(ctx, raw)
		if errors.Is(err, syscall.EPIPE) {
			log.Print("microphone overrun; restarting capture")
			if err = d.Reset(); err != nil {
				return err
			}
			pending = nil
			continue
		}
		if err != nil {
			return fmt.Errorf("microphone: %w", err)
		}
		pcm, err := audio.Mono16(raw[:n], gain)
		if err != nil {
			return err
		}
		pending = append(pending, pcm...)
		captured += len(pcm)
		// 32 ms packets; a slow network cannot block capture or accumulate
		// seconds of stale microphone audio.
		for len(pending) >= 1024 {
			packet := append([]byte(nil), pending[:1024]...)
			select {
			case out <- packet:
			default:
				dropped += len(packet)
			}
			pending = pending[1024:]
		}
		if diagnostics && time.Since(lastDiagnostic) >= 5*time.Second {
			log.Printf("capture diagnostic: captured_bytes=%d dropped_bytes=%d (drops expected while muted/setup)", captured, dropped)
			captured, dropped = 0, 0
			lastDiagnostic = time.Now()
		}
	}
	return ctx.Err()
}
