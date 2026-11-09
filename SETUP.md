# Set up your own Orion

This guide starts with an **already-rooted, recoverable, compatible Echo Dot 2**.
It does not supply firmware, a rooting exploit or boot persistence. The tested
platform is MT8163, Cortex-A53 hardware with **32-bit ARM userspace**. Verify your
own unit: the case label alone can disagree with firmware-reported identity.
Use a USB **data** cable: a power-only cable cannot carry ADB commands, hardware
queries or file transfers. A computer and coding agent can assist with the build;
the finished client and wake detector run on the Dot itself.

Read the entire guide before running device commands. Commands below assume an
already-authorized root ADB shell, one selected device, and a recovery method you
have verified. Stop if your device paths or firmware differ. Host builds do not
operate the device. Staging and audio tests below do change the selected device.

## 1. Build on the computer

Install the Go version declared in `go.mod`, Python 3.11, Git and Android platform
tools (`adb`). Native builds also need the compiler/dependencies described in
[native build notes](native/microwake/README.md). macOS or Linux are build hosts;
GPU training uses a separate Linux environment. Linked steps also use `uv`,
`curl`, `tar`, `file`, `readelf` (Linux binutils), and ffmpeg or an audio editor.
Use Docker/Podman or a separate Debian 12 machine for the package extraction step.

```sh
sh scripts/check.sh
sh scripts/build.sh
```

The outputs are `build/client/dottyd`, `dotty-probe` and `dotty-calibrate`, all
linux/arm GOARM=7. Build the native helper and runtime with the native guide,
and train/adapt your own wake model with the
[training walkthrough](training/microwakeword/README.md). There is no downloadable
personal model in this package. Build output alone is not a ready installation.

## 2. Set up Gemini through the GCP console

This client calls the **Gemini Developer API / Generative Language API**, not a
Vertex AI endpoint. Creating the key in the Google Cloud console does not change
which API the code uses.

