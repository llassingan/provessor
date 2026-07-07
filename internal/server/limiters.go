package server

import "time"

type serverLimiters struct {
	globalAPI           *RateLimiter
	login               *RateLimiter
	signup              *RateLimiter
	credentialsCallback *RateLimiter
	userActions         *RateLimiter
}

func newServerLimiters() serverLimiters {
	return serverLimiters{
		globalAPI:           NewRateLimiter(300, time.Minute),
		login:               NewRateLimiter(5, time.Minute),
		signup:              NewRateLimiter(3, time.Minute),
		credentialsCallback: NewRateLimiter(10, time.Minute),
		userActions:         NewRateLimiter(20, time.Minute),
	}
}
