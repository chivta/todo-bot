package worker

import (
	"sync"

	"golang.org/x/time/rate"
)

// secondsPerHour converts the per-hour budget in the config into the per-second
// refill rate the limiter wants.
const secondsPerHour = 3600

// userBuckets is a token bucket per user. A shared bucket would let one heavy
// user consume everyone else's budget, which is the whole thing this prevents.
type userBuckets struct {
	mu      sync.Mutex
	buckets map[int64]*rate.Limiter
	perHour int
	burst   int
}

func newUserBuckets(perHour, burst int) *userBuckets {
	return &userBuckets{
		buckets: make(map[int64]*rate.Limiter),
		perHour: perHour,
		burst:   burst,
	}
}

// allow reports whether a user may submit right now, spending a token if so.
func (b *userBuckets) allow(userID int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	limiter, ok := b.buckets[userID]
	if !ok {
		limiter = rate.NewLimiter(rate.Limit(float64(b.perHour)/secondsPerHour), b.burst)
		b.buckets[userID] = limiter
	}

	return limiter.Allow()
}
