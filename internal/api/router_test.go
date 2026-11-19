package api

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ppclassvip/internal/auth"
	"ppclassvip/internal/config"
)

func testRouter(t *testing.T) http.Handler {
	t.Helper()
	tokens, err := auth.NewTokenManager(strings.Repeat("t", 32), "ppclassvip", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.App.Env = "test"
	cfg.HTTP.CORSOrigins = nil
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(Dependencies{
		Config: cfg,
		Logger: log,
		Tokens: tokens,
	})
}

func TestPing(t *testing.T) {
	router := testRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Request-ID") == "" {
		t.Fatal("missing request id")
	}
}

func TestMeRequiresToken(t *testing.T) {
	router := testRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestHealthWithoutDatabase(t *testing.T) {
	router := testRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}