1. Select or create your project in [Google Cloud console](https://console.cloud.google.com/).
2. Under **APIs & Services → Library**, enable **Generative Language API**
   (`generativelanguage.googleapis.com`). Review project eligibility, billing,
   quota and current Live model availability.
3. Open **APIs & Services → Credentials → Create credentials → API key**.
   Follow the console's current service-account binding requirements for a
   Gemini authorization key. Google is transitioning key types; do not assume
   a newly created unrestricted standard key will work.
4. Restrict the key to the API and request origins supported by your setup.
   A server IP restriction uses your public egress IP, not the Dot's LAN address;
   browser-referrer restrictions are unsuitable for this native client.
   Keep the key in a private local text file, never a command argument or commit.
5. Set `DOTTY_MODEL` to a currently supported **Live audio** model for this
   project. The source's historical default is not a guarantee of availability.
   Run the `api-test` below before allowing microphone conversations.

Follow Google's current [Cloud key creation guide](https://cloud.google.com/docs/authentication/api-keys#create),
[Gemini key requirements](https://ai.google.dev/gemini-api/docs/api-key),
[Live guide](https://ai.google.dev/gemini-api/docs/live) and
[terms](https://ai.google.dev/gemini-api/terms). Active conversation audio goes to
Google and may incur usage charges. Local waiting does not open Gemini. This
project is for a personally controlled device; do not embed its long-lived key
in a publicly distributed app or image.

## 3. Prepare a private release directory

On the host, use a new `build/release` directory. Copy `build/client/dottyd` to
`build/release/dottyd-wake` (the launcher's expected name), then add the native
bundle and your validated model:

```text
build/release/
  dottyd-wake
  dotty-wake               # wrapper from native/microwake/
  dotty-microwake          # ARM PIE helper
  lib/libtensorflowlite_c.so
  libc/ld-linux-armhf.so.3
  libc/libc.so.6
  libc/libm.so.6
  libc/libpthread.so.0
  libc/libgcc_s.so.1
  libc/libstdc++.so.6
  wake/model.tflite        # your private model
```

Keep your key, model and `runtime.conf` out of Git. Copy
`scripts/dotty-config.example` to a private directory and fill in both
`DOTTY_MODEL` and `DOTTY_WAKE_THRESHOLD`. The latter must be validated for your
model, between 0.5 inclusive and 1 exclusive. Empty settings stop the launcher.
PocketSphinx's `1e-34` is not a probability and is invalid for microWakeWord.

Obtain a current trusted CA bundle, for example Mozilla's trust roots distributed
by [curl's CA Extract service](https://curl.se/docs/caextract.html). Download
`https://curl.se/ca/cacert.pem` over verified HTTPS on the host and compare its
SHA-256 to the published checksum. Save as private `cert.pem`. Refresh this
bundle periodically; never disable TLS verification to bypass certificate errors.

## 4. Stage over USB, then check network and time

First inspect existing state; do not overwrite an existing release or key. On a
fresh installation, create root-owned directories and transfer files:

```sh
sh <<'STAGE'
set -eu
adb shell 'id; getprop ro.product.cpu.abi; date -u'
adb shell 'mkdir -p /data/local/tmp/dotty; chmod 700 /data/local/tmp/dotty'
adb shell 'test ! -e /data/local/tmp/dotty/current && test ! -e /data/local/tmp/dotty/api-key.txt && test ! -e /data/local/tmp/dotty/runtime.conf && test ! -e /data/local/tmp/dotty/cert.pem && test ! -e /data/local/tmp/dotty/dotty-probe && test ! -e /data/local/tmp/dotty/dotty-calibrate'
# Any failed check stops this shell before transfer; preserve existing installations.
adb push build/release /data/local/tmp/dotty/current
adb push build/client/dotty-probe /data/local/tmp/dotty/dotty-probe
adb push build/client/dotty-calibrate /data/local/tmp/dotty/dotty-calibrate
adb push /path/to/private/api-key.txt /data/local/tmp/dotty/api-key.txt
adb push /path/to/private/cert.pem /data/local/tmp/dotty/cert.pem
adb push /path/to/private/runtime.conf /data/local/tmp/dotty/runtime.conf
adb shell 'chown -R 0:0 /data/local/tmp/dotty; chmod -R go-rwx /data/local/tmp/dotty'
adb shell 'chmod 700 /data/local/tmp/dotty/current/dotty* /data/local/tmp/dotty/dotty-probe /data/local/tmp/dotty/dotty-calibrate /data/local/tmp/dotty/current/libc/ld-linux-armhf.so.3'
adb shell 'chmod 600 /data/local/tmp/dotty/api-key.txt /data/local/tmp/dotty/cert.pem /data/local/tmp/dotty/runtime.conf /data/local/tmp/dotty/current/wake/model.tflite'
STAGE
```

Desired ownership is root:root throughout; directories/launchers/loader 0700,
key/config/model 0600, shared libraries root-readable and inaccessible to other
users. The supplied wrapper resolves libraries relative to itself. Never copy
these glibc libraries into `/system/lib` or replace the system linker.

Wi-Fi must already be configured before stopping the original setup services.
Use your firmware's supported onboarding while available, or your own private
wpa_supplicant configuration procedure with a recovery connection. This package
does not provision Wi-Fi credentials. Find the existing network ID with:

```sh
adb shell 'wpa_cli -p /data/misc/wifi/sockets list_networks'
```

Put the matching ID in `DOTTY_WIFI_NETWORK_ID`. The command prints network names;
keep its output private. Verify connectivity and DHCP before testing Gemini.
Check `date -u` against actual UTC and repair time using the firmware's supported
time service. The client rejects dates before 2025, but passing that guard alone
does not mean the clock is accurate enough for TLS.

## 5. Bring up one function at a time

Run **one client/capture process at a time**. Stop an existing Orion service using
its known firmware-specific mechanism. For the tested firmware, the service list
in `scripts/dotty_init.sh` identifies competing Amazon audio/LED services; inspect
and stop only applicable services before audio tests. Preserve their previous
state for recovery. `scripts/audio-prepare.sh` is an optional route diagnostic;
the client itself configures its speaker/microphone routes. An enforcing SELinux
policy may need appropriate device permissions. The demonstrated rooted setup
used permissive SELinux; enforcing-mode compatibility is not established.
The launcher leaves SELinux unchanged unless you explicitly opt in.

Enter an interactive root device shell (`adb shell`) and load your private config:

```sh
. /data/local/tmp/dotty/runtime.conf
client="$DOTTY_RELEASE/dottyd-wake"
"$client" -mode speaker-test -volume 3
"$client" -mode loopback -volume 3
"$client" -mode api-test -model "$DOTTY_MODEL" \
  -key-file "$DOTTY_KEY_FILE" -ca-file "$DOTTY_CA_FILE" \
  -dns "$DOTTY_DNS" -out /data/local/tmp/dotty/api-test.wav
"$client" -mode live -wake -supervise=false \
  -wake-helper "$DOTTY_RELEASE/dotty-wake" -wake-model "$DOTTY_RELEASE/wake" \
  -wake-threshold "$DOTTY_WAKE_THRESHOLD" -model "$DOTTY_MODEL" \
  -key-file "$DOTTY_KEY_FILE" -ca-file "$DOTTY_CA_FILE" -dns "$DOTTY_DNS" -volume 3
```

Run each line separately and inspect its result before the next. Speaker-test
plays a tone: confirm it is audible and clean. In loopback, press Action to record three seconds locally and play
back; stop it with Ctrl-C after checking clarity. API-test sends a text request
and writes a new reply WAV; it does not open the microphone or speaker. It refuses
an existing output path. The final command starts local wake detection and can
send microphone audio to Google after activation. Confirm the selected threshold,
hardware mute, Action, volume, idle disconnect and error recovery. Use the
[operation checklist](PHASE1.md). Fresh speech and background-noise tests matter
more than replaying training clips.

The speaker needs the correct internal route: PCM samples alone are insufficient.
On the tested board, both DAC mixer paths and I²S must be enabled for the amplifier
and speaker. A wrong route can yield silence or damaged-sounding output even
when cloud replies and PCM data are valid. Check local speaker-test first; then
check stream format, gain, buffering and route state independently.

## 6. Startup and maintenance

Only after manual tests pass, copy `scripts/dotty_init.sh` to a private executable
path. For a supervised test from an interactive root `adb shell`, run:

```sh
DOTTY_RESTART_ADB=0 sh /path/to/staged/dotty_init.sh
```

Keep this shell open during the test; Ctrl-C stops the foreground launcher. This
changes service state but skips restarting ADB, avoiding loss of its own shell.
The normal launcher defaults to `DOTTY_RESTART_ADB=1`; it must be launched from a
firmware service or another verified detached context. The USB-only/wireless ADB
port choice takes effect when ADB is restarted; the manual-test override does
not change an already-running ADB listener. Preserve your recovery connection. It does **not** install itself at
boot. **Persistent boot integration is not provided.** Firmware-specific init
changes need a tested recovery path, correct ownership/SELinux context and an
explicit review of the service definition before any boot modification.

The template appends `/data/local/tmp/dotty_init.log` and
`/data/local/tmp/dotty/live.log`; it does not rotate them. Check `du -k` and free
space regularly. For maintenance, stop your Orion launcher, preserve any logs
you need privately, truncate these two files with `: > PATH`, then restart.
Do not share raw logs without checking them for identifiers and errors containing
private context. The launcher does not retain microphone recordings; calibration
and API-test outputs are separately created files you manage yourself.

## Using a coding agent

Ask it to read this guide, the architecture and training notes first; verify your
hardware through USB; build and test locally; and show a proposed device layout
before installation. Give it placeholders and private file paths, not pasted
keys or recordings. Require separate approval for firmware/boot changes. Ask for
checks of hardware mute and local waiting before accepting a cloud voice test.
Never ask it to infer a working threshold from the video alone.
