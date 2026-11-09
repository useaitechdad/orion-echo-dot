package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// speakerPCM allows the proven system player and the diagnostic direct writer
// to share the same bounded speech queue.
type speakerPCM interface {
	Transfer(context.Context, []byte) (int, error)
	Reset() error
	Close() error
}

type tinyPlayer struct {
	writer  *os.File
	command *exec.Cmd
	done    chan error
	closed  bool
}

func openTinyPlayer() (*tinyPlayer, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	// Limit the pipe to one PCM period so cancellation cannot leave seconds of
	// speech hidden in the pipe. Failure is fatal rather than changing latency.
	if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, writer.Fd(), 1031, 4096); errno != 0 {
		reader.Close()
		writer.Close()
		return nil, fmt.Errorf("PCM pipe size: %w", errno)
	}
	cmd := exec.Command("/system/bin/tinyplay", "/proc/self/fd/0", "-D", "0", "-d", "23", "-p", "1024", "-n", "4")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = reader, os.Stderr, os.Stderr
	if err = cmd.Start(); err != nil {
		reader.Close()
		writer.Close()
		return nil, err
	}
	reader.Close()
	p := &tinyPlayer{writer: writer, command: cmd, done: make(chan error, 1)}
	go func() { p.done <- cmd.Wait() }()
	// Standard PCM WAV header. The process consumes the stream until EOF; the
	// large data size also permits players which honor the declared chunk size.
	hdr := make([]byte, 44)
	copy(hdr, "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:], 0x7ffff024)
	copy(hdr[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(hdr[16:], 16)
	binary.LittleEndian.PutUint16(hdr[20:], 1)
	binary.LittleEndian.PutUint16(hdr[22:], 2)
	binary.LittleEndian.PutUint32(hdr[24:], 48000)
	binary.LittleEndian.PutUint32(hdr[28:], 192000)
	binary.LittleEndian.PutUint16(hdr[32:], 4)
	binary.LittleEndian.PutUint16(hdr[34:], 16)
	copy(hdr[36:], "data")
	binary.LittleEndian.PutUint32(hdr[40:], 0x7ffff000)
	if _, err = writer.Write(hdr); err != nil {
		p.Close()
		return nil, err
	}
	return p, nil
}
func (p *tinyPlayer) Transfer(ctx context.Context, b []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	// An actual driver hang must not trap shutdown indefinitely.
	if err := p.writer.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
		return 0, err
	}
	return p.writer.Write(b)
}
func (p *tinyPlayer) Reset() error {
	log.Printf("Restarting tinyplay after stream failure; old process exit: %v", p.Close())
	next, err := openTinyPlayer()
	if err != nil {
		return fmt.Errorf("restart tinyplay: %w", err)
	}
	*p = *next
	return nil
}
func (p *tinyPlayer) Close() error {
	if p.closed {
		return nil
	}
	p.closed = true
	p.writer.Close()
	select {
	case err := <-p.done:
		return err
	case <-time.After(time.Second):
		_ = p.command.Process.Kill()
		return <-p.done
	}
}
