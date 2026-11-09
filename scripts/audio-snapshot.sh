#!/system/bin/sh
# Private diagnostic snapshot; review/redact output before filming or sharing.
# Process/kernel/input output can contain device or network identifiers.
# Do not inspect legacy amz_privacy/state or power_button_state (kernel panic).
echo '=== UTC / kernel / build ==='
date -u
uname -a
getprop ro.build.display.id
getprop sys.boot_completed
getenforce
echo '=== relevant services ==='
for svc in dotty_init flash_recovery mixer audioserver uxeventd puffin oobed_on_boot ledcontroller; do
    echo "$svc: $(getprop init.svc.$svc)"
done
echo '=== processes ==='
ps
echo '=== mixer ==='
tinymix
echo '=== ALSA ==='
cat /proc/asound/cards
cat /proc/asound/pcm
for f in /proc/asound/card0/pcm23p/sub0/status /proc/asound/card0/pcm23p/sub0/hw_params /proc/asound/card0/pcm24c/sub0/status /proc/asound/card0/pcm24c/sub0/hw_params; do
    echo "$f"
    cat "$f"
done
echo '=== sound device owners ==='
for p in /proc/[0-9]*; do
    links=$(ls -l "$p/fd" 2>/dev/null | grep /dev/snd/)
    if [ -n "$links" ]; then
        echo "$p: $(cat "$p/comm" 2>/dev/null)"
        echo "$links"
    fi
done
echo '=== input devices / jack state ==='
cat /proc/bus/input/devices
for f in /sys/class/switch/*/state; do echo "$f"; cat "$f"; done
echo '=== recent kernel audio events ==='
dmesg | grep -iE 'aic|codec|amp_|i2s|snd|audio|avc:' | tail -n 100
