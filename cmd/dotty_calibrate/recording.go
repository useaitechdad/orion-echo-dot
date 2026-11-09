package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func requireUnmuted(read func() (bool, error)) error {
	muted, err := read()
	if err != nil {
		return err
	}
	if muted {
		return fmt.Errorf("hardware privacy is on; calibration cancelled")
	}
	return nil
}

// Only the temporary file created by this invocation is removed on failure.
// Completed/existing recordings are never overwritten or removed.
func writeRecording(out string, capture func(*os.File) error) error {
	f, err := os.CreateTemp(filepath.Dir(out), ".orion-calibration-*.partial")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = capture(f); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// link is atomic and fails if destination exists, unlike rename overwrite.
	if err = os.Link(f.Name(), out); err != nil {
		return fmt.Errorf("publish recording without overwrite: %w", err)
	}
	return nil
}
