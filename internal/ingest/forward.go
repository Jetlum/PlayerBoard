package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// ForwardSigned builds a performance-observed payload, signs it exactly as the real
// ScoreBoard/ScoreAlerts sender would (HMAC-SHA256 over "timestamp.body"), and posts it to
// this same process's webhook endpoint. Both the ClubBoard "record appearance" action and
// the dev simulate button play the role of "the club's system telling PlayerBoard about a
// newly played match" — they share this one code path into the real ingest pipeline.
func ForwardSigned(ctx context.Context, baseURL, secret, eventIDPrefix, athleteID, metric string, value int64) (status int, respBody []byte, err error) {
	body, err := json.Marshal(map[string]any{
		"athlete_id": athleteID,
		"metric":     metric,
		"value":      value,
	})
	if err != nil {
		return 0, nil, err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := HMACVerifier{Secret: []byte(secret)}.Sign(ts, body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/webhooks/scoreboard", bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Event-Id", fmt.Sprintf("%s-%d", eventIDPrefix, time.Now().UnixNano()))
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("X-Signature", sig)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out, nil
}
