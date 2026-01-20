package e2e

import (
	"encoding/json"
)

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
