package wake

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestNativeHelperSilenceResetAndShutdown(t *testing.T) {
	helper := os.Getenv("DOTTY_WAKE_TEST_HELPER")
	model := os.Getenv("DOTTY_WAKE_TEST_MODEL")
	if helper == "" || model == "" {
		t.Skip("set native helper/model paths for integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	threshold := 1e-34
	if value := os.Getenv("DOTTY_WAKE_TEST_THRESHOLD"); value != "" {
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			t.Fatal(err)
		}
		threshold = parsed
	}
	d, err := Open(ctx, helper, model, threshold)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for i := 0; i < 64; i++ {
		hit, err := d.Process(make([]byte, 1024))
		if err != nil || hit {
			t.Fatalf("silence hit=%t err=%v", hit, err)
		}
	}
	if err = d.Reset(); err != nil {
		t.Fatal(err)
	}
	if _, err = d.Process([]byte{1}); err == nil {
		t.Fatal("odd sample accepted")
	}
	d.Close()
	if _, err = d.Process(make([]byte, 1024)); err == nil {
		t.Fatal("closed helper did not fail closed")
	}
}
