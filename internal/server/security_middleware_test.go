package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	apphandler "github.com/llassingan/provessor/internal/handler"
)

var testCSRFSecret = []byte("0123456789abcdef0123456789abcdef")

func TestRateLimitLoginReturnsTooManyRequestsAfterThreshold(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	limiter := newRateLimiter(2, time.Minute, func() time.Time { return now })
	handler := RateLimitByIP(limiter, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	}))

	for i := 0; i < 2; i++ {
		res := performMiddlewareRequest(handler, http.MethodPost, "/api/auth/login", "203.0.113.10:1234", nil)
		if res.Code != http.StatusOK {
			t.Fatalf("request %d expected 200, got %d", i+1, res.Code)
		}
	}

	res := performMiddlewareRequest(handler, http.MethodPost, "/api/auth/login", "203.0.113.10:1234", nil)
	if res.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", res.Code)
	}
	if res.Header().Get("Retry-After") != "60" {
		t.Fatalf("expected Retry-After 60, got %q", res.Header().Get("Retry-After"))
	}
	var body map[string]string
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["error"] != "rate limit exceeded" {
		t.Fatalf("unexpected error body: %#v", body)
	}
}

func TestRateLimitCallbackUsesRemoteAddrAndIgnoresForwardedHeaders(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	limiter := newRateLimiter(1, time.Minute, func() time.Time { return now })
	handler := RateLimitByIP(limiter, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	res := performMiddlewareRequest(handler, http.MethodPost, "/api/vps/1/credentials", "203.0.113.20:1000", map[string]string{"X-Forwarded-For": "198.51.100.1"})
	if res.Code != http.StatusNoContent {
		t.Fatalf("expected first callback 204, got %d", res.Code)
	}

	res = performMiddlewareRequest(handler, http.MethodPost, "/api/vps/1/credentials", "203.0.113.20:1000", map[string]string{"X-Forwarded-For": "198.51.100.2", "X-Real-IP": "198.51.100.3", "Forwarded": "for=198.51.100.4"})
	if res.Code != http.StatusTooManyRequests {
		t.Fatalf("expected same RemoteAddr to be throttled despite changed proxy headers, got %d", res.Code)
	}

	res = performMiddlewareRequest(handler, http.MethodPost, "/api/vps/1/credentials", "203.0.113.21:1000", map[string]string{"X-Forwarded-For": "203.0.113.20"})
	if res.Code != http.StatusNoContent {
		t.Fatalf("expected different RemoteAddr to have separate bucket, got %d", res.Code)
	}
}

func TestRateLimitBucketsRecoverAfterWindow(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	limiter := newRateLimiter(1, time.Minute, func() time.Time { return now })
	handler := RateLimitByIP(limiter, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	res := performMiddlewareRequest(handler, http.MethodPost, "/api/auth/login", "203.0.113.30:1000", nil)
	if res.Code != http.StatusNoContent {
		t.Fatalf("expected first request 204, got %d", res.Code)
	}
	res = performMiddlewareRequest(handler, http.MethodPost, "/api/auth/login", "203.0.113.30:1000", nil)
	if res.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second request 429, got %d", res.Code)
	}

	now = now.Add(time.Minute)
	res = performMiddlewareRequest(handler, http.MethodPost, "/api/auth/login", "203.0.113.30:1000", nil)
	if res.Code != http.StatusNoContent {
		t.Fatalf("expected recovered bucket 204, got %d", res.Code)
	}
}

func TestRateLimitSkipsOptions(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	limiter := newRateLimiter(1, time.Minute, func() time.Time { return now })
	handler := RateLimitByIP(limiter, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for i := 0; i < 3; i++ {
		res := performMiddlewareRequest(handler, http.MethodOptions, "/api/auth/login", "203.0.113.40:1000", nil)
		if res.Code != http.StatusNoContent {
			t.Fatalf("OPTIONS request %d expected 204, got %d", i+1, res.Code)
		}
	}
	res := performMiddlewareRequest(handler, http.MethodPost, "/api/auth/login", "203.0.113.40:1000", nil)
	if res.Code != http.StatusNoContent {
		t.Fatalf("expected first counted POST 204, got %d", res.Code)
	}
}

func TestRateLimitAPIByIPOnlyCountsAPIRoutes(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	limiter := newRateLimiter(1, time.Minute, func() time.Time { return now })
	handler := RateLimitAPIByIP(limiter, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	res := performMiddlewareRequest(handler, http.MethodGet, "/api/health", "203.0.113.50:1000", nil)
	if res.Code != http.StatusNoContent {
		t.Fatalf("expected first API request 204, got %d", res.Code)
	}
	res = performMiddlewareRequest(handler, http.MethodGet, "/api/health", "203.0.113.50:1000", nil)
	if res.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second API request 429, got %d", res.Code)
	}

	for i := 0; i < 2; i++ {
		res = performMiddlewareRequest(handler, http.MethodGet, "/assets/app.js", "203.0.113.50:1000", nil)
		if res.Code != http.StatusNoContent {
			t.Fatalf("non-API request %d expected 204, got %d", i+1, res.Code)
		}
	}
}

func TestRateLimitUserActionsDoNotAffectUnrelatedUsers(t *testing.T) {
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	limiter := newRateLimiter(1, time.Minute, func() time.Time { return now })
	handler := RateLimitByUser(limiter, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	res := performUserRequest(handler, 10)
	if res.Code != http.StatusNoContent {
		t.Fatalf("expected first user request 204, got %d", res.Code)
	}
	res = performUserRequest(handler, 10)
	if res.Code != http.StatusTooManyRequests {
		t.Fatalf("expected same user throttled, got %d", res.Code)
	}
	res = performUserRequest(handler, 11)
	if res.Code != http.StatusNoContent {
		t.Fatalf("expected unrelated user separate bucket, got %d", res.Code)
	}
}

func TestCSRFTokenEndpointSetsReadableCookieAndJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/auth/csrf", nil)
	res := httptest.NewRecorder()

	HandleCSRFToken(testCSRFSecret, true)(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}

	var body map[string]string
	if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["csrf_token"] == "" {
		t.Fatal("expected csrf_token in response")
	}

	cookies := res.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie, got %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != csrfCookieName || cookie.Value != body["csrf_token"] {
		t.Fatalf("cookie/token mismatch: %#v body=%q", cookie, body["csrf_token"])
	}
	if cookie.HttpOnly {
		t.Fatal("csrf cookie must be readable by frontend")
	}
	if !cookie.Secure {
		t.Fatal("expected production CSRF cookie to be Secure")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("expected Strict SameSite, got %v", cookie.SameSite)
	}
	if _, ok := validCSRFToken(body["csrf_token"], testCSRFSecret); !ok {
		t.Fatal("expected endpoint to return a signed CSRF token")
	}
}

func TestCSRFUnsafeRequestRequiresMatchingHeaderAndCookie(t *testing.T) {
	handler := CSRFMiddleware(testCSRFSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	validToken := signedCSRFToken("nonce", testCSRFSecret)

	res := performCSRFRequest(handler, http.MethodPost, "", "")
	if res.Code != http.StatusForbidden {
		t.Fatalf("expected missing token 403, got %d", res.Code)
	}

	res = performCSRFRequest(handler, http.MethodPost, validToken, signedCSRFToken("other-nonce", testCSRFSecret))
	if res.Code != http.StatusForbidden {
		t.Fatalf("expected mismatched token 403, got %d", res.Code)
	}

	res = performCSRFRequest(handler, http.MethodPost, validToken, strings.Replace(validToken, "nonce", "tampered", 1))
	if res.Code != http.StatusForbidden {
		t.Fatalf("expected tampered token 403, got %d", res.Code)
	}

	res = performCSRFRequest(handler, http.MethodPost, validToken, validToken)
	if res.Code != http.StatusNoContent {
		t.Fatalf("expected valid token 204, got %d", res.Code)
	}
}

func TestCSRFSafeMethodsAndOptionsDoNotRequireToken(t *testing.T) {
	handler := CSRFMiddleware(testCSRFSecret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		res := performCSRFRequest(handler, method, "", "")
		if res.Code != http.StatusNoContent {
			t.Fatalf("%s expected 204 without CSRF token, got %d", method, res.Code)
		}
	}
}

func TestCredentialsCallbackLimiterDoesNotRequireCSRFToken(t *testing.T) {
	router := chi.NewRouter()
	router.With(RateLimitByIP(newRateLimiter(10, time.Minute, time.Now), nil)).Post("/api/vps/{id}/credentials", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/vps/1/credentials", strings.NewReader("{}"))
	req.RemoteAddr = "203.0.113.60:1000"
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected callback limiter chain to allow request without CSRF token, got %d", res.Code)
	}
}

func performMiddlewareRequest(handler http.Handler, method, path, remoteAddr string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	req.RemoteAddr = remoteAddr
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}

func performUserRequest(handler http.Handler, userID int64) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/vps", strings.NewReader("{}"))
	req = req.WithContext(context.WithValue(req.Context(), apphandler.UserIDKey, userID))
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}

func performCSRFRequest(handler http.Handler, method, cookieToken, headerToken string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/vps", strings.NewReader("{}"))
	if cookieToken != "" {
		req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: cookieToken})
	}
	if headerToken != "" {
		req.Header.Set("X-CSRF-Token", headerToken)
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}
