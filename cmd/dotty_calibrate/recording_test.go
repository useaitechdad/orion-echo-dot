package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCancelledRecordingNeverPublishesOrLeavesPartial(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "clip.s16le")
	err := writeRecording(out, func(f *os.File) error { f.Write([]byte("private partial")); return errors.New("cancelled") })
	if err == nil {
		t.Fatal("cancel accepted")
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 0 {
		t.Fatal("partial recording retained")
	}
}
func TestRecordingPublishesPrivatelyWithoutOverwriting(t *testing.T) {
	out := filepath.Join(t.TempDir(), "clip.s16le")
	capture := func(f *os.File) error { _, err := f.Write([]byte("sample")); return err }
	if err := writeRecording(out, capture); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(out)
	if info.Mode().Perm() != 0600 {
		t.Fatal("recording permissions")
	}
	if err := writeRecording(out, capture); err == nil {
		t.Fatal("overwrote completed clip")
	}
	b, _ := os.ReadFile(out)
	if string(b) != "sample" {
		t.Fatal("existing recording modified")
	}
}
func TestHardwarePrivacyFailsClosed(t *testing.T) {
	for _, read := range []func() (bool, error){func() (bool, error) { return true, nil }, func() (bool, error) { return false, errors.New("invalid") }} {
		if requireUnmuted(read) == nil {
			t.Fatal("privacy bypass")
		}
	}
}
