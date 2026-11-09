#!/system/bin/sh
# Orion startup template. Keep any device-specific choices in a root-owned 0600 file.
config=${DOTTY_CONFIG:-/data/local/tmp/dotty/runtime.conf}
if [ -r "$config" ]; then . "$config"; fi
: "${DOTTY_WAKE_THRESHOLD:?Set a validated microWakeWord probability threshold in runtime.conf}"
: "${DOTTY_MODEL:?Select a currently supported Gemini Live model in runtime.conf}"
exec >> /data/local/tmp/dotty_init.log 2>&1
echo "=== Orion supervised startup $(date) ==="

# Development privileges are explicit opt-ins; do not relax SELinux by default.
if [ "${DOTTY_ALLOW_PERMISSIVE:-0}" = 1 ]; then setenforce 0; fi
for svc in puffin oobed_on_boot oobe_setup oobed_on_press oobed_on_no_nw mixer uxeventd neo-coordinator ace_eventmgr boot2_anim_start ledcontroller; do
    stop "$svc"
done

# USB remains available. Wireless root debugging is off unless privately enabled.
if [ "${DOTTY_WIRELESS_ADB:-0}" = 1 ]; then
    setprop service.adb.tcp.port 5555
else
    setprop service.adb.tcp.port -1
fi
if [ "${DOTTY_RESTART_ADB:-1}" = 1 ]; then
    stop adbd
    start adbd
fi

# Network setup is owned/reaped by the supervisor and cannot block local wake.
release=${DOTTY_RELEASE:-/data/local/tmp/dotty/current}
if [ -x "$release/dottyd-wake" ]; then
    # The supervisor owns crash recovery. Missing helper/key/CA fails closed,
    # reaches a visible attention state, and never enables continuous listening.
    exec "$release/dottyd-wake" -mode live -wake -supervise \
        -wifi-network "${DOTTY_WIFI_NETWORK_ID:-0}" \
        -wake-helper "$release/dotty-wake" -wake-model "$release/wake" \
        -wake-threshold "${DOTTY_WAKE_THRESHOLD}" \
        -model "$DOTTY_MODEL" \
        -key-file "${DOTTY_KEY_FILE:-/data/local/tmp/dotty/api-key.txt}" \
        -ca-file "${DOTTY_CA_FILE:-/data/local/tmp/dotty/cert.pem}" \
        -dns "${DOTTY_DNS:-auto}" -volume 5 >> /data/local/tmp/dotty/live.log 2>&1
fi

# Even a missing executable must have an attention indicator. Mute takes priority.
echo "Client missing; reinstall the selected release and restart"
echo 0 > /sys/bus/i2c/devices/0-003f/boot_animation
while :; do
    muted=$(cat /sys/devices/soc/10010000.keypad/amz_privacy/privacy_state 2>/dev/null)
    red=030000
    if [ "$muted" != 1 ]; then red=280000; fi
    frame=""
    for pixel in $(seq 1 12); do frame="$frame$red"; done
    echo "$frame" > /sys/bus/i2c/devices/0-003f/frame
    sleep 1
    if [ "$muted" != 1 ]; then
        echo 000000000000000000000000000000000000000000000000000000000000000000000000 > /sys/bus/i2c/devices/0-003f/frame
    fi
    sleep 1
done
