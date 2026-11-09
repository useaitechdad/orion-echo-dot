package live

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestKeyFileParsing(t *testing.T) {
	for _, input := range []string{"example-key\n", "GEMINI_API_KEY=example-key", "GOOGLE_API_KEY='example-key'", "GEMINI_API_KEY=\"example-key\""} {
		key, err := ParseKey([]byte(input))
		if err != nil || key != "example-key" {
			t.Fatal("valid key file rejected")
		}
	}
	for _, input := range []string{"", "GEMINI_API_KEY=", "key\nsecond-line"} {
		if _, err := ParseKey([]byte(input)); err == nil {
			t.Fatal("invalid key file accepted")
		}
	}
}

func TestSetupAndSecretSeparation(t *testing.T) {
	b, _ := json.Marshal(Setup(Config{Model: "models/example-live", Key: "never-in-json", ResumeHandle: "resume-1"}))
	var m map[string]any
	json.Unmarshal(b, &m)
	s := m["setup"].(map[string]any)
	if s["model"] != "models/example-live" {
		t.Fatal(s["model"])
	}
	if strings.Contains(string(b), "never-in-json") {
		t.Fatal("key leaked into setup")
	}
	if !strings.Contains(string(b), `"disabled":false`) || !strings.Contains(string(b), `"handle":"resume-1"`) {
		t.Fatal(string(b))
	}
}
func TestServerLifecycleAndPCM(t *testing.T) {
	m, err := Decode([]byte(`{"serverContent":{"interrupted":true,"turnComplete":true,"modelTurn":{"parts":[{"inlineData":{"mimeType":"audio/pcm;rate=24000","data":"AQACAA=="}}]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if !m.ServerContent.Interrupted || !m.ServerContent.TurnComplete {
		t.Fatal(m)
	}
	p, err := m.ServerContent.ModelTurn.Parts[0].InlineData.PCM()
	if err != nil || string(p) != string([]byte{1, 0, 2, 0}) {
		t.Fatal(p, err)
	}
	for _, b := range []Blob{{"audio/pcm;rate=16000", "AQACAA=="}, {"audio/pcm;rate=24000", "AQ=="}, {"audio/pcm;rate=24000", "!"}} {
		if _, err := b.PCM(); err == nil {
			t.Fatal("accepted malformed/unsupported audio", b)
		}
	}
	if _, err := Decode([]byte(`{"error":{"code":403,"message":"denied"}}`)); err == nil {
		t.Fatal("ignored API failure")
	}
	if _, err := Decode([]byte(`not json`)); err == nil {
		t.Fatal("ignored malformed JSON")
	}
}
