# Orion architecture

Orion has a Go client and a separate native wake detector on the Echo Dot. The
client owns microphone capture, playback, buttons, indicators and the Gemini
connection. The helper runs the local phrase model. Training and adaptation
happen separately; personal model weights and recordings are not in this package.

## Runtime path

```text
Echo microphone array
  → nine-channel ALSA capture
  → average microphone channels 0–6, fixed gain → 16 kHz mono PCM
  → local wake helper while waiting
  → Gemini Live after wake or Action
  → 24 kHz mono response → 48 kHz stereo playback
```

Channels 7–8 are reference channels and are excluded from the microphone average.
This averaging is not calibrated directional beamforming. The local helper has
no network connection. During conversations, the client withholds microphone
upload during generated speech, queued playback and the acoustic tail; full-duplex
echo cancellation is not implemented.

## Components

| Component | Responsibility |
|---|---|
| `cmd/dottyd` | Supervisor, isolated voice worker, controls, status and session lifecycle |
| `internal/live` | Gemini WebSocket messages, TLS, key parsing and transport |
| `internal/audio` | Device-specific ALSA ABI, conversion, queues and LED driver |
| `internal/wake` | Framed pipe protocol and helper lifecycle |
| `internal/privacy` | Shared fail-closed hardware privacy check |
| `native/microwake` | Microfrontend, streaming inference, reset and runtime wrapper |
| `training/microwakeword` | Public-data training and consented local adaptation tools |
| `native/wake`, `assets/wake` | Optional legacy PocketSphinx implementation |
| `scripts/dotty_init.sh` | Startup template for an already-configured device |

## Tested hardware interfaces

| Interface | Configuration |
|---|---|
| Capture | Card 0/device 24; 16 kHz, 9 channels, packed S24_3LE; 256 frames × 4 periods |
| Playback | Card 0/device 23; 48 kHz stereo S16_LE; 1024 frames × 4 periods |
| Conversion | Average channels 0–6; fixed microphone gain 6 |
| Speaker routes | Both DAC output mixers on, I²S on, DacMux off, HP DAC on/on |
| Ring | `/sys/bus/i2c/devices/0-003f/frame`, twelve RGB pixels |
| Privacy | Read-only `/sys/devices/soc/10010000.keypad/amz_privacy/privacy_state` |
| Buttons | Action 138/Mute 113 on event1; volume 114/115 on event2 |

The client keeps a continuous `tinyplay` stream, writing silence between replies,
and disables the amplifier on shutdown. These interfaces are device/firmware
specific. Use the running system's ABI, not CPU capability alone, to choose builds.

## Wake, privacy and controls

The microWakeWord helper averages three model probabilities, applies hysteresis,
and resets frontend and model state when returning to waiting. Configure its
threshold together with the matching model. The legacy PocketSphinx threshold
has a different meaning and cannot be reused as a probability.

Hardware privacy is read before the session loop, polled every 100 ms and checked
again before dialing. Invalid or unreadable state stops the client. Raw mute-key
events are not used as an independent software toggle. Do not read the neighboring
legacy `amz_privacy/state` or `power_button_state` attributes: inspection of them
caused a kernel panic on the tested firmware.

Waiting is dim blue, readiness/connection/recovery amber, listening white,
speaking green, and mute steady red. Mute dims after two minutes. Attention is
red for one second, off for one second. Mute overrides attention; Action cannot
override mute. Both Amazon animation owners must stop and the kernel boot
animation must be disabled before the client controls the ring.

## Session and recovery

After local activation, network readiness can wait up to 60 seconds without
microphone upload. Mute cancels this wait. Setup has bounded retries; failures
reach the attention state. A fresh connection requests a spoken acknowledgement
before question audio is uploaded. Inactivity closes the conversation after
about 20 seconds; the maximum conversation length is five minutes. An energy
baseline helps bound sustained-noise activity but is not a general speech VAD.

The supervisor restarts failed voice workers and reports attention after three
rapid failures. Five minutes of worker uptime resets that count. The supervisor
owns indicators while a worker is down. Worker/player process cleanup and
playback backpressure keep mute and Action responsive. The supervisor itself
still depends on the service/device lifecycle.

## Configuration and secrets

API keys are read from a private file and sent in an authentication header. TLS
verification remains enabled; the target FireOS environment needs a trusted CA
bundle. Keep keys, network configuration, personal audio and weights outside the
source tree. Default diagnostics omit transcripts and recordings; diagnostic
output still needs review before sharing. See [operation and configuration](PHASE1.md).
