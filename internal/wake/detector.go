// Package wake runs a local keyword helper over anonymous pipes. It has no network access.
package wake

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

type Detector struct {
	input, output *os.File
	cmd           *exec.Cmd
	done          chan error
	once          sync.Once
}

func Open(ctx context.Context, path, model string, threshold float64) (*Detector, error) {
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		inR.Close()
		inW.Close()
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, model, strconv.FormatFloat(threshold, 'g', -1, 64))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, os.Stderr
	if err = cmd.Start(); err != nil {
		inR.Close()
		inW.Close()
		outR.Close()
		outW.Close()
		return nil, err
	}
	inR.Close()
	outW.Close()
	d := &Detector{input: inW, output: outR, cmd: cmd, done: make(chan error, 1)}
	go func() { d.done <- cmd.Wait() }()
	_ = outR.SetReadDeadline(time.Now().Add(20 * time.Second))
	var ready [1]byte
	if _, err = io.ReadFull(outR, ready[:]); err != nil || ready[0] != 'R' {
		d.Close()
		return nil, fmt.Errorf("local wake detector not ready: %v", err)
	}
	return d, nil
}

// Process and Reset have one caller. A stalled/crashed helper fails closed.
func (d *Detector) Process(pcm []byte) (bool, error) {
	if len(pcm) > 4096 || len(pcm)%2 != 0 {
		return false, fmt.Errorf("invalid wake PCM packet length")
	}
	frame := make([]byte, len(pcm)+2)
	binary.LittleEndian.PutUint16(frame, uint16(len(pcm)))
	copy(frame[2:], pcm)
	if err := d.input.SetWriteDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return false, err
	}
	if _, err := d.input.Write(frame); err != nil {
		return false, fmt.Errorf("wake input: %w", err)
	}
	if err := d.output.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return false, err
	}
	var reply [1]byte
	if _, err := io.ReadFull(d.output, reply[:]); err != nil {
		return false, fmt.Errorf("wake output: %w", err)
	}
	if reply[0] != '0' && reply[0] != '1' {
		return false, fmt.Errorf("invalid wake detector reply")
	}
	return reply[0] == '1', nil
}
func (d *Detector) Reset() error { _, err := d.Process(nil); return err }
func (d *Detector) Close() {
	d.once.Do(func() { d.input.Close(); d.output.Close(); _ = d.cmd.Process.Kill(); <-d.done })
}
