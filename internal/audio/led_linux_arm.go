//go:build linux && arm

package audio

import (
	"fmt"
	"os"
	"strings"
)

// Ring uses the kernel driver's RGB frame interface. One frame update avoids
// competing raw I2C writes and lets diagnostics read the current frame back.
type Ring struct{}

const ringDriver = "/sys/bus/i2c/devices/0-003f/"

func OpenRing() (*Ring, error) {
	// Stopping ledcontroller alone does not stop the kernel's boot animation.
	// It otherwise keeps replacing our status colors with a rotating ring.
	if err := os.WriteFile(ringDriver+"boot_animation", []byte("0\n"), 0); err != nil {
		return nil, fmt.Errorf("disable LED boot animation: %w", err)
	}
	r := &Ring{}
	if err := r.Set([3]byte{}); err != nil {
		return nil, err
	}
	return r, nil
}
func (r *Ring) Set(rgb [3]byte) error {
	frame := strings.Repeat(fmt.Sprintf("%02x%02x%02x", rgb[0], rgb[1], rgb[2]), 12) + "\n"
	if err := os.WriteFile(ringDriver+"frame", []byte(frame), 0); err != nil {
		return fmt.Errorf("LED frame: %w", err)
	}
	return nil
}
func (r *Ring) Close() error { return r.Set([3]byte{}) }
