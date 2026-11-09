package main

import "testing"

func TestHalfDuplexGate(t *testing.T) {
	g := gateState{}
	if !g.allowMic(false) {
		t.Fatal("idle mic closed")
	}
	g.serverSpeaking = true
	if g.allowMic(false) {
		t.Fatal("model audio leaked to mic")
	}
	g.boundary()
	if g.allowMic(true) {
		t.Fatal("turnComplete reopened mic before speaker drained")
	}
	if !g.allowMic(false) {
		t.Fatal("drained mic stayed closed")
	}
	g.serverSpeaking = true
	g.interrupt()
	if !g.suppressOutput || !g.allowMic(false) || g.allowMic(true) {
		t.Fatal("interrupt gate")
	}
	g.boundary()
	if g.suppressOutput {
		t.Fatal("next response still suppressed")
	}
}

func TestIdleActionDoesNotSuppressNextAnswer(t *testing.T) {
	g := gateState{}
	g.interrupt()
	if g.suppressOutput {
		t.Fatal("idle action suppressed the next response")
	}
	g.serverSpeaking = true
	g.interrupt()
	g.interrupt()
	if !g.suppressOutput {
		t.Fatal("repeated action allowed stale response")
	}
	g.boundary()
	if g.suppressOutput {
		t.Fatal("boundary did not release output")
	}
}
