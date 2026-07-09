package ingest_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jetlum/playerboard/internal/ingest"
	"github.com/jetlum/playerboard/internal/platform/db"
)

// Set TEST_DATABASE_URL to run this against a real Postgres, e.g.:
//   TEST_DATABASE_URL=postgres://player:player@localhost:5544/playerboard?sslmode=disable go test ./internal/ingest -run Dedupe -v
func TestWebhookDedupe(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping DB integration test")
	}
	if err := db.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	ctx := context.Background()
	pool, err := db.Pool(ctx, dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	secret := []byte("dev-webhook-secret-change-me")
	h := ingest.NewHandler(pool, ingest.HMACVerifier{Secret: secret})
	r := chi.NewRouter()
	r.Route("/api/v1", h.Routes)
	srv := httptest.NewServer(r)
	defer srv.Close()

	athleteID := "11111111-1111-1111-1111-111111111111"
	eventID := fmt.Sprintf("itest-%d", time.Now().UnixNano())
	body := fmt.Sprintf(`{"athlete_id":%q,"metric":"appearances","value":21}`, athleteID)

	post := func() int {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		sig := ingest.HMACVerifier{Secret: secret}.Sign(ts, []byte(body))
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/webhooks/scoreboard", strings.NewReader(body))
		req.Header.Set("X-Event-Id", eventID)
		req.Header.Set("X-Timestamp", ts)
		req.Header.Set("X-Signature", sig)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	if got := post(); got != http.StatusAccepted {
		t.Fatalf("first post = %d, want 202", got)
	}
	if got := post(); got != http.StatusOK {
		t.Fatalf("duplicate post = %d, want 200 (no-op)", got)
	}

	var inbound, outbox int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM inbound_event WHERE source_event_id=$1`, eventID).Scan(&inbound); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM outbox WHERE payload->>'source_event_id'=$1`, eventID).Scan(&outbox); err != nil {
		t.Fatal(err)
	}
	if inbound != 1 {
		t.Errorf("inbound_event rows = %d, want 1", inbound)
	}
	if outbox != 1 {
		t.Errorf("outbox rows = %d, want 1 (duplicate must not re-emit)", outbox)
	}

	// Tampered signature must be rejected.
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/webhooks/scoreboard", strings.NewReader(body))
	req.Header.Set("X-Event-Id", eventID+"-bad")
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("X-Signature", "bm90LXZhbGlk")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad signature = %d, want 401", resp.StatusCode)
	}
}
