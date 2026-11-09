//go:build !linux || !arm

package audio

import "fmt"

type Ring struct{}

func OpenRing() (*Ring, error)  { return nil, fmt.Errorf("Echo LED hardware unavailable") }
func (*Ring) Set([3]byte) error { return nil }
func (*Ring) Close() error      { return nil }
