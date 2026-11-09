# microWakeWord native helper

This C++ helper evaluates a compact phrase model locally. The Go client supplies
PCM and receives detection events. The personalized model used in the demo is
not included; prepare and validate your own model and compatible runtime bundle.

`main.cc` uses TensorFlow Lite's C ABI and the microfrontend from
`pymicro-features` 2.0.2. Dependency commits and file hashes are in
`training/microwakeword/dependencies.json`. No dependency or model is downloaded
by the build script. The runtime does not capture, save or transmit audio.

## Build and protocol

```sh
python3 scripts/build-microwake.py \
  --frontend /path/to/pymicro_features-2.0.2 \
  --library /path/to/libtensorflowlite_c.dylib \
  --output /path/to/new/dotty-microwake-host
```

For the Dot, use the pinned ARM C library and a compiler targeting
`arm-linux-gnueabihf.2.34`, Cortex-A7/NEON. The build adds PIE for Linux.
The successful ARM build used Zig 0.13.0. FireOS needs an explicitly invoked,
isolated compatible glibc loader and libraries; it must not replace system
libraries. A non-PIE candidate crashed before `main`; its PIE successor ran.
The `dotty-wake` wrapper expects `dotty-microwake`, `libc/` and `lib/` beside it;
it invokes the isolated loader with `exec`, retaining client cancellation behavior.

Invocation: `dotty-microwake MODEL_DIRECTORY PROBABILITY_THRESHOLD`.
The directory contains `model.tflite`. Threshold must be finite, at least 0.5,
and less than 1. PocketSphinx thresholds such as `1e-34` are invalid here.

The existing Go framed protocol is retained: `R` when ready, then little-endian
uint16 byte length followed by mono 16 kHz signed-16 PCM. Length zero resets all
frontend and inference state; other lengths must be even and at most 4096.
Each packet receives `0` or `1`. EOF in a partial packet is an error.

Frontend: 30 ms windows, 10 ms step, 40 mel channels (125–7500 Hz), noise
reduction and PCAN matching the pinned upstream frontend. Models must have one
int8/uint8 input `[1, stride, 40]` and one quantized scalar output. Detection uses
the mean of three probabilities, with hysteresis to avoid repeated triggers
inside one high-probability region. Explicit reset also rearms detection.
`ORION_WAKE_SCORES=1` prints probability values to stderr for file-only evaluation.

## Validation and limits

```sh
python3 scripts/test-microwake.py /path/to/helper /path/to/model \
  --threshold .9 --positive /path/to/mono16k.wav=3 \
  --negative /path/to/unrelated-speech.wav
```

This checks framing, partial-frame reset, repeated resets, silence, invalid
packets, threshold mismatch, and supplied recognition counts. It does not prove
general accuracy. Private fixtures stay outside public source packages.

Validate frontend output and protocol behavior against a public reference,
then evaluate your own model on held-out speech, noise and hardware. Reference
agreement does not establish phrase accuracy or a household false-wake rate.

Runtime libraries and compiled bundles are not included. Preserve the relevant
upstream notices and review redistribution obligations for any model/data,
glibc/C++ libraries and corresponding source before sharing a runtime bundle.

## Obtain the pinned dependencies

The source and binary URLs below correspond to hashes already recorded in
`training/microwakeword/dependencies.json`. Run from the repository root into a
new build directory. Downloading these dependencies does not operate the Dot.

```sh
(
set -eu
mkdir -p build/microwake-deps
curl -fL https://files.pythonhosted.org/packages/39/46/328092b4df890385594cf3d3e6015da72d77f63c58d3057276ac2353e891/pymicro_features-2.0.2.tar.gz \
  -o build/microwake-deps/pymicro_features-2.0.2.tar.gz
curl -fL https://raw.githubusercontent.com/OHF-Voice/pymicro-wakeword/333da550c54b95192af472cfe37d98dab2f2086e/lib/linux_armv7l/libtensorflowlite_c.so \
  -o build/microwake-deps/libtensorflowlite_c-arm.so
python3 - <<'PY'
import hashlib, json
from pathlib import Path
pins = json.loads(Path('training/microwakeword/dependencies.json').read_text())['files']
for name in ('pymicro_features-2.0.2.tar.gz', 'libtensorflowlite_c-arm.so'):
    assert hashlib.sha256((Path('build/microwake-deps') / name).read_bytes()).hexdigest() == pins[name], name
print('Dependency hashes match')
PY
# Only extract after hashes match.
tar -xzf build/microwake-deps/pymicro_features-2.0.2.tar.gz -C build/microwake-deps
)
```

