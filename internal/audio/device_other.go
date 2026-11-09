//go:build !linux || !arm

package audio

import (
	"context"
	"fmt"
)

type Device struct{}

func OpenCapture() (*Device, error) {
	return nil, fmt.Errorf("Echo audio requires linux/arm (GOARM=7)")
}
func OpenSpeaker() (*Device, error) { return OpenCapture() }
func (*Device) Transfer(context.Context, []byte) (int, error) {
	return 0, fmt.Errorf("Echo hardware unavailable")
}
func (*Device) Reset() error { return fmt.Errorf("Echo hardware unavailable") }
func (*Device) Close() error { return nil }
