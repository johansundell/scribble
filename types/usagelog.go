package types

import (
	"encoding/json"
	"time"
)

type UsageLog struct {
	ID        int       `json:"id"`
	Status    int       `json:"status"`
	Method    string    `json:"method"`
	Error     string    `json:"error"`
	Endpoint  string    `json:"endpoint"`
	CreatedAt time.Time `json:"created_at"`
	Response  RawJSON   `json:"response"`
	Request   RawJSON   `json:"request"`
}

// RawJSON holds a logged request or response body. Bodies that are valid
// JSON are written into the output as they are; anything else (plain text,
// truncated or binary data) is written as a JSON string, so one bad body
// can't make the whole GET /logs response unencodable.
type RawJSON string

func (r RawJSON) MarshalJSON() ([]byte, error) {
	if len(r) == 0 {
		return []byte("null"), nil
	}
	if json.Valid([]byte(r)) {
		return []byte(r), nil
	}
	// Invalid UTF-8 becomes U+FFFD, so the result is always valid JSON.
	return json.Marshal(string(r))
}

func (r *RawJSON) UnmarshalJSON(data []byte) error {
	*r = RawJSON(data)
	return nil
}
