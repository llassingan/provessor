package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/llassingan/provessor/internal/handler"
	"github.com/llassingan/provessor/internal/service"
)

const (
	csrfCookieName     = "csrf_token"
	csrfTokenSeparator = "."
)

func AuthMiddleware(authService *service.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("session_token")
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}

			userID, err := authService.ValidateSession(cookie.Value)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}

			ctx := context.WithValue(r.Context(), handler.UserIDKey, userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type rateLimitBucket struct {
	count int
	reset time.Time
}

type RateLimiter struct {
	limit  int
	window time.Duration
	now    func() time.Time

	mu        sync.Mutex
	buckets   map[string]rateLimitBucket
	lastPrune time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return newRateLimiter(limit, window, time.Now)
}

func newRateLimiter(limit int, window time.Duration, now func() time.Time) *RateLimiter {
	return &RateLimiter{
		limit:   limit,
		window:  window,
		now:     now,
		buckets: make(map[string]rateLimitBucket),
	}
}

func (l *RateLimiter) allow(key string) (bool, time.Duration) {
	if l == nil || l.limit < 1 || l.window <= 0 {
		return true, 0
	}

	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	l.pruneExpired(now)

	bucket := l.buckets[key]
	if bucket.reset.IsZero() || !now.Before(bucket.reset) {
		l.buckets[key] = rateLimitBucket{count: 1, reset: now.Add(l.window)}
		return true, 0
	}

	if bucket.count >= l.limit {
		return false, bucket.reset.Sub(now)
	}

	bucket.count++
	l.buckets[key] = bucket
	return true, 0
}

func (l *RateLimiter) pruneExpired(now time.Time) {
	if !l.lastPrune.IsZero() && now.Sub(l.lastPrune) < l.window {
		return
	}
	for key, bucket := range l.buckets {
		if !now.Before(bucket.reset) {
			delete(l.buckets, key)
		}
	}
	l.lastPrune = now
}

func RateLimitAPIByIP(limiter *RateLimiter) func(http.Handler) http.Handler {
	ipLimiter := RateLimitByIP(limiter)
	return func(next http.Handler) http.Handler {
		limited := ipLimiter(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api" {
				next.ServeHTTP(w, r)
				return
			}
			limited.ServeHTTP(w, r)
		})
	}
}

func RateLimitByIP(limiter *RateLimiter) func(http.Handler) http.Handler {
	return rateLimitByKey(limiter, func(r *http.Request) (string, bool) {
		ip := remoteIP(r.RemoteAddr)
		if ip == "" {
			return "", false
		}
		return ip, true
	})
}

func RateLimitByUser(limiter *RateLimiter) func(http.Handler) http.Handler {
	return rateLimitByKey(limiter, func(r *http.Request) (string, bool) {
		userID, ok := handler.UserIDFromContext(r.Context())
		if !ok {
			return "", false
		}
		return strconv.FormatInt(userID, 10), true
	})
}

func rateLimitByKey(limiter *RateLimiter, keyFunc func(*http.Request) (string, bool)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			key, ok := keyFunc(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			allowed, retryAfter := limiter.allow(key)
			if !allowed {
				if retryAfter > 0 {
					w.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
				}
				writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "rate limit exceeded"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func remoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
}

func CSRFMiddleware(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodOptions || isSafeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			cookie, err := r.Cookie(csrfCookieName)
			if err != nil || cookie.Value == "" {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid csrf token"})
				return
			}

			header := r.Header.Get("X-CSRF-Token")
			if !validCSRFPair(header, cookie.Value, secret) {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid csrf token"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func HandleCSRFToken(secret []byte, secureCookie bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, err := newCSRFToken(secret)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create csrf token"})
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     csrfCookieName,
			Value:    token,
			Path:     "/",
			HttpOnly: false,
			Secure:   secureCookie,
			SameSite: http.SameSiteStrictMode,
			MaxAge:   86400,
		})
		writeJSON(w, http.StatusOK, map[string]string{"csrf_token": token})
	}
}

func newCSRFToken(secret []byte) (string, error) {
	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	return signedCSRFToken(nonce, secret), nil
}

func signedCSRFToken(nonce string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(nonce))
	return nonce + csrfTokenSeparator + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func validCSRFPair(headerToken string, cookieToken string, secret []byte) bool {
	headerNonce, ok := validCSRFToken(headerToken, secret)
	if !ok {
		return false
	}
	cookieNonce, ok := validCSRFToken(cookieToken, secret)
	if !ok {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(headerNonce), []byte(cookieNonce)) == 1
}

func validCSRFToken(token string, secret []byte) (string, bool) {
	if token == "" || len(secret) == 0 {
		return "", false
	}
	nonce, sig, ok := strings.Cut(token, csrfTokenSeparator)
	if !ok || nonce == "" || sig == "" {
		return "", false
	}
	expected := signedCSRFToken(nonce, secret)
	if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		return "", false
	}
	return nonce, true
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}
