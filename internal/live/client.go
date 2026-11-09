// Package live implements Gemini's documented bidirectional audio protocol.
package live

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

const Endpoint = "wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent"
const DefaultModel = "gemini-3.8-live"

type Config struct {
	Model, Voice, Key, CAFile, DNS, ResumeHandle string
}

func Setup(c Config) map[string]any {
	model := strings.TrimPrefix(c.Model, "models/")
	generation := map[string]any{"responseModalities": []string{"AUDIO"}}
	if c.Voice != "" {
		generation["speechConfig"] = map[string]any{"voiceConfig": map[string]any{"prebuiltVoiceConfig": map[string]any{"voiceName": c.Voice}}}
	}
	resumption := map[string]any{}
	if c.ResumeHandle != "" {
		resumption["handle"] = c.ResumeHandle
	}
	return map[string]any{"setup": map[string]any{
		"model":                    "models/" + model,
		"generationConfig":         generation,
		"systemInstruction":        map[string]any{"parts": []map[string]string{{"text": "You are Orion, a witty, warm desk companion living in a repurposed Echo Dot. Respond naturally and briefly, usually one or two sentences. Speak plain conversational English unless asked otherwise. Do not use markdown, emojis, or read formatting aloud. You can hear speech but cannot see or control other devices. Never claim to have performed actions you cannot perform."}}},
		"realtimeInputConfig":      map[string]any{"automaticActivityDetection": map[string]any{"disabled": false, "silenceDurationMs": 700}},
		"sessionResumption":        resumption,
		"contextWindowCompression": map[string]any{"slidingWindow": map[string]any{}},
	}}
}

type Blob struct {
	MIMEType string `json:"mimeType"`
	Data     string `json:"data"`
}
type Message struct {
	SetupComplete *struct{} `json:"setupComplete"`
	ServerContent *struct {
		Interrupted        bool `json:"interrupted"`
		TurnComplete       bool `json:"turnComplete"`
		GenerationComplete bool `json:"generationComplete"`
		ModelTurn          *struct {
			Parts []struct {
				InlineData *Blob `json:"inlineData"`
			} `json:"parts"`
		} `json:"modelTurn"`
	} `json:"serverContent"`
	SessionResumptionUpdate *struct {
		NewHandle string `json:"newHandle"`
		Resumable bool   `json:"resumable"`
	} `json:"sessionResumptionUpdate"`
	GoAway *struct {
		TimeLeft string `json:"timeLeft"`
	} `json:"goAway"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func Decode(data []byte) (Message, error) {
	var m Message
	if err := json.Unmarshal(data, &m); err != nil {
		return m, fmt.Errorf("decode Gemini message: %w", err)
	}
	if m.Error != nil {
		return m, fmt.Errorf("Gemini API error %d: %s", m.Error.Code, m.Error.Message)
	}
	return m, nil
}

func (b Blob) PCM() ([]byte, error) {
	// A different output rate must be handled explicitly; playing it as 24 kHz
	// would alter pitch/speed and conceal a protocol change.
	if strings.ReplaceAll(b.MIMEType, " ", "") != "audio/pcm;rate=24000" {
		return nil, fmt.Errorf("unsupported output MIME type %q", b.MIMEType)
	}
	p, err := base64.StdEncoding.DecodeString(b.Data)
	if err != nil {
		return nil, fmt.Errorf("invalid audio base64: %w", err)
	}
	if len(p)%2 != 0 {
		return nil, fmt.Errorf("partial output PCM sample")
	}
	return p, nil
}
