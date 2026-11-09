# Orion technical lessons

These notes describe reusable findings from the Echo Dot 2 implementation.
Use [architecture](ARCHITECTURE.md) and [configuration](PHASE1.md) for the current
code. Hardware findings are specific to the tested FireOS/32-bit environment.

## Identify the running system

CPU capability and userspace architecture are different facts. Build for the
running ABI: the client targets Linux ARM with `GOARM=7`, not arm64. Model names
and firmware identifiers help identify the device, but direct observations of
formats, routes and runtime libraries determine compatibility.

## A valid audio stream can still be silent

The tested speaker is card 0/device 23, not the desktop-style default device 0.
Microphone capture is card 0/device 24. Competing audio services may hold those
devices exclusively; check ownership before running another client or diagnostic.

The working playback route requires I²S On, DacMux Off, both DAC output mixers
On and HP DAC playback On/On. A saved mixer snapshot is not an initialization
recipe: explicitly writing a historical Off value disconnected digital playback.
Check the route and audible result rather than assuming a successful write means
sound reached the speaker.

API setup, nonzero returned PCM, successful playback writes and audible speech
are separate milestones. Verify each boundary. The current client keeps a
continuous playback stream with silence between replies; older per-clip amplifier
gating is not the current policy. Do not promise zero hiss or clicks without
listening and measurement on the actual device.

## Match the capture format

The capture interface expects nine channels at 16 kHz using packed signed
24-bit little-endian samples (`S24_3LE`). Its working period is 256 frames with
four periods. A capture tool that requests a different ALSA format can fail even
when its channel count and sample rate look correct.

Average channels 0–6 for microphone audio; exclude reference channels 7–8.
Apply the intended fixed gain, then convert to the mono 16 kHz format used by the
wake helper. This is simple averaging, not measured directional beamforming.
Gemini's 24 kHz mono response is converted to 48 kHz stereo for playback.

## Hardware privacy must be authoritative

A mute-key event does not establish the hardware microphone state. Read the
verified `privacy_state` attribute at startup and during operation. An unreadable
state must stop the client. Mute clears queued output and closes Gemini; Action
must never bypass it. Synthetic key events must not restore a guessed mute state.

Even read-only device inspection can fail dangerously: the neighboring legacy
`state` and `power_button_state` attributes triggered a kernel panic on the tested
firmware. Use only the verified privacy interface.

## Give the ring one owner

Stopping one animation service was insufficient: a separate boot-animation owner
and kernel animation could overwrite the application's colors. Stop both owners,
disable boot animation and use the driver's frame interface. Validate visible
colors on hardware; distinct RGB values may still look alike to the viewer.
Cancel mute-dimming timers when state changes so they cannot overwrite a new
listening, recovery or attention state.

## Wake recognition needs realistic evaluation

A passing synthetic phrase does not establish live recognition. The earlier
PocketSphinx approach could pass saved clips yet falsely activate on household
sounds. Synthetic-only microWakeWord candidates also failed to generalize to
real speech. Local adaptation can help, but keep a separate recording out of
training and use consented recordings only.

Test unrelated speech, similar-sounding phrases, transient noise, long idle
periods, repeated resets and the actual ARM helper. Reset frontend statistics
as well as model state. Threshold selection must balance recall and false
activation; a threshold tuned against a corpus is not independent validation on
that corpus. Report the observation and its duration, not a universal error rate.

Keep personal recordings, derived features and trained personal weights private.
Public training code does not make those artifacts public. Hard-negative mining
must use the training split, not held-out test data.

## Keep training and inference compatible

Use the same 10 ms microfrontend timing and feature layout in training and the
native runtime. Do not substitute legacy 20 ms feature archives. Quantization
calibration windows must divide into complete model strides. A successful export
is distinct from runtime compatibility and model quality.

The tested ARM helper needed PIE and an explicitly invoked isolated loader with
compatible runtime libraries. Do not replace FireOS system libraries to satisfy
a build. Compare host/device behavior with a public reference before evaluating
your own phrase model. Public dependency pins are in
`training/microwakeword/dependencies.json`.

## Separate activation from conversation

Recognizing the phrase opens Gemini; it does not mean the user has heard an
acknowledgement. Hold microphone upload until setup, the spoken cue and its
playback tail complete. Half-duplex gating prevents the assistant from treating
its own reply as a new question. Distinguish a new wake attempt from speech
inside an already-open conversation when evaluating recognition.

## Bound startup and failure paths

Wi-Fi readiness can lag application startup. Wait for readiness before spending
connection retries, show amber and keep privacy handling responsive. Persist the
helper/model selection and its matching threshold together. Confirm what actually
starts after reboot rather than assuming an interactive trial changed startup.

Test worker crashes, player failure, stalled setup, shutdown, mute during recovery
and Action retries. A successful software reboot does not prove repeated physical
cold boots or hardware-privacy persistence through loss of power. Keep a recovery
backup privately before changing device startup.
