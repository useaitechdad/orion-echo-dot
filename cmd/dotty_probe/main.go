// dotty_probe reports per-channel audio levels without saving any recording.
package main

import (
	"context"
	"dotty/internal/audio"
	"fmt"
	"math"
	"os"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	d, err := audio.OpenCapture()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer d.Close()
	b := make([]byte, 256*27)
	var sum [9]float64
	var peak [9]float64
	frames := 0
	start := time.Now()
	report := start
	for ctx.Err() == nil {
		n, err := d.Transfer(ctx, b)
		if err != nil {
			if ctx.Err() == nil {
				fmt.Println(err)
			}
			return
		}
		for f := 0; f < n/27; f++ {
			for c := 0; c < 9; c++ {
				x := b[f*27+c*3:]
				v := float64(int32(x[0])|int32(x[1])<<8|int32(int8(x[2]))<<16) / 256
				sum[c] += v * v
				peak[c] = math.Max(peak[c], math.Abs(v))
			}
			frames++
		}
		if time.Since(report) >= time.Second {
			fmt.Printf("t=%.1fs frames=%d rms=", time.Since(start).Seconds(), frames)
			for c := 0; c < 9; c++ {
				fmt.Printf(" %.1f", math.Sqrt(sum[c]/float64(frames)))
			}
			fmt.Printf(" peak=")
			for _, v := range peak {
				fmt.Printf(" %.0f", v)
			}
			fmt.Println()
			sum = [9]float64{}
			peak = [9]float64{}
			frames = 0
			report = time.Now()
		}
	}
}
