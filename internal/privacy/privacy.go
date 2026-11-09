// Package privacy reads the sole verified hardware privacy attribute.
package privacy

import (
	"fmt"
	"os"
	"strings"
)

const StatePath = "/sys/devices/soc/10010000.keypad/amz_privacy/privacy_state"

func Read() (bool, error) {
	b, err := os.ReadFile(StatePath)
	if err != nil {
		return true, fmt.Errorf("read hardware privacy: %w", err)
	}
	return Parse(string(b))
}

func Parse(value string) (bool, error) {
	switch strings.TrimSpace(value) {
	case "0":
		return false, nil
	case "1":
		return true, nil
	default:
		return true, fmt.Errorf("invalid hardware privacy state")
	}
}
