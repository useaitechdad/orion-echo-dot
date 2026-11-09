// Package audio contains the format conversions used by Dotty's hardware and Live API.
package audio

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync"
)

// Mono16 averages the seven microphones, ignoring both reference channels.
// This is channel averaging, not calibrated directional beamforming. Fixed gain
// avoids turning quiet-room noise into full-scale audio through peak normalization.
func Mono16(raw []byte, gain float64) ([]byte, error) {
	if len(raw)%27 != 0 || gain <= 0 || math.IsNaN(gain) || math.IsInf(gain, 0) {
		return nil, fmt.Errorf("invalid nine-channel PCM or gain")
	}
	out := make([]byte, len(raw)/27*2)
	for f := 0; f < len(raw)/27; f++ {
		var sum int64
		for c := 0; c < 7; c++ {
			b := raw[f*27+c*3:]
			sum += int64(int32(b[0]) | int32(b[1])<<8 | int32(int8(b[2]))<<16)
		}
		value := math.Round(float64(sum) * gain / (7 * 256))
		value = math.Max(-32768, math.Min(32767, value))
		binary.LittleEndian.PutUint16(out[f*2:], uint16(int16(value)))
	}
	return out, nil
}

// Upsampler converts mono 24 kHz to dual-mono 48 kHz, retaining interpolation
// history across network chunks. Reset on interruptions and new connections.
type Upsampler struct {
	previous int16
	valid    bool
}

func (u *Upsampler) Convert(in []byte) ([]byte, error) {
	if len(in)%2 != 0 {
		return nil, fmt.Errorf("odd-length S16_LE audio")
	}
	out := make([]byte, len(in)*4)
	for i := 0; i < len(in)/2; i++ {
		s := int16(binary.LittleEndian.Uint16(in[i*2:]))
		if !u.valid {
			u.previous = s
			u.valid = true
		}
		mid := int16((int32(u.previous) + int32(s)) / 2)
		for j, v := range []int16{mid, s} {
			binary.LittleEndian.PutUint16(out[i*8+j*4:], uint16(v))
			binary.LittleEndian.PutUint16(out[i*8+j*4+2:], uint16(v))
		}
		u.previous = s
	}
	return out, nil
}

// Queue is bounded in bytes and always frame-aligned. Overflow fails the
// connection instead of silently dropping speech or growing memory indefinitely.
type Queue struct {
	mu    sync.Mutex
	data  []byte
	limit int
	reset bool
}

func NewQueue(limit int) *Queue { return &Queue{limit: limit} }
func (q *Queue) Add(p []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(p)%4 != 0 {
		return fmt.Errorf("unaligned stereo audio")
	}
	if len(p)+len(q.data) > q.limit {
		return fmt.Errorf("speaker queue exceeded %d bytes", q.limit)
	}
	q.data = append(q.data, p...)
	return nil
}
func (q *Queue) Clear()    { q.mu.Lock(); q.data = nil; q.reset = true; q.mu.Unlock() }
func (q *Queue) Len() int  { q.mu.Lock(); defer q.mu.Unlock(); return len(q.data) }
func (q *Queue) Free() int { q.mu.Lock(); defer q.mu.Unlock(); return q.limit - len(q.data) }
func (q *Queue) Take(dst []byte) (int, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	clear(dst)
	n := min(len(dst)/4*4, len(q.data))
	copy(dst, q.data[:n])
	q.data = q.data[n:]
	reset := q.reset
	q.reset = false
	return n, reset
}
