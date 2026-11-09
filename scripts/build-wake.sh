#!/bin/sh
# Historical PocketSphinx build on macOS/Linux -> static Linux ARMv7.
# Requires uv, curl, tar, sha256sum or shasum, and make. Does not deploy or touch the Dot.
set -eu
cd "$(dirname "$0")/.."
project=$(pwd)
build=${DOTTY_WAKE_BUILD_ROOT:-${TMPDIR:-/tmp}/dotty-wake-repro}
mkdir -p "$build"
archive="$build/pocketsphinx-v5.1.1.tar.gz"
if [ ! -f "$archive" ]; then
    curl -fL https://api.github.com/repos/cmusphinx/pocketsphinx/tarball/v5.1.1 -o "$archive"
fi
hash_files() {
    if command -v sha256sum >/dev/null 2>&1; then sha256sum "$@"; else shasum -a 256 "$@"; fi
}
actual=$(hash_files "$archive" | cut -d ' ' -f 1)
expected=4e0cc167a9b745422ab524e4ebc303ad5b10a0819c8e074689a95d3e5753a097
[ "$actual" = "$expected" ] || { echo 'PocketSphinx archive checksum mismatch' >&2; exit 1; }
export UV_CACHE_DIR="$build/uv-cache"
if [ ! -x "$build/tools/bin/python" ]; then uv venv "$build/tools"; fi
uv pip install --python "$build/tools/bin/python" cmake==3.31.6 ziglang==0.13.0
zig=$("$build/tools/bin/python" -c 'import pathlib,ziglang; print(pathlib.Path(ziglang.__file__).parent / "zig")')
mkdir -p "$build/source"
if [ ! -f "$build/source/CMakeLists.txt" ]; then
    tar -xzf "$archive" -C "$build/source" --strip-components=1
fi
cat > "$build/cc" <<SCRIPT
#!/bin/sh
export ZIG_GLOBAL_CACHE_DIR='$build/zig-cache'
exec '$zig' cc -target arm-linux-musleabihf -mcpu=cortex_a7 "\$@"
SCRIPT
cat > "$build/ar" <<SCRIPT
#!/bin/sh
exec '$zig' ar "\$@"
SCRIPT
cat > "$build/ranlib" <<SCRIPT
#!/bin/sh
exec '$zig' ranlib "\$@"
SCRIPT
chmod +x "$build/cc" "$build/ar" "$build/ranlib"
cat > "$build/toolchain.cmake" <<CMAKE
set(CMAKE_SYSTEM_NAME Linux)
set(CMAKE_SYSTEM_PROCESSOR arm)
set(CMAKE_C_COMPILER "$build/cc")
set(CMAKE_AR "$build/ar")
set(CMAKE_RANLIB "$build/ranlib")
set(CMAKE_EXE_LINKER_FLAGS "-static")
CMAKE
cmake="$build/tools/bin/cmake"
"$cmake" -S "$build/source" -B "$build/arm" -DCMAKE_TOOLCHAIN_FILE="$build/toolchain.cmake" -DCMAKE_BUILD_TYPE=Release -DBUILD_TESTING=OFF -DBUILD_SHARED_LIBS=OFF
"$cmake" --build "$build/arm" --target pocketsphinx -j 4
output=${DOTTY_OUTPUT_DIR:-"$project/build/wake"}
mkdir -p "$output/wake/en-us"
"$build/cc" -O2 -static -I"$build/source/include" -I"$build/arm/include" native/wake/main.c "$build/arm/libpocketsphinx.a" -lm -o "$output/dotty-wake"
cp -R "$build/source/model/en-us/en-us/." "$output/wake/en-us/"
cp "$build/source/LICENSE" "$output/wake/LICENSE.pocketsphinx"
cp "$project/assets/wake/wake.dict" "$project/assets/wake/wake.kws" "$output/wake/"
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -o "$output/dottyd-wake" ./cmd/dottyd
hash_files "$output/dotty-wake" "$output/dottyd-wake"
