package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"alexandria/internal/auth"
	"alexandria/internal/config"
)

func TestAuthProtectsAPIAndAllowsLogin(t *testing.T) {
	authService, err := auth.New(config.AuthConfig{
		Enabled:  true,
		Username: "admin",
		Password: "secret",
		Secret:   "test-secret",
	})
	if err != nil {
		t.Fatalf("create auth service: %v", err)
	}
	router := NewRouter(Services{Auth: authService})

	healthRec := httptest.NewRecorder()
	router.ServeHTTP(healthRec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if healthRec.Code != http.StatusOK {
		t.Fatalf("expected health status 200, got %d", healthRec.Code)
	}

	protectedRec := httptest.NewRecorder()
	router.ServeHTTP(protectedRec, httptest.NewRequest(http.MethodGet, "/api/unknown", nil))
	if protectedRec.Code != http.StatusUnauthorized {
		t.Fatalf("expected protected status 401, got %d", protectedRec.Code)
	}

	loginRec := httptest.NewRecorder()
	router.ServeHTTP(
		loginRec,
		httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"admin","password":"secret"}`)),
	)
	if loginRec.Code != http.StatusOK {
		t.Fatalf("expected login status 200, got %d: %s", loginRec.Code, loginRec.Body.String())
	}
	cookies := loginRec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected session cookie")
	}

	statusReq := httptest.NewRequest(http.MethodGet, "/api/auth/status", nil)
	statusReq.AddCookie(cookies[0])
	statusRec := httptest.NewRecorder()
	router.ServeHTTP(statusRec, statusReq)
	if statusRec.Code != http.StatusOK {
		t.Fatalf("expected status endpoint 200, got %d", statusRec.Code)
	}
	if !strings.Contains(statusRec.Body.String(), `"authenticated":true`) {
		t.Fatalf("expected authenticated status, got %s", statusRec.Body.String())
	}
}
