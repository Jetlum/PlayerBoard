// Command api is the PlayerBoard HTTP gateway: auth, contract reads, signed webhook ingest,
// the outbox relay, and the WebSocket fan-out for realtime milestone events.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/jetlum/playerboard/web"

	"github.com/jetlum/playerboard/internal/auth"
	"github.com/jetlum/playerboard/internal/club"
	"github.com/jetlum/playerboard/internal/contract"
	"github.com/jetlum/playerboard/internal/events"
	"github.com/jetlum/playerboard/internal/ingest"
	"github.com/jetlum/playerboard/internal/milestone"
	"github.com/jetlum/playerboard/internal/platform/bus"
	"github.com/jetlum/playerboard/internal/platform/config"
	"github.com/jetlum/playerboard/internal/platform/db"
	"github.com/jetlum/playerboard/internal/platform/httpx"
	logpkg "github.com/jetlum/playerboard/internal/platform/log"
	"github.com/jetlum/playerboard/internal/realtime"
)

func main() {
	// Convenience subcommand: `api mint-token <athlete_id> [role]` prints a dev JWT.
	if len(os.Args) > 1 && os.Args[1] == "mint-token" {
		mintToken(os.Args[2:])
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	log := logpkg.New("api")
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	log.Info("migrations applied")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Pool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("db pool: %w", err)
	}
	defer pool.Close()

	b, err := bus.Connect(cfg.NATSURL)
	if err != nil {
		return fmt.Errorf("bus: %w", err)
	}
	defer b.Close()

	verifier, err := buildVerifier(cfg)
	if err != nil {
		return fmt.Errorf("verifier: %w", err)
	}

	hub := realtime.NewHub()
	clubHub := realtime.NewClubHub()

	// Fan every milestone event out two ways: to the specific player's own board (scoped by
	// athlete_id, unspoofable) and to every connected ClubBoard viewer (broadcast, so the
	// club sees "bonus fired for player X" the same instant the player does).
	if err := b.Subscribe(bus.SubjectMilestone, "api-ws", func(_ string, data []byte) error {
		var evt events.MilestoneChanged
		if err := json.Unmarshal(data, &evt); err != nil {
			return nil
		}
		if aid, err := uuid.Parse(evt.AthleteID); err == nil {
			hub.Push(aid, data)
		}
		clubHub.Broadcast(data)
		return nil
	}); err != nil {
		return fmt.Errorf("subscribe milestone: %w", err)
	}

	// Transactional-outbox relay.
	relay := ingest.NewRelay(pool, b)
	go relay.Run(ctx)

	// Wiring.
	selfURL := "http://127.0.0.1:" + cfg.Port
	contractH := contract.NewHandler(contract.NewService(contract.NewRepo(pool)))
	milestoneH := milestone.NewReadHandler(pool)
	ingestH := ingest.NewHandler(pool, verifier)
	wsH := realtime.NewHandler(hub)
	clubH := club.NewHandler(pool, cfg.WebhookSecret, selfURL)
	clubWSH := realtime.NewClubStreamHandler(clubHub)

	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		if err := pool.Ping(req.Context()); err != nil {
			httpx.Error(w, http.StatusServiceUnavailable, "db not ready")
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	r.Route("/api/v1", func(r chi.Router) {
		// Signed webhook: authenticity from the signature, no user JWT.
		ingestH.Routes(r)

		// Club console ("ScoreBoard" side): roster + "record appearance" + broadcast feed.
		// No JWT here — see internal/club package doc for the deferred-auth note.
		r.Route("/club", func(r chi.Router) {
			clubH.Routes(r)
			r.Get("/stream", clubWSH.Stream)
		})

		// Everything under /me is JWT-authenticated; athlete_id comes from the token.
		r.Group(func(r chi.Router) {
			r.Use(auth.Middleware(cfg.JWTSecret))
			r.Route("/me", func(r chi.Router) {
				contractH.Routes(r)
				milestoneH.Routes(r)
				r.Get("/stream", wsH.Stream)
			})
		})

		if cfg.DevMode {
			r.Post("/dev/token", devTokenHandler(cfg.JWTSecret))
			r.Post("/dev/simulate", devSimulateHandler(cfg, selfURL))
		}
	})

	// Dev dashboard (static, same-origin) so the realtime demo has a UI.
	if cfg.DevMode {
		r.Handle("/*", http.FileServer(http.FS(web.FS)))
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	log.Info("api listening", "port", cfg.Port, "signature_scheme", cfg.SignatureSchme)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Info("api stopped")
	return nil
}

func buildVerifier(cfg config.Config) (ingest.Verifier, error) {
	switch cfg.SignatureSchme {
	case "rsa":
		pub, err := ingest.ParseRSAPublicKey([]byte(cfg.RSAPublicKey))
		if err != nil {
			return nil, err
		}
		return ingest.RSAVerifier{Pub: pub}, nil
	default:
		return ingest.HMACVerifier{Secret: []byte(cfg.WebhookSecret)}, nil
	}
}

func devTokenHandler(secret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		athleteID := r.URL.Query().Get("athlete_id")
		if athleteID == "" {
			athleteID = "11111111-1111-1111-1111-111111111111" // seeded "Everton"
		}
		role := r.URL.Query().Get("role")
		if role == "" {
			role = "athlete"
		}
		tok, err := auth.Mint(secret, athleteID, role, 24*time.Hour)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "mint failed")
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]string{"token": tok, "athlete_id": athleteID, "role": role})
	}
}

// devSimulateHandler is a raw curl/dev convenience: it signs a webhook server-side and posts
// it through the real ingest endpoint for an arbitrary value. The ClubBoard "record
// appearance" action (internal/club) is the production-shaped equivalent used by the UI.
func devSimulateHandler(cfg config.Config, selfURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.SignatureSchme != "hmac" {
			httpx.Error(w, http.StatusBadRequest, "simulate supports the hmac scheme only")
			return
		}
		value, err := strconv.Atoi(r.URL.Query().Get("value"))
		if err != nil || value < 0 {
			httpx.Error(w, http.StatusBadRequest, "value must be a non-negative integer")
			return
		}
		athleteID := r.URL.Query().Get("athlete_id")
		if athleteID == "" {
			athleteID = "11111111-1111-1111-1111-111111111111"
		}

		status, body, err := ingest.ForwardSigned(r.Context(), selfURL, cfg.WebhookSecret,
			"sim", athleteID, "appearances", int64(value))
		if err != nil {
			httpx.Error(w, http.StatusBadGateway, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}
}

func mintToken(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: api mint-token <athlete_id> [role]")
		os.Exit(2)
	}
	secret := config.MustSecret()
	role := "athlete"
	if len(args) > 1 {
		role = args[1]
	}
	tok, err := auth.Mint(secret, args[0], role, 24*time.Hour)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mint failed:", err)
		os.Exit(1)
	}
	fmt.Println(tok)
}
