package live

import (
	"fmt"
	"strings"
)

// ParseKey accepts a bare key or a single conventional env-style assignment.
// It never evaluates shell text and never includes the secret in an error.
func ParseKey(data []byte) (string, error) {
	key := strings.TrimSpace(string(data))
	for _, prefix := range []string{"GEMINI_API_KEY=", "GOOGLE_API_KEY="} {
		if strings.HasPrefix(key, prefix) {
			key = strings.TrimSpace(strings.TrimPrefix(key, prefix))
			break
		}
	}
	if len(key) >= 2 && ((key[0] == '"' && key[len(key)-1] == '"') || (key[0] == '\'' && key[len(key)-1] == '\'')) {
		key = key[1 : len(key)-1]
	}
	if key == "" || strings.ContainsAny(key, "\r\n \t\x00") {
		return "", fmt.Errorf("key file must contain one API key or one GEMINI_API_KEY assignment")
	}
	return key, nil
}
