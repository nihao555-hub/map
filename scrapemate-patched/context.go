package scrapemate

import (
	"context"

	"github.com/gosom/kit/logging"
)

// JobPusher enqueues a follow-up job while another job is still running
// (e.g. streaming PlaceJobs mid-scroll from a Maps feed).
type JobPusher func(ctx context.Context, job IJob) error

// GetLoggerFromContext returns a logger from the context or a default logger
func GetLoggerFromContext(ctx context.Context) logging.Logger {
	log, ok := ctx.Value(contextKey("log")).(logging.Logger)
	if !ok {
		return logging.Get()
	}

	return log
}

// ContextWithLogger returns a new context with the logger
func ContextWithLogger(ctx context.Context, logger logging.Logger) context.Context {
	return context.WithValue(ctx, contextKey("log"), logger)
}

// ContextWithJobPusher attaches a mid-job enqueue helper for BrowserActions.
func ContextWithJobPusher(ctx context.Context, push JobPusher) context.Context {
	if push == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey("jobPusher"), push)
}

// GetJobPusherFromContext returns the mid-job enqueue helper, or nil.
func GetJobPusherFromContext(ctx context.Context) JobPusher {
	push, _ := ctx.Value(contextKey("jobPusher")).(JobPusher)
	return push
}

type contextKey string
