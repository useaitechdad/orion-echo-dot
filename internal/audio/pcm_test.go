package audio

import (
	"encoding/binary"
	"testing"
)

func TestCaptureSignGainAndReferenceExclusion(t *testing.T) {
	raw := make([]byte, 3*27)
	for f, v := range []int32{-256, 256, 8388607} {
		for c := 0; c < 9; c++ {
			s := v
			if c >= 7 {
				s = -8388608
			}
			o := f*27 + c*3
			raw[o] = byte(s)
			raw[o+1] = byte(s >> 8)
			raw[o+2] = byte(s >> 16)
		}
	}
	out, err := Mono16(raw, 6)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []int16{-6, 6, 32767} {
		if got := int16(binary.LittleEndian.Uint16(out[i*2:])); got != want {
			t.Fatalf("sample %d: %d != %d", i, got, want)
		}
	}
	if _, err = Mono16(raw[:28], 6); err == nil {
		t.Fatal("accepted partial frame")
	}
}
func TestUpsamplingChunkBoundary(t *testing.T) {
	p := []byte{0, 0, 0xe8, 3, 0xd0, 7}
	var a, b Upsampler
	whole, _ := a.Convert(p)
	x, _ := b.Convert(p[:2])
	y, _ := b.Convert(p[2:])
	x = append(x, y...)
	if string(whole) != string(x) {
		t.Fatal("conversion changed at chunk boundary")
	}
	if len(whole) != 24 {
		t.Fatal(len(whole))
	}
	for i := 0; i < len(whole); i += 4 {
		if string(whole[i:i+2]) != string(whole[i+2:i+4]) {
			t.Fatal("channels differ")
		}
	}
	if _, err := a.Convert([]byte{1}); err == nil {
		t.Fatal("accepted partial sample")
	}
}
func TestQueueInterruptOverflowAndPadding(t *testing.T) {
	q := NewQueue(8)
	if err := q.Add([]byte{1, 2, 3, 4}); err != nil {
		t.Fatal(err)
	}
	if err := q.Add(make([]byte, 8)); err == nil {
		t.Fatal("overflow accepted")
	}
	dst := make([]byte, 8)
	n, r := q.Take(dst)
	if n != 4 || r || dst[0] != 1 || dst[4] != 0 {
		t.Fatal(n, r, dst)
	}
	q.Add([]byte{5, 6, 7, 8})
	q.Clear()
	n, r = q.Take(dst)
	if n != 0 || !r || dst[0] != 0 {
		t.Fatal("stale audio after interruption")
	}
}
