package gateway

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/distributed-transcoder/internal/infra"
	"github.com/golang-jwt/jwt/v5"
)

type RateLimiter struct {
	state        infra.StateStore
	limitPerIP   int
	limitPerUser int
	jwtSecret    string
}

func NewRateLimiter(state infra.StateStore, limitPerIP, limitPerUser int, jwtSecret string) *RateLimiter {
	return &RateLimiter{
		state:        state,
		limitPerIP:   limitPerIP,
		limitPerUser: limitPerUser,
		jwtSecret:    jwtSecret,
	}
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getIP(r)
		
		// 1. Check IP limit (global upload endpoint protection)
		if r.URL.Path == "/api/jobs/upload-session" {
			key := fmt.Sprintf("ratelimit:ip:%s", ip)
			count, err := rl.state.IncrRateLimit(r.Context(), key, 60)
			if err != nil {
				// Fail open on Redis error
			} else if int(count) > rl.limitPerIP {
				http.Error(w, "Too many requests from this IP", http.StatusTooManyRequests)
				return
			}
		}

		// 2. Check User/JWT limit for data APIs
		if strings.HasPrefix(r.URL.Path, "/api/jobs/") && r.URL.Path != "/api/jobs/upload-session" {
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
				tokenStr := authHeader[7:]
				token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
					return []byte(rl.jwtSecret), nil
				})
				if err == nil && token.Valid {
					if claims, ok := token.Claims.(jwt.MapClaims); ok {
						jobID := ""
						if sub, ok := claims["sub"].(string); ok {
							jobID = sub
						} else if jid, ok := claims["job_id"].(string); ok {
							jobID = jid
						}
						if jobID != "" {
							key := fmt.Sprintf("ratelimit:user:%s", jobID)
							count, err := rl.state.IncrRateLimit(r.Context(), key, 60)
							if err == nil && int(count) > rl.limitPerUser {
								http.Error(w, "Too many requests for this job", http.StatusTooManyRequests)
								return
							}
						}
					}
				}
			}
		}

		next.ServeHTTP(w, r)
	})
}

// getIP extracts the real client IP (handles load balancers).
func getIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
