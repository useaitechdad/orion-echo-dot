# Build and operate Orion

Orion's main client targets an already-rooted Echo Dot 2 with a 32-bit ARM
userspace. Obtain compatible dependencies, prepare your own wake model and keep
a recovery backup before adapting it to a device. This package does not flash
firmware or provide a complete binary runtime bundle.

## Everyday operation

1. With the button light off and ring dim blue, say **“Hey Orion.”**
2. Wait for the greeting/acknowledgement, then ask a question. Its exact wording can vary.
3. Follow-up questions work during the active conversation. After about 20 seconds without activity, Gemini disconnects and the ring returns to dim blue.
4. Action activates from waiting or interrupts playback during a conversation.
5. Tap Mute to switch hardware privacy on: button light on, ring red, cloud session closed. After two minutes the ring becomes dim red, still muted. Tap again for button light off and local waiting. Volume ± adjusts output.
6. Slow flashing red means attention is required. Action retries recovery; wait for dim blue and say the wake phrase if the worker was restarted. If the error was a connection failure, Action retries that connection directly. Hardware mute must be off to retry. Restart the device if recovery keeps failing.

The app reads hardware privacy at startup and during operation. App-only restart while muted is verified. Do not assume the hardware state survives a full power cycle. Do not inject KEY_MUTE to restore state; that caused the earlier button/ring inversion.

White means connected listening, green speaking and amber connecting/recovering. Error attention flashes red on/off at a two-second cycle; mute remains steady red and takes priority. Active conversation audio goes to Google. Playback remains half duplex; no full-duplex echo cancellation is claimed.

## Build and checks

The Go version is declared in `go.mod`. The production target is **linux/arm, GOARM=7**, matching the Echo's 32-bit userspace. Do not deploy arm64 binaries.

```sh
sh scripts/check.sh
sh scripts/build.sh
# Output: build/client/dottyd (client only)

# Historical PocketSphinx build, retained for reproduction/rollback:
sh scripts/build-wake.sh
# Outputs: build/wake/dottyd-wake, dotty-wake, and wake/ assets
```

For the installed microWakeWord helper, see [the native build notes](native/microwake/README.md). Its personalized weights and compatibility libraries are not included in the source package.

The historical PocketSphinx wake build uses `uv`, `curl`, `tar`, `sha256sum` (or `shasum`) and `make`; it pins PocketSphinx 5.1.1, checks its source archive hash, and uses CMake/Zig for static ARM compilation. `DOTTY_WAKE_BUILD_ROOT` selects its temporary build area; `DOTTY_OUTPUT_DIR` can select a separate output directory. Build scripts preserve old `bin/` artifacts and do not deploy.

The PocketSphinx regression `scripts/test-wake-idle.py` is **PocketSphinx-only**;
it hard-codes its likelihood threshold and is not recommended for unattended use.
The provided build creates an ARM executable, so run that regression in a compatible
Linux ARM environment with Python, or supply a separately host-built PocketSphinx
helper. Provide your own 16 kHz mono signed-16 WAV files by external paths: a
positive clip with exactly three phrases, unrelated speech, and a confusable phrase.
No prerecorded TTS fixtures are distributed. This historical detector can false-wake
on background sounds even when a saved-clip test passes.

For microWakeWord, use `scripts/test-microwake.py` with your model-specific threshold
and held-out recordings. See the native/training guides for complete commands.
Start with [installation and staged tests](SETUP.md) before device operation.

## Configuration

The startup template `scripts/dotty_init.sh` defaults to `/data/local/tmp/dotty/current`. Stage the client as `dottyd-wake`, the helper as `dotty-wake`, and the matching assets as `wake/`. For PocketSphinx, include its dictionary and `wake.kws`. For microWakeWord, include the matching `wake/model.tflite`, native helper, wrapper and isolated runtime libraries described in the native build notes. Privately configure:

| Setting | Meaning |
|---|---|
| `DOTTY_RELEASE` | Optional override for the staged device release directory |
| `DOTTY_WAKE_THRESHOLD` | Required model-specific threshold; no silent default. microWakeWord accepts 0.5 inclusive to 1 exclusive. Validate your own candidate. |
| `DOTTY_WIFI_NETWORK_ID` | Already-configured Wi-Fi network index; default 0 |
| `DOTTY_DNS` | Defaults to `auto`: refresh DHCP DNS/gateway per dial; explicit overrides must be host:port |
| `DOTTY_CONFIG` | Optional root-owned configuration path; defaults to `/data/local/tmp/dotty/runtime.conf` |
| `DOTTY_WIRELESS_ADB` | Defaults to 0 (USB only); 1 explicitly enables wireless debugging |
| `DOTTY_RESTART_ADB` | Defaults to 1; set to 0 only for a foreground USB launcher test (existing ADB listener is retained) |
| `DOTTY_ALLOW_PERMISSIVE` | Defaults to 0 (do not relax SELinux); 1 explicitly requests the tested development mode |
| `DOTTY_KEY_FILE` / `-key-file` | Private Gemini API key file; never embedded in source or video |
| `DOTTY_CA_FILE` / `-ca-file` | Trusted PEM CA bundle; TLS verification is mandatory |
| `DOTTY_MODEL` / `-model`, `-voice` | Required model selection in the launcher; optional voice |
| `-continuous-listening` | Explicit diagnostic opt-in; normal live mode requires local wake |
| `-supervise=false` | Diagnostic-only bypass of worker supervision; normal live mode is supervised |

The checked deployment used the model configured in `internal/live/client.go`; that records this project's tested setting, not a current universal availability claim. Verify vendor support before using it.

The startup template selects an existing Wi-Fi configuration; it does not
supply credentials. Keep the API key, Wi-Fi choices, wake model and runtime
configuration outside source control. The main application requires the native
helper, model, trusted CA bundle and compatible audio tools to be staged
separately. Do not run competing clients or capture diagnostics simultaneously.

The example selects a microWakeWord bundle and requires an explicit probability
threshold. Empty threshold/model settings fail before launcher service changes.
The Go CLI also requires an explicit wake threshold. PocketSphinx is retained only
for historical comparison; it is not recommended for unattended cloud activation.

Wireless ADB and permissive SELinux are explicit opt-ins. The demonstrated rooted
configuration used permissive SELinux; enforcing-mode compatibility is not
established. Changing a launcher or service is a separate device operation:
review its permissions, contexts, recovery path and read-only root state.

## Validation on your device

Check wake detection after idle, unrelated speech and realistic background noise;
verify mute blocks wake and Action, clears output and closes the cloud session.
Check volume, interrupted playback, delayed Wi-Fi readiness, failure indication,
and restart behavior. Distinguish replayed recordings from fresh live tests.
Do not infer a household error rate from a small development corpus.

`cmd/dotty_probe` reports levels without saving audio. `cmd/dotty_calibrate` makes
an explicitly requested local clip and checks hardware privacy before and during
capture. Obtain consent and keep its output outside this source folder. The audio
diagnostic scripts may print device details; review their output before sharing.

Both DAC output mixers and I²S must be enabled for the tested speaker route.
See [architecture](ARCHITECTURE.md) and [technical lessons](LESSONS_LEARNED.md).
`RELEASE_MANIFEST.json` lists this source package's files and hashes.
