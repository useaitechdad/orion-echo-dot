package main

import (
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"dotty/internal/audio"
	"dotty/internal/live"
	"dotty/internal/wake"
)

type options struct {
	wakeEnabled                                   bool
	supervised, continuous                        bool
	wifiNetwork                                   string
	wakeHelper, wakeModel                         string
	wakeThreshold                                 float64
	playback                                      string
	diagnostics, startupGreeting                  bool
	mode, keyFile, caFile, dns, model, voice, out string
	volume                                        int
	gain                                          float64
}

func main() {
	var o options
	flag.StringVar(&o.mode, "mode", "live", "live, speaker-test, loopback, or api-test (no hardware)")
	flag.StringVar(&o.keyFile, "key-file", "/data/local/tmp/dotty/api-key.txt", "file containing Gemini API key")
	flag.StringVar(&o.caFile, "ca-file", "", "PEM CA bundle; required on FireOS if system roots unavailable")
	flag.StringVar(&o.dns, "dns", "", "DNS server host:port (e.g. LAN router); empty uses OS resolver")
	flag.StringVar(&o.model, "model", live.DefaultModel, "Gemini Live model ID")
	flag.StringVar(&o.voice, "voice", "", "optional Gemini voice name; empty uses model default")
	flag.StringVar(&o.out, "out", "dotty-api-test.wav", "api-test output WAV path (must not already exist)")
	flag.IntVar(&o.volume, "volume", 5, "starting volume 0..10")
	flag.Float64Var(&o.gain, "mic-gain", 6, "fixed microphone gain (no peak normalization)")
	flag.BoolVar(&o.diagnostics, "diagnostics", false, "log audio byte counts and levels, never recordings or transcripts")
	flag.BoolVar(&o.startupGreeting, "startup-greeting", false, "request one spoken greeting after the first successful Live connection")
	flag.StringVar(&o.playback, "playback", "tinyplay", "speaker backend: tinyplay or direct (diagnostic)")
	flag.BoolVar(&o.wakeEnabled, "wake", true, "require local Hey Orion or Action before opening Gemini")
	flag.BoolVar(&o.continuous, "continuous-listening", false, "explicitly opt into continuous cloud microphone streaming (diagnostic)")
	flag.BoolVar(&o.supervised, "supervise", true, "restart failed live workers; flash error after repeated failures")
	flag.StringVar(&o.wifiNetwork, "wifi-network", "", "supervisor: configure an existing Wi-Fi network ID asynchronously")
	flag.StringVar(&o.wakeHelper, "wake-helper", "/data/local/tmp/dotty/dotty-wake", "local keyword helper executable")
	flag.StringVar(&o.wakeModel, "wake-model", "/data/local/tmp/dotty/wake", "local keyword model directory")
	flag.Float64Var(&o.wakeThreshold, "wake-threshold", 0, "required model-specific local keyword threshold")
	flag.Parse()
	log.SetFlags(log.Ldate | log.Ltime | log.LUTC | log.Lmicroseconds)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if o.mode == "live" && o.supervised {
		if err := normalizeOptions(&o); err != nil {
			log.Print(err)
			os.Exit(1)
		}
		executable, err := os.Executable()
		if err == nil {
			err = supervise(ctx, executable, append(os.Args[1:], "-supervise=false"), o.wifiNetwork)
		}
		if err != nil && ctx.Err() == nil {
			log.Printf("Supervisor stopped: %v", err)
			os.Exit(1)
		}
		return
	}
	if err := run(ctx, o); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("STOP: %v", err)
		os.Exit(1)
	}
}
func run(ctx context.Context, o options) error {
	if err := normalizeOptions(&o); err != nil {
		return err
	}
	if o.wakeEnabled && (o.mode != "live" || o.startupGreeting || !(o.wakeThreshold > 0 && o.wakeThreshold < 1)) {
		return fmt.Errorf("wake mode requires live mode, threshold >0 and <1, and no startup-greeting bypass")
	}
	if o.volume < 0 || o.volume > 10 || o.gain <= 0 || o.gain > 20 || math.IsNaN(o.gain) {
		return fmt.Errorf("volume must be 0..10 and mic-gain >0..20")
	}
	cfg := live.Config{Model: o.model, Voice: o.voice, CAFile: o.caFile, DNS: o.dns}
	if o.mode == "live" || o.mode == "api-test" {
		b, err := os.ReadFile(o.keyFile)
		if err != nil {
			return fmt.Errorf("read key file: %w", err)
		}
		cfg.Key, err = live.ParseKey(b)
		if err != nil {
			return err
		}
	}
	if o.mode == "api-test" {
		if err := apiTest(ctx, cfg, o.out); err != nil {
			return errors.New(strings.ReplaceAll(err.Error(), cfg.Key, "[redacted]"))
		}
		return nil
	}
	if o.mode != "live" && o.mode != "speaker-test" && o.mode != "loopback" {
		return fmt.Errorf("unknown mode %q", o.mode)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "arm" {
		return fmt.Errorf("hardware modes must run on the Echo's linux/arm userspace")
	}
	lock, err := os.OpenFile("/data/local/tmp/dotty.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open singleton lock: %w", err)
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("another Dotty process holds the audio lock: %w", err)
	}
	defer func() {
		if err := amp(false); err != nil {
			log.Printf("final amp shutdown: %v", err)
		}
	}()
	if err = initHardware(); err != nil {
		return err
	}
	if err = setVolume(o.volume); err != nil {
		return err
	}
	var speaker speakerPCM
	switch o.playback {
	case "tinyplay":
		speaker, err = openTinyPlayer()
	case "direct":
		speaker, err = audio.OpenSpeaker()
	default:
		return fmt.Errorf("unknown playback backend %q", o.playback)
	}
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	errs := make(chan error, 8)
	start := func(fn func() error) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			err := fn()
			if err != nil && ctx.Err() == nil {
				select {
				case errs <- err:
				case <-ctx.Done():
				}
			}
		}()
	}
	var micData chan []byte
	if o.mode != "speaker-test" {
		if err = initMicrophones(); err != nil {
			speaker.Close()
			return err
		}
		mic, err := audio.OpenCapture()
		if err != nil {
			speaker.Close()
			return err
		}
		capacity := 2
		if o.wakeEnabled {
			capacity = 64
		} // Bounded local buffer; setup/acknowledgement audio is deliberately discarded.
		micData = make(chan []byte, capacity)
		start(func() error { return capture(ctx, mic, o.gain, micData, o.diagnostics) })
	}
	p := newPlayer()
	start(func() error { return p.run(ctx, speaker) })
	states := make(chan string, 1)
	start(func() error { ledWorker(ctx, states); return nil })
	show(states, "connecting")
	if o.mode == "speaker-test" {
		// -20 dBFS, 440 Hz, faded, dual-mono; no cloud or microphone needed.
		tone := make([]byte, 48000*4)
		for i := 0; i < 48000; i++ {
			envelope := math.Min(1, math.Min(float64(i)/480, float64(47999-i)/480))
			v := int16(3276 * envelope * math.Sin(2*math.Pi*440*float64(i)/48000))
			binary.LittleEndian.PutUint16(tone[i*4:], uint16(v))
			binary.LittleEndian.PutUint16(tone[i*4+2:], uint16(v))
		}
		if err = p.queue.Add(tone); err != nil {
			return err
		}
		show(states, "speaking")
		select {
		case err = <-errs:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
		log.Print("speaker test data submitted; human audible confirmation is required")
		return nil
	}
	events := make(chan uint16, 32)
	start(func() error { keys(ctx, "/dev/input/event1", o.mode == "live", events, errs); return nil })
	start(func() error { keys(ctx, "/dev/input/event2", o.mode == "live", events, errs); return nil })
	if o.mode == "loopback" {
		return loopback(ctx, p, events, micData, errs, states, o.volume)
	}
	initialMute, err := readPrivacyState()
	if err != nil {
		return fmt.Errorf("%w: %v", errHardware, err)
	}
	log.Printf("Initial hardware privacy: muted=%t", initialMute)
	start(func() error { return watchPrivacy(ctx, readPrivacyState, initialMute, events) })
	var detector *wake.Detector
	if o.wakeEnabled {
		if o.startupGreeting {
			return fmt.Errorf("startup-greeting cannot bypass local wake mode")
		}
		detector, err = wake.Open(ctx, o.wakeHelper, o.wakeModel, o.wakeThreshold)
		if err != nil {
			return fmt.Errorf("wake mode unavailable (upload remains off): %w", err)
		}
		defer detector.Close()
		log.Print("Live mode: local wake required; no cloud session while waiting")
	} else {
		log.Print("Live mode: continuous server VAD; Action interrupts; Mute disables upstream and output; volume +/-")
	}
	state := sessionState{volume: o.volume, diagnostics: o.diagnostics, greeting: o.startupGreeting, wakeEnabled: o.wakeEnabled, muted: initialMute}
	retry := time.Second
	for ctx.Err() == nil {
		// Mute stays local and sticky across reconnects.
		if state.muted {
			show(states, "muted")
			select {
			case <-ctx.Done():
				return ctx.Err()
			case err := <-errs:
				return err
			case <-time.After(100 * time.Millisecond):
				// A direct pre-dial read may observe a short transition the
				// change-only watcher missed. Reconcile the current level.
				muted, err := readPrivacyState()
				if err != nil {
					return fmt.Errorf("%w: %v", errHardware, err)
				}
				state.muted = muted
			case key := <-events:
				if key == privacyUnmuted {
					state.muted = false
					log.Print("Unmuted by hardware privacy state")
				}
				if key == 114 || key == 115 {
					if err := state.adjustVolume(key); err != nil {
						return err
					}
				}
			}
			continue
		}
		if state.wakeEnabled && !state.awake {
			if err := waitForWake(ctx, detector, p, &state, events, micData, errs, states); err != nil {
				return err
			}
			if !state.awake {
				continue
			}
		}
		// Recheck the circuit before opening a cloud connection, including
		// a state change that occurred during wake/helper setup.
		muted, err := readPrivacyState()
		if err != nil {
			return fmt.Errorf("%w: %v", errHardware, err)
		}
		if muted {
			state.muted, state.awake = true, false
			continue
		}
		show(states, "connecting")
		cfg.ResumeHandle = state.resume
		conn, err := connectWithRecovery(ctx, cfg, &state, events, errs, states)
		if err == nil && conn != nil {
			log.Printf("Gemini setupComplete model=%s", cfg.Model)
			retry = time.Second
			err = serve(ctx, conn, p, &state, events, micData, errs, states)
			conn.Close()
		} else if state.resume != "" {
			// A rejected/expired resume handle should not trap reconnect forever.
			state.resume = ""
		}
		p.clear()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, errHardware) {
			return err
		}
		if state.wakeEnabled {
			state.awake = false
			state.firstWake = false
			state.resume = ""
			if err != nil && !errors.Is(err, errSleep) {
				log.Printf("wake conversation ended: %s", strings.ReplaceAll(err.Error(), cfg.Key, "[redacted]"))
				state.attention = true
				log.Print("Conversation unavailable; flashing red; say wake phrase or press Action to retry")
			}
			continue
		}
		if state.muted {
			continue
		}
		if err != nil {
			log.Printf("session ended: %s; retry in %s", strings.ReplaceAll(err.Error(), cfg.Key, "[redacted]"), retry)
		}
		show(states, "error")
		timer := time.NewTimer(retry)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case err := <-errs:
			timer.Stop()
			return err
		case key := <-events:
			timer.Stop()
			if key == privacyMuted {
				state.muted = true
			}
			if key == 114 || key == 115 {
				if err = state.adjustVolume(key); err != nil {
					return err
				}
			}
		case <-timer.C:
		}
		retry = min(retry*2, 30*time.Second)
	}
	return ctx.Err()
}

