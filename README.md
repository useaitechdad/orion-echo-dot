# Orion — voice on an Echo Dot

Companion video: [I Upgraded an Old Echo Dot with Gemini Live](https://www.youtube.com/watch?v=F1QI-WroUT8).

Orion runs on a rooted Echo Dot 2 and uses Gemini Live for spoken conversations.
A small microWakeWord model detects “Hey Orion” locally. Gemini is disconnected
while waiting; after wake detection or Action, the client opens the conversation.
The Mac is used for development and model preparation, not as the runtime host.
The source module and executable names retain `dotty`.

This is a source release for an already-rooted compatible device. It includes
client and wake-helper source, build scripts, tests and training tools. It does
not include firmware, a rooting procedure, compiled runtime libraries, API keys,
private recordings or personalized model weights. Building the client alone is
not a complete installation.

## Use and indicators

Say “Hey Orion,” wait for the spoken acknowledgement, then ask a question.
Follow-up questions work during the active conversation. After about 20 seconds
without activity, the client disconnects and returns to local wake detection.

| Control or indicator | Meaning |
|---|---|
| Action | Activate from waiting; interrupt playback during a conversation; retry attention state |
| Volume ± | Adjust speaker volume |
| Mute button light on | Hardware privacy on; output stops and the cloud session closes |
| Dim blue ring | Waiting for the local wake phrase |
| Amber ring | Network readiness, connection setup or recovery |
| White ring | Listening during an active conversation |
| Green ring | Speaking |
| Steady red ring | Muted; dims after two minutes |
| Slow flashing red | Attention required; Action retries when unmuted |

Hardware mute overrides Action and wake detection. An unreadable privacy state
stops the client. Active-conversation audio goes to Google. Playback is half
duplex: microphone upload pauses during replies and their acoustic tail.

## Source map

| Folder | Purpose |
|---|---|
| `cmd/dottyd` | Main application, supervisor, controls and audio lifecycle |
| `cmd/dotty_probe`, `cmd/dotty_calibrate` | Channel diagnostics and explicitly requested local calibration |
| `internal` | Shared audio, Gemini transport, wake-helper and privacy code |
| `native/microwake` | Current C++ microWakeWord helper |
| `training/microwakeword` | Training, local adaptation and evaluation source |
| `native/wake`, `assets/wake` | Optional legacy PocketSphinx implementation and public test assets |
| `scripts` | Build, check, startup and audio diagnostic helpers |

## Documentation

- [Start here: installation and first tests](SETUP.md)
- [Build, configuration and operation](PHASE1.md)
- [Architecture](ARCHITECTURE.md)
- [Technical lessons](LESSONS_LEARNED.md)
- [Native wake helper](native/microwake/README.md)
- [Training tools](training/microwakeword/README.md)

## Limits

The hardware settings describe the tested FireOS/32-bit environment, not every
Echo revision. Basic conversations and controls have been demonstrated; this is
not a measured household false-wake guarantee or validation for every voice.
Software reboot behavior does not establish hardware privacy persistence across
all physical power cycles. Full-duplex echo cancellation is not implemented.
Long-running audio quality, noise robustness and recovery require validation on
each installation. Original code is [MIT licensed](LICENSE), allowing reuse and
modification with the copyright and license notice retained. Dependencies keep
their own terms; see [third-party notices](THIRD_PARTY_NOTICES.md).
