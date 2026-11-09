package audio

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// ConfigureMicrophones is shared by live capture and the consented recorder.
func ConfigureMicrophones(ctx context.Context) error {
	for _, adc := range []string{"A", "B", "C", "D"} {
		prefix := "ADC_" + adc
		controls := [][]string{{prefix + " MICPGA Volume Ctrl", "40", "40"}}
		for _, side := range []string{"Left", "Right"} {
			channel := "L"
			if side == "Right" {
				channel = "R"
			}
			controls = append(controls, []string{prefix + " " + side + " Ip Select " + prefix + " DIF1_" + channel + " switch", "1"})
		}
		for _, control := range controls {
			call, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := exec.CommandContext(call, "/system/bin/tinymix", control...).Run()
			cancel()
			if err != nil {
				return fmt.Errorf("microphone setup %s: %w", control[0], err)
			}
		}
	}
	return nil
}
