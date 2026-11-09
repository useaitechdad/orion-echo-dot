#!/system/bin/sh
# Isolated playback test; only run after snapshot and with other audio owners stopped.
set -eu
if [ "$#" -ne 1 ] || [ ! -r "$1" ]; then
    echo 'usage: sh audio-play.sh /path/to/known-good-48k-stereo.wav' >&2
    exit 2
fi
cleanup() {
    tinymix 'MFP Gpio Mute' Off
    tinymix 'Ext_Speaker_Amp_Switch' Off
}
trap cleanup EXIT
trap 'exit 130' INT TERM
tinymix 'Ext_Speaker_Amp_Switch' On
tinymix 'MFP Gpio Mute' On
/system/bin/tinyplay "$1" -D 0 -d 23
echo 'tinyplay completed; this is NOT confirmation of audible sound.'
