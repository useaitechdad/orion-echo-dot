package main

import (
	"context"
	"dotty/internal/audio"
	"dotty/internal/live"
	"encoding/binary"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type fakeSession struct {
	input                chan live.Message
	closed, ready        chan struct{}
	closeOnce, readyOnce sync.Once
	uploads              atomic.Int32
}

func newFakeSession() *fakeSession {
	return &fakeSession{input: make(chan live.Message, 8), closed: make(chan struct{}), ready: make(chan struct{})}
}
func (f *fakeSession) Receive() (live.Message, error) {
	select {
	case m := <-f.input:
		return m, nil
	case <-f.closed:
		return live.Message{}, errors.New("closed")
	}
}
func (f *fakeSession) Send(any) error     { f.readyOnce.Do(func() { close(f.ready) }); return nil }
func (f *fakeSession) Audio([]byte) error { f.uploads.Add(1); return nil }
func (f *fakeSession) StreamEnd() error   { return nil }
func (f *fakeSession) Ping() error        { return nil }
func (f *fakeSession) Close() error       { f.closeOnce.Do(func() { close(f.closed) }); return nil }
func message(t *testing.T, s string) live.Message {
	t.Helper()
	m, err := live.Decode([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSessionAcknowledgementTimeoutDoesNotUpload(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newFakeSession()
		mic := make(chan []byte, 1)
		done := make(chan error, 1)
		go func() {
			done <- serve(context.Background(), c, newPlayer(), &sessionState{wakeEnabled: true}, make(chan uint16), mic, make(chan error), make(chan string, 1))
		}()
		<-c.ready
		mic <- make([]byte, 1024)
		time.Sleep(16 * time.Second)
		synctest.Wait()
		if err := <-done; err == nil || !strings.Contains(err.Error(), "acknowledgement") {
			t.Fatalf("%v", err)
		}
		if c.uploads.Load() != 0 {
			t.Fatal("uploaded before acknowledgement")
		}
	})
}

func TestSessionMuteClosesDuringAcknowledgementAndBackpressure(t *testing.T) {
	for _, buffered := range []bool{false, true} {
		synctest.Test(t, func(t *testing.T) {
			c := newFakeSession()
			keys := make(chan uint16, 1)
			mic := make(chan []byte, 1)
			p := newPlayer()
			p.queue = audio.NewQueue(4)
			state := &sessionState{wakeEnabled: true}
			done := make(chan error, 1)
			go func() {
				done <- serve(context.Background(), c, p, state, keys, mic, make(chan error), make(chan string, 1))
			}()
			<-c.ready
			if buffered {
				c.input <- message(t, `{"serverContent":{"modelTurn":{"parts":[{"inlineData":{"mimeType":"audio/pcm;rate=24000","data":"AAAAAAAAAAA="}}]}}}`)
				synctest.Wait()
				if p.queue.Len() != 4 {
					t.Fatal("output was not queued under backpressure")
				}
			}
			keys <- privacyMuted
			synctest.Wait()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if !state.muted || p.queue.Len() != 0 || c.uploads.Load() != 0 {
				t.Fatal("mute failed under pending output")
			}
			select {
			case <-c.closed:
			default:
				t.Fatal("session not closed")
			}
		})
	}
}

func TestSessionResumesMicOnlyAfterAcknowledgement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		c := newFakeSession()
		mic := make(chan []byte, 1)
		done := make(chan error, 1)
		go func() {
			done <- serve(ctx, c, newPlayer(), &sessionState{wakeEnabled: true}, make(chan uint16), mic, make(chan error), make(chan string, 1))
		}()
		<-c.ready
		mic <- make([]byte, 1024)
		synctest.Wait()
		if c.uploads.Load() != 0 {
			t.Fatal("early mic")
		}
		c.input <- message(t, `{"serverContent":{"turnComplete":true}}`)
		synctest.Wait()
		mic <- make([]byte, 1024)
		synctest.Wait()
		if c.uploads.Load() != 1 {
			t.Fatal("mic did not reopen")
		}
		cancel()
		synctest.Wait()
		<-done
	})
}

func TestSustainedNoiseDoesNotKeepConversationOpen(t *testing.T) {
	start := time.Unix(0, 0)
	w := newConversationWindow(start)
	pcm := make([]byte, 1024)
	for i := 0; i < len(pcm); i += 2 {
		binary.LittleEndian.PutUint16(pcm[i:], 1000)
	}
	for i := 0; i < 1500; i++ {
		w.observe(start.Add(time.Duration(i)*32*time.Millisecond), false, pcm)
	}
	if !w.expired(start.Add(48 * time.Second)) {
		t.Fatal("constant noise kept session active")
	}
}
