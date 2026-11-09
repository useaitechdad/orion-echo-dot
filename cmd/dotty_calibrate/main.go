// dotty_calibrate makes an explicitly requested local wake calibration clip.
// No network/key handling. Action starts one bounded recording; Mute cancels.
package main

import (
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"dotty/internal/audio"
	"dotty/internal/privacy"
)

func main() {
	out := flag.String("out", "", "new private file for 16 kHz mono S16_LE PCM")
	seconds := flag.Int("seconds", 12, "recording duration, 1..15 seconds")
	flag.Parse()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, *out, *seconds); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(parent context.Context, out string, seconds int) error {
	if runtime.GOOS != "linux" || runtime.GOARCH != "arm" || out == "" || seconds < 1 || seconds > 15 {
		return errors.New("requires Echo linux/arm, explicit -out and seconds 1..15")
	}
	lock, err := os.OpenFile("/data/local/tmp/dotty.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return err
	}
	ring, err := audio.OpenRing()
	if err != nil {
		return err
	}
	defer func() {
		muted, err := privacy.Read()
		if err != nil || muted {
			_ = ring.Set([3]byte{40, 0, 0})
		} else {
			_ = ring.Set([3]byte{0, 0, 3})
		}
	}()
	if err := requireUnmuted(privacy.Read); err != nil {
		return err
	}
	if err = ring.Set([3]byte{20, 10, 0}); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()
	keys, err := os.Open("/dev/input/event1")
	if err != nil {
		return err
	}
	defer keys.Close()
	start := make(chan struct{}, 1)
	go func() {
		var event [16]byte
		for {
			if _, err := io.ReadFull(keys, event[:]); err != nil {
				cancel()
				return
			}
			if binary.LittleEndian.Uint16(event[8:]) != 1 || binary.LittleEndian.Uint32(event[12:]) != 1 {
				continue
			}
			switch binary.LittleEndian.Uint16(event[10:]) {
			case 113:
				cancel()
				return
			case 138:
				select {
				case start <- struct{}{}:
				default:
				}
			}
		}
	}()
	fmt.Printf("READY: Action starts %d-second local recording; white=recording; Mute cancels. No network.\n", seconds)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-start:
	}
	if err := requireUnmuted(privacy.Read); err != nil {
		return err
	}
	if err := audio.ConfigureMicrophones(ctx); err != nil {
		return err
	}
	err = writeRecording(out, func(f *os.File) error {
		mic, err := audio.OpenCapture()
		if err != nil {
			return err
		}
		defer mic.Close()
		if err = ring.Set([3]byte{32, 32, 32}); err != nil {
			return err
		}
		deadline, stop := context.WithTimeout(ctx, time.Duration(seconds+3)*time.Second)
		defer stop()
		fmt.Println("RECORDING")
		remaining := seconds * 16000 * 2
		buf := make([]byte, 256*27)
		for remaining > 0 {
			if err := requireUnmuted(privacy.Read); err != nil {
				return err
			}
			n, err := mic.Transfer(deadline, buf)
			if err != nil {
				return err
			}
			pcm, err := audio.Mono16(buf[:n], 6)
			if err != nil {
				return err
			}
			if len(pcm) > remaining {
				pcm = pcm[:remaining]
			}
			if err := deadline.Err(); err != nil {
				return err
			}
			if err := requireUnmuted(privacy.Read); err != nil {
				return err
			}
			n, err = f.Write(pcm)
			if err != nil {
				return err
			}
			remaining -= n
		}
		if err := requireUnmuted(privacy.Read); err != nil {
			return err
		}
		if err = f.Sync(); err != nil {
			return err
		}
		return nil
	})
	if err == nil {
		fmt.Printf("COMPLETE: %d bytes, 16000 Hz mono S16_LE, gain 6\n", seconds*32000)
	}
	return err
}
