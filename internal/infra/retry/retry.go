// Package retry shares failure classification between request and task retries.
package retry

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/cenkalti/backoff/v4"
)

type Policy struct {
	MaxAttempts                  int
	InitialInterval, MaxInterval time.Duration
}

type permanentError struct{ error }

func (e *permanentError) Unwrap() error { return e.error }
func Permanent(err error) error         { return &permanentError{err} }
func IsPermanent(err error) bool        { var target *permanentError; return errors.As(err, &target) }

type deferredError struct {
	error
	Until time.Time
}

func (e *deferredError) Unwrap() error { return e.error }
func NotBefore(err error) time.Time {
	if err == nil {
		return time.Time{}
	}
	var until time.Time
	var deferred *deferredError
	if errors.As(err, &deferred) {
		until = deferred.Until
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if candidate := NotBefore(child); candidate.After(until) {
				until = candidate
			}
		}
	} else if child := errors.Unwrap(err); child != nil {
		if candidate := NotBefore(child); candidate.After(until) {
			until = candidate
		}
	}
	return until
}

func Network(err error) bool {
	if IsPermanent(err) || errors.Is(err, context.Canceled) {
		return false
	}
	var n net.Error
	var dns *net.DNSError
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) || (errors.As(err, &dns) && dns.IsTemporary) || (errors.As(err, &n) && n.Timeout())
}

func Status(code int) bool {
	return code == 429 || code == 500 || code == 502 || code == 503 || code == 504
}

func HTTPError(err error, code int, header http.Header) error {
	if !Status(code) {
		return Permanent(err)
	}
	if code != 429 && code != 503 {
		return err
	}
	value := strings.TrimSpace(header.Get("Retry-After"))
	if value != "" && strings.Trim(value, "0123456789") == "" && len(strings.TrimLeft(value, "0")) > 18 {
		return &deferredError{err, time.Now().Add(time.Duration(1<<63 - 1))}
	}
	if seconds, parseErr := strconv.ParseInt(value, 10, 64); parseErr == nil && seconds >= 0 {
		// Saturate huge server intervals instead of overflowing into an immediate retry.
		if seconds > int64((1<<63-1)/time.Second) {
			return &deferredError{err, time.Now().Add(time.Duration(1<<63 - 1))}
		}
		return &deferredError{err, time.Now().Add(time.Duration(seconds) * time.Second)}
	}
	if until, parseErr := http.ParseTime(value); parseErr == nil {
		return &deferredError{err, until}
	}
	return err
}

type requestBackoff struct {
	*backoff.ExponentialBackOff
	until time.Time
}

func (b *requestBackoff) NextBackOff() time.Duration {
	return max(min(b.ExponentialBackOff.NextBackOff(), b.MaxInterval), time.Until(b.until))
}

func Do[T any](ctx context.Context, cfg Policy, operation func() (T, error)) (T, error) {
	b := &requestBackoff{ExponentialBackOff: backoff.NewExponentialBackOff()}
	b.InitialInterval, b.MaxInterval = cfg.InitialInterval, cfg.MaxInterval
	b.MaxElapsedTime = 0
	b.Multiplier = 2
	return backoff.RetryWithData(func() (T, error) {
		var zero T
		if err := ctx.Err(); err != nil {
			return zero, backoff.Permanent(err)
		}
		result, err := operation()
		if err == nil {
			return result, nil
		}
		if IsPermanent(err) {
			return result, backoff.Permanent(err)
		}
		b.until = NotBefore(err)
		if deadline, ok := ctx.Deadline(); ok && !b.until.IsZero() && !b.until.Before(deadline) {
			return result, backoff.Permanent(err)
		}
		return result, err
	}, backoff.WithContext(backoff.WithMaxRetries(b, uint64(max(cfg.MaxAttempts-1, 0))), ctx))
}

// Delay calculates the persisted worker schedule without sleeping a worker.
func Delay(cfg Policy, attempts int) time.Duration {
	delay := cfg.InitialInterval
	for i := 1; i < attempts && delay < cfg.MaxInterval; i++ {
		delay = min(delay*2, cfg.MaxInterval)
	}
	return delay
}

// Budget bounds a request operation even when its caller has no deadline.
func Budget(timeout time.Duration, cfg Policy) time.Duration {
	return time.Duration(cfg.MaxAttempts)*timeout + time.Duration(max(cfg.MaxAttempts-1, 0))*cfg.MaxInterval
}
