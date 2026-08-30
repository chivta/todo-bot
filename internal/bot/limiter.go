package bot

import (
	"context"

	"golang.org/x/time/rate"
)

// sendLimiter paces every outbound Telegram call. Workers run concurrently and
// each of them edits a status message as its job progresses, so without a shared
// limiter a few parallel jobs are enough to trip Telegram's global rate limit
// and get the bot temporarily blocked. This only shows up under load, which is
// why it is here from the start rather than added after the first outage.
type sendLimiter struct {
	limiter *rate.Limiter
}

func newSendLimiter(perSecond, burst int) *sendLimiter {
	return &sendLimiter{limiter: rate.NewLimiter(rate.Limit(perSecond), burst)}
}

// wait blocks until the call may proceed, or until ctx is done.
func (l *sendLimiter) wait(ctx context.Context) error {
	return l.limiter.Wait(ctx)
}
