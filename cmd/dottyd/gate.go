package main

// Gate separates response completion from actual speaker drain. The acoustic
// tail is accounted for by player.busy(). After Action, stale server output is
// ignored until an interrupted/turnComplete boundary arrives.
type gateState struct{ serverSpeaking, suppressOutput, streamOpen bool }

func (g *gateState) allowMic(speakerBusy bool) bool { return !g.serverSpeaking && !speakerBusy }
func (g *gateState) interrupt() {
	// An idle Action press must not discard the next, unrelated answer.
	g.suppressOutput = g.suppressOutput || g.serverSpeaking
	g.serverSpeaking = false
}
func (g *gateState) boundary() { g.suppressOutput = false; g.serverSpeaking = false }