The 32-bit ARM library is supplied by the pinned OHF runtime repository, **not**
by the arm64 release of `tphakala/tflite_c`. See its
[ARM provenance](https://github.com/OHF-Voice/pymicro-wakeword/blob/333da550c54b95192af472cfe37d98dab2f2086e/lib/linux_armv7l/SOURCE).
For host evaluation, the same pinned repository has `lib/darwin_arm64/` and
`lib/linux_amd64/` variants (confirm the tree for your host); compare the actual
library against the matching macOS/Linux-amd64 hash in `dependencies.json`.
Use a native host library for file-only evaluation, never the ARM library on a Mac.

## ARM compile and private runtime bundle

With `uv` installed, prepare Zig 0.13.0 and compile:

```sh
uv venv build/microwake-tools
uv pip install --python build/microwake-tools/bin/python ziglang==0.13.0
zig=$(build/microwake-tools/bin/python -c 'import pathlib,ziglang; print(pathlib.Path(ziglang.__file__).parent / "zig")')
python3 scripts/build-microwake.py \
  --frontend build/microwake-deps/pymicro_features-2.0.2 \
  --library build/microwake-deps/libtensorflowlite_c-arm.so \
  --compiler "$zig c++ -target arm-linux-gnueabihf.2.34 -mcpu=cortex_a7+neon" \
  --output build/release/dotty-microwake
mkdir -p build/release/lib build/release/libc build/release/wake
cp native/microwake/dotty-wake build/release/dotty-wake
cp build/microwake-deps/libtensorflowlite_c-arm.so build/release/lib/libtensorflowlite_c.so
cp build/client/dottyd build/release/dottyd-wake
```

Obtain the isolated runtime from official **Debian 12 Bookworm armhf** packages:
`libc6` (glibc 2.36), `libgcc-s1` and `libstdc++6` (GCC 12 family). Use current
Bookworm security revisions, record the exact package versions/checksums locally,
and retest the resulting bundle. Do not mix releases or architectures. A clean
Debian 12 container is a convenient extraction environment; the following runs
**inside that container**, as root, not on FireOS or your host OS:

```sh
dpkg --add-architecture armhf
apt-get update
mkdir -p /work/debs /work/extracted
cd /work/debs
apt-get download libc6:armhf libgcc-s1:armhf libstdc++6:armhf
sha256sum *.deb > package-checksums.txt
for archive in ./*.deb; do
  dpkg-deb -f "$archive" Package Version Architecture
  dpkg-deb -x "$archive" /work/extracted
done
```

Copy from the extracted package tree into `build/release/libc` on the host,
**dereferencing symlinks** (`cp -L`) so every item is a regular file:

| Extracted location | Destination filename |
|---|---|
| `lib/arm-linux-gnueabihf/ld-linux-armhf.so.3` | `ld-linux-armhf.so.3` |
| `lib/arm-linux-gnueabihf/libc.so.6` | `libc.so.6` |
| `lib/arm-linux-gnueabihf/libm.so.6` | `libm.so.6` |
| `lib/arm-linux-gnueabihf/libpthread.so.0` | `libpthread.so.0` |
| `lib/arm-linux-gnueabihf/libgcc_s.so.1` | `libgcc_s.so.1` |
| `usr/lib/arm-linux-gnueabihf/libstdc++.so.6` | `libstdc++.so.6` |

Inspect each ELF with `file`/`readelf`; it must be ARM 32-bit. Inspect the helper's
`NEEDED` entries and the libraries' dependencies; all non-system requirements
must be present under the isolated directory. The wrapper invokes the loader
explicitly with `--library-path`; never alter the device's system libraries.
Package references: [libc6](https://packages.debian.org/bookworm/armhf/libc6),
[libgcc-s1](https://packages.debian.org/bookworm/armhf/libgcc-s1),
[libstdc++6](https://packages.debian.org/bookworm/armhf/libstdc++6).

Finally copy **your** validated `model.tflite` into `build/release/wake/`. Perform
file-only host evaluation and then the manual device tests in the root `SETUP.md`.
Security updates can change runtime bytes; this recipe does not claim that every
future Bookworm revision has been tested on this FireOS build.

## Host evaluation libraries and ELF checks

Download the library for the **evaluation host**, using the exact pinned commit:

```sh
# macOS arm64:
curl -fL https://raw.githubusercontent.com/OHF-Voice/pymicro-wakeword/333da550c54b95192af472cfe37d98dab2f2086e/lib/darwin_arm64/libtensorflowlite_c.dylib \
  -o build/microwake-deps/libtensorflowlite_c.dylib
# Or Linux amd64:
curl -fL https://raw.githubusercontent.com/OHF-Voice/pymicro-wakeword/333da550c54b95192af472cfe37d98dab2f2086e/lib/linux_amd64/libtensorflowlite_c.so \
  -o build/microwake-deps/libtensorflowlite_c-linux-amd64.so
```

Use the hash-check snippet above with the downloaded filename substituted in its
file list. On Linux, copy the verified library to `libtensorflowlite_c.so` in the
same directory and set `LD_LIBRARY_PATH` to that directory's absolute path when
running the host helper; the Linux build does not add an rpath. macOS adds an
rpath for its supplied `.dylib`. Compile with the matching library and `clang++`.

On a host with binutils, inspect the ARM bundle:

```sh
readelf -d build/release/dotty-microwake
readelf -d build/release/lib/libtensorflowlite_c.so
```

For the pinned library, SONAME is `libtensorflowlite_c.so`. The compiled helper's
NEEDED entries are `libtensorflowlite_c.so`, `libm.so.6`, `libpthread.so.0` and
`libc.so.6`; the pinned TFLite library additionally needs `libstdc++.so.6`,
`libgcc_s.so.1` and `ld-linux-armhf.so.3`. No NEEDED entry should contain a host
path. Inspect the packaged runtime libraries too. If a different reviewed build
requires `libdl.so.2` or `librt.so.1`, obtain it from the same extracted `libc6`
package and repeat validation; do not mix arbitrary libraries.

The Go client retains a broader (0,1) threshold range for historical PocketSphinx
compatibility. A microWakeWord value below 0.5 is rejected by the helper and
produces an attention error, never a working wake detector. Use the model-specific
[0.5,1) range from the outset.