var errHardware = errors.New("hardware failure")
var errSleep = errors.New("conversation finished; returning to local wake")

type sessionState struct {
	wakeEnabled, awake, firstWake bool
	diagnostics, greeting         bool
	muted                         bool
	attention                     bool
	volume                        int
	resume                        string
}

func (s *sessionState) adjustVolume(key uint16) error {
	old := s.volume
	if key == 114 {
		s.volume = max(0, s.volume-1)
	} else {
		s.volume = min(10, s.volume+1)
	}
	if err := setVolume(s.volume); err != nil {
		s.volume = old
		log.Printf("Volume change failed; retaining previous level: %v", err)
	}
	return nil
}

type received struct {
	message live.Message
	err     error
}

type liveSession interface {
	Receive() (live.Message, error)
	Send(any) error
	Audio([]byte) error
	StreamEnd() error
	Ping() error
	Close() error
}

func serve(ctx context.Context, c liveSession, p *player, s *sessionState, keys <-chan uint16, mic <-chan []byte, errs <-chan error, states chan string) error {
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(ctx, func() { c.Close() })
	receiveDone := make(chan struct{})
	defer func() { cancel(); c.Close(); <-receiveDone; stop() }()
	incoming := make(chan received, 2)
	go func() {
		defer close(receiveDone)
		for {
			m, err := c.Receive()
			select {
			case incoming <- received{m, err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	progressTick := time.NewTicker(50 * time.Millisecond)
	defer progressTick.Stop()
	started, lastOutput := time.Now(), time.Now()
	var pendingOutput []byte
	var up audio.Upsampler
	window := newConversationWindow(time.Now())
	gate := gateState{}
	if s.greeting {
		if err := c.Send(map[string]any{"realtimeInput": map[string]any{"text": "Say exactly: Hello, I am Orion. My live speaker test is working."}}); err != nil {
			return err
		}
		s.greeting = false
	}
	var inputBytes, sentBytes, outputBytes, turnBytes int
	var peak int
	var squares float64
	lastDiagnostic := time.Now()
	// Discard anything captured during setup/previous playback.
	for len(mic) > 0 {
		<-mic
	}
	s.firstWake = false
	acknowledgingWake := s.wakeEnabled
	if s.wakeEnabled {
		// Only reached after local activation and setupComplete. Use the
		// accepted speech path so readiness is unmistakable. Withhold the
		// microphone until acknowledgement playback and its tail finish.
		if err := c.Send(map[string]any{"realtimeInput": map[string]any{"text": "The user has just activated you with the wake phrase. Acknowledge by saying exactly one word: Yes? Then wait for their question."}}); err != nil {
			return err
		}
		gate.serverSpeaking = true
		show(states, "connecting")
		log.Print("Spoken wake acknowledgement requested; ask your question after it finishes")
	} else {
		show(states, "listening")
	}
	for {
		// Backpressure the receive loop while draining one bounded message.
		// Keep controls and timers live; never block mute behind speaker space.
		if n := min(len(pendingOutput), p.queue.Free()/4*4); n > 0 {
			if err := p.queue.Add(pendingOutput[:n]); err != nil {
				return err
			}
			pendingOutput = pendingOutput[n:]
		}
		var receive <-chan received = incoming
		if len(pendingOutput) > 0 {
			receive = nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errs:
			return fmt.Errorf("%w: %v", errHardware, err)
		case <-tick.C:
			if err := c.Ping(); err != nil {
				return err
			}
		case <-progressTick.C:
			now := time.Now()
			busy := p.busy() || len(pendingOutput) > 0
			if acknowledgingWake && now.Sub(started) >= 15*time.Second {
				return fmt.Errorf("wake acknowledgement timed out")
			}
			if !acknowledgingWake && gate.serverSpeaking && !busy && now.Sub(lastOutput) >= 10*time.Second {
				return fmt.Errorf("response stalled without completion")
			}
			if gate.suppressOutput && now.Sub(lastOutput) >= 10*time.Second {
				return fmt.Errorf("interrupted response did not finish")
			}
			if s.wakeEnabled {
				window.observe(now, busy, nil)
				if window.expired(now) {
					log.Print("Conversation inactive or time limit reached; closing cloud session")
					return errSleep
				}
			}
		case key := <-keys:
			switch key {
			case privacyMuted:
				s.muted = true
				p.clear()
				show(states, "muted")
				log.Print("Muted by hardware privacy state; closing cloud session")
				// Closing discards in-flight generation and prevents new audio
				// reaching Gemini until the user explicitly unmutes.
				return nil
			case 114, 115:
				if err := s.adjustVolume(key); err != nil {
					return fmt.Errorf("%w: %v", errHardware, err)
				}
			case 138:
				p.clear()
				pendingOutput = nil
				acknowledgingWake = false
				lastOutput = time.Now()
				up = audio.Upsampler{}
				gate.interrupt()
				show(states, "listening")
				log.Print("Action: cleared output; microphone resumes after acoustic tail")
			}
		case pcm := <-mic:
			if s.wakeEnabled {
				busy := p.busy() || len(pendingOutput) > 0
				window.observe(time.Now(), busy, nil)
				if window.expired(time.Now()) {
					log.Print("Conversation inactive or time limit reached; closing cloud session")
					return errSleep
				}
				if !busy {
					window.observe(time.Now(), false, pcm)
				}
			}
			if s.diagnostics {
				inputBytes += len(pcm)
				for i := 0; i+1 < len(pcm); i += 2 {
					v := int(int16(binary.LittleEndian.Uint16(pcm[i:])))
					squares += float64(v) * float64(v)
					peak = max(peak, int(math.Abs(float64(v))))
				}
				if time.Since(lastDiagnostic) >= 5*time.Second {
					log.Printf("audio diagnostic: mic_bytes=%d sent_bytes=%d mic_peak=%d mic_rms=%.1f output_bytes=%d queue_bytes=%d speaking=%t suppressed=%t", inputBytes, sentBytes, peak, math.Sqrt(squares/float64(inputBytes/2)), outputBytes, p.queue.Len(), gate.serverSpeaking, gate.suppressOutput)
					inputBytes, sentBytes, outputBytes, peak = 0, 0, 0, 0
					squares = 0
					lastDiagnostic = time.Now()
				}
			}
			allowed := gate.allowMic(p.busy() || len(pendingOutput) > 0)
			if allowed {
				if err := c.Audio(pcm); err != nil {
					return err
				}
				sentBytes += len(pcm)
				gate.streamOpen = true
				show(states, "listening")
			} else if gate.streamOpen {
				if err := c.StreamEnd(); err != nil {
					return err
				}
				gate.streamOpen = false
			}
		case r := <-receive:
			if r.err != nil {
				return r.err
			}
			m := r.message
			if u := m.SessionResumptionUpdate; u != nil && u.Resumable {
				s.resume = u.NewHandle
			}
			if m.GoAway != nil {
				return fmt.Errorf("server requested reconnect (timeLeft=%s)", m.GoAway.TimeLeft)
			}
			content := m.ServerContent
			if content == nil {
				continue
			}
			if content.Interrupted {
				p.clear()
				pendingOutput = nil
				up = audio.Upsampler{}
				gate.boundary()
				acknowledgingWake = false
				log.Print("Gemini interrupted: output discarded")
			}
			if content.ModelTurn != nil && !gate.suppressOutput {
				for _, part := range content.ModelTurn.Parts {
					if part.InlineData == nil {
						continue
					}
					raw, err := part.InlineData.PCM()
					if err != nil {
						return err
					}
					outputBytes += len(raw)
					turnBytes += len(raw)
					pcm, err := up.Convert(raw)
					if err != nil {
						return err
					}
					gate.serverSpeaking = true
					lastOutput = time.Now()
					if len(pendingOutput)+len(pcm) > 16<<20 {
						return fmt.Errorf("single response message exceeds bounded output staging")
					}
					pendingOutput = append(pendingOutput, pcm...)
					show(states, "speaking")
				}
			}
			if content.TurnComplete {
				window.lastActivity = time.Now()
				gate.boundary()
				up = audio.Upsampler{}
				if acknowledgingWake {
					log.Printf("Wake acknowledgement complete: %d audio bytes (%.2fs); waiting for speaker drain", turnBytes, float64(turnBytes)/48000)
					acknowledgingWake = false
				} else {
					log.Printf("Gemini turn complete: %d audio bytes (%.2fs); waiting for speaker drain", turnBytes, float64(turnBytes)/48000)
				}
				turnBytes = 0
			}
		}
	}
}

func loopback(ctx context.Context, p *player, keys <-chan uint16, mic <-chan []byte, errs <-chan error, states chan string, volume int) error {
	log.Print("Loopback ready: Action records 3 seconds; Mute cancels; no network")
	var recording []byte
	active := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errs:
			return err
		case key := <-keys:
			switch key {
			case 138:
				p.clear()
				recording = nil
				active = true
				show(states, "listening")
			case 113:
				p.clear()
				recording = nil
				active = false
				show(states, "muted")
			case 114, 115:
				if key == 114 {
					volume = max(0, volume-1)
				} else {
					volume = min(10, volume+1)
				}
				if err := setVolume(volume); err != nil {
					return err
				}
			}
		case pcm := <-mic:
			if !active {
				continue
			}
			recording = append(recording, pcm...)
			if len(recording) >= 16000*2*3 {
				active = false
				out := make([]byte, 4800*4, 4800*4+len(recording)*6) // 100 ms lead-in
				for i := 0; i < len(recording); i += 2 {
					for j := 0; j < 3; j++ {
						out = append(out, recording[i], recording[i+1], recording[i], recording[i+1])
					}
				}
				if err := p.queue.Add(out); err != nil {
					return err
				}
				show(states, "speaking")
				log.Printf("loopback queued %d microphone bytes; audible confirmation required", len(recording))
				recording = nil
			}
		}
	}
}

func apiTest(ctx context.Context, cfg live.Config, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	dns, err := refreshDNS(ctx, cfg.DNS)
	if err != nil {
		return err
	}
	cfg.DNS = dns
	c, err := live.Dial(ctx, cfg)
	if err != nil {
		return err
	}
	defer c.Close()
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	if err = c.Send(map[string]any{"realtimeInput": map[string]any{"text": "Say exactly: Hello, I am Orion. My voice connection is working."}}); err != nil {
		return err
	}
	var pcm []byte
	for {
		m, err := c.Receive()
		if err != nil {
			return err
		}
		if m.GoAway != nil {
			return fmt.Errorf("server requested reconnect during API test")
		}
		if m.ServerContent == nil {
			continue
		}
		if turn := m.ServerContent.ModelTurn; turn != nil {
			for _, part := range turn.Parts {
				if part.InlineData != nil {
					p, err := part.InlineData.PCM()
					if err != nil {
						return err
					}
					pcm = append(pcm, p...)
					if len(pcm) > 24000*2*30 {
						return fmt.Errorf("test response exceeded 30 seconds")
					}
				}
			}
		}
		if m.ServerContent.TurnComplete {
			break
		}
	}
	if len(pcm) == 0 {
		return fmt.Errorf("Gemini completed without audio")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	hdr := make([]byte, 44)
	copy(hdr, "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:], uint32(len(pcm)+36))
	copy(hdr[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(hdr[16:], 16)
	binary.LittleEndian.PutUint16(hdr[20:], 1)
	binary.LittleEndian.PutUint16(hdr[22:], 1)
	binary.LittleEndian.PutUint32(hdr[24:], 24000)
	binary.LittleEndian.PutUint32(hdr[28:], 48000)
	binary.LittleEndian.PutUint16(hdr[32:], 2)
	binary.LittleEndian.PutUint16(hdr[34:], 16)
	copy(hdr[36:], "data")
	binary.LittleEndian.PutUint32(hdr[40:], uint32(len(pcm)))
	if _, err = f.Write(hdr); err != nil {
		return err
	}
	if _, err = f.Write(pcm); err != nil {
		return err
	}
	log.Printf("Gemini API verified: %d PCM bytes, %.2fs at 24kHz; saved %s", len(pcm), float64(len(pcm))/48000, path)
	return nil
}
