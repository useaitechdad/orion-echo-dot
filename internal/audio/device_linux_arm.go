//go:build linux && arm

package audio

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

// The Echo runs a 32-bit userspace. These are its 32-bit kernel ABI structures,
// deliberately restricted to GOARCH=arm rather than silently used on a Mac.
type transfer struct {
	result int32
	buf    uintptr
	frames uint32
}
type Device struct {
	file       *os.File
	capture    bool
	frameBytes int
}

func ioctl(fd uintptr, request uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

func OpenCapture() (*Device, error) { return openPCM(true) }
func OpenSpeaker() (*Device, error) { return openPCM(false) }
func openPCM(capture bool) (*Device, error) {
	path := "/dev/snd/pcmC0D23p"
	format, channels, rate, period := uint32(2), uint32(2), uint32(48000), uint32(1024)
	frameBytes := 4
	periods := uint32(4)
	if capture {
		path = "/dev/snd/pcmC0D24c"
		format = 32
		channels = 9
		rate = 16000
		period = 256
		frameBytes = 27
		periods = 4
	}
	// O_NONBLOCK prevents another service's exclusive open from hanging startup.
	f, err := os.OpenFile(path, os.O_RDWR|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	d := &Device{file: f, capture: capture, frameBytes: frameBytes}
	ok := false
	defer func() {
		if !ok {
			f.Close()
		}
	}()
	p := make([]byte, 604)
	for m := 0; m < 3; m++ {
		for w := 0; w < 8; w++ {
			binary.LittleEndian.PutUint32(p[4+m*32+w*4:], 0xffffffff)
		}
	}
	for i := 0; i < 12; i++ {
		binary.LittleEndian.PutUint32(p[0x104+i*12+4:], 0xffffffff)
	}
	binary.LittleEndian.PutUint32(p[0x208:], 0xffffffff)
	refine := func() error {
		binary.LittleEndian.PutUint32(p[0x200:], 0xffffffff)
		return ioctl(f.Fd(), 0xc25c4110, unsafe.Pointer(&p[0]))
	}
	if err = refine(); err != nil {
		return nil, fmt.Errorf("%s initial HW_REFINE: %w", path, err)
	}
	// Set access and format masks exactly (not just their lowest word).
	clear(p[4:36])
	binary.LittleEndian.PutUint32(p[4:], 1<<3)
	clear(p[36:68])
	binary.LittleEndian.PutUint32(p[36+format/32*4:], 1<<(format%32))
	if err = refine(); err != nil {
		return nil, fmt.Errorf("%s format HW_REFINE: %w", path, err)
	}
	for _, v := range [][2]uint32{{2, channels}, {3, rate}, {5, period}, {7, periods}} {
		o := 0x104 + int(v[0])*12
		binary.LittleEndian.PutUint32(p[o:], v[1])
		binary.LittleEndian.PutUint32(p[o+4:], v[1])
		if err = refine(); err != nil {
			return nil, fmt.Errorf("%s HW_REFINE interval %d=%d: %w", path, v[0], v[1], err)
		}
	}
	if err = ioctl(f.Fd(), 0xc25c4111, unsafe.Pointer(&p[0])); err != nil {
		return nil, fmt.Errorf("%s HW_PARAMS: %w", path, err)
	}
	// Playback needs explicit start/stop thresholds; capture follows the verified
	// PREPARE + START sequence from the local round-trip diagnostic.
	if !capture {
		sw := make([]byte, 104)
		binary.LittleEndian.PutUint32(sw[4:], 1)               // period_step
		binary.LittleEndian.PutUint32(sw[12:], period)         // avail_min
		binary.LittleEndian.PutUint32(sw[16:], 1)              // xfer_align
		binary.LittleEndian.PutUint32(sw[20:], period*periods) // start_threshold
		binary.LittleEndian.PutUint32(sw[24:], period*periods) // stop_threshold
		boundary := period * periods
		for boundary <= (0x7fffffff-period*periods)/2 {
			boundary *= 2
		}
		binary.LittleEndian.PutUint32(sw[36:], boundary)
		if err = ioctl(f.Fd(), 0xc0684113, unsafe.Pointer(&sw[0])); err != nil {
			return nil, fmt.Errorf("%s SW_PARAMS: %w", path, err)
		}
	}
	if err = d.prepare(); err != nil {
		return nil, err
	}
	if !capture {
		// Use the driver's blocking write path (as tinyplay does). Opening stays
		// nonblocking so exclusive owners fail immediately; only configured writes
		// block, paced by the 48 kHz DMA clock rather than userspace polling.
		if err = syscall.SetNonblock(int(f.Fd()), false); err != nil {
			return nil, err
		}
	}
	ok = true
	return d, nil
}
func (d *Device) prepare() error {
	if err := ioctl(d.file.Fd(), 0x4140, nil); err != nil {
		return fmt.Errorf("PCM PREPARE: %w", err)
	}
	if d.capture {
		if err := ioctl(d.file.Fd(), 0x4142, nil); err != nil {
			return fmt.Errorf("PCM START: %w", err)
		}
	}
	return nil
}
func (d *Device) Reset() error {
	if err := ioctl(d.file.Fd(), 0x4143, nil); err != nil {
		return err
	}
	return d.prepare()
}
func (d *Device) Close() error { return d.file.Close() }

// Transfer returns actual frames, handles partial transfers and retries only
// EAGAIN/EINTR. XRUNs surface to the worker and are logged before recovery.
func (d *Device) Transfer(ctx context.Context, p []byte) (int, error) {
	if len(p) == 0 || len(p)%d.frameBytes != 0 {
		return 0, fmt.Errorf("unaligned PCM transfer")
	}
	// The ioctl ABI stores the buffer address as an integer. KeepAlive alone
	// does not prevent a Go stack move between constructing it and entering
	// the syscall; pinning forces stable storage for reads and writes.
	var pinned runtime.Pinner
	pinned.Pin(&p[0])
	defer pinned.Unpin()
	req := uintptr(0x400c4150)
	if d.capture {
		req = 0x800c4151
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		x := transfer{buf: uintptr(unsafe.Pointer(&p[0])), frames: uint32(len(p) / d.frameBytes)}
		err := ioctl(d.file.Fd(), req, unsafe.Pointer(&x))
		runtime.KeepAlive(p)
		if err == nil && x.result < 0 {
			err = syscall.Errno(-x.result)
		}
		if err == syscall.EAGAIN || err == syscall.EINTR || (err == nil && x.result == 0) {
			if time.Now().After(deadline) {
				return 0, fmt.Errorf("PCM transfer stalled for 3 seconds")
			}
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-time.After(2 * time.Millisecond):
			}
			continue
		}
		if err != nil {
			return 0, err
		}
		if x.result < 0 || uint32(x.result) > x.frames {
			return 0, fmt.Errorf("invalid kernel transfer length %d", x.result)
		}
		return int(x.result) * d.frameBytes, nil
	}
}
