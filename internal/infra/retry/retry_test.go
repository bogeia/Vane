package retry

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestAttemptsPermanentAndCancellation(t *testing.T) {
	cfg := Policy{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: time.Millisecond}
	for _, permanent := range []bool{false, true} {
		calls := 0
		_, err := Do(t.Context(), cfg, func() (int, error) {
			calls++
			err := fmt.Errorf("failure")
			if permanent {
				err = Permanent(err)
			}
			return 0, err
		})
		want := 3
		if permanent {
			want = 1
		}
		if calls != want || err == nil || IsPermanent(err) != permanent {
			t.Fatalf("calls=%d err=%v permanent=%v", calls, err, IsPermanent(err))
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	calls := 0
	_, err := Do(ctx, cfg, func() (int, error) { calls++; return 0, fmt.Errorf("failure") })
	if calls != 0 || !errors.Is(err, context.Canceled) || IsPermanent(err) {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	_, err = Do(ctx, cfg, func() (int, error) { cancel(); return 0, fmt.Errorf("failure") })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestRetryAfterHonorsServerAndBudget(t *testing.T) {
	for _, value := range []string{"60", time.Now().Add(time.Minute).UTC().Format(http.TimeFormat), "9223372036854775807", "999999999999999999999999999999"} {
		start := time.Now()
		err := HTTPError(fmt.Errorf("busy"), 503, http.Header{"Retry-After": []string{value}})
		if !NotBefore(err).After(start.Add(58 * time.Second)) {
			t.Fatalf("lost delay for %q", value)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		calls := 0
		_, got := Do(ctx, Policy{MaxAttempts: 3, InitialInterval: time.Millisecond, MaxInterval: time.Millisecond}, func() (int, error) { calls++; return 0, err })
		cancel()
		if calls != 1 || IsPermanent(got) || NotBefore(got) != NotBefore(err) {
			t.Fatalf("calls=%d err=%v", calls, got)
		}
	}
	// A short server interval must be honored even above the local cap.
	start := time.Now()
	until := start.Add(15 * time.Millisecond)
	calls := 0
	_, err := Do(t.Context(), Policy{MaxAttempts: 2, InitialInterval: time.Millisecond, MaxInterval: time.Millisecond}, func() (int, error) {
		calls++
		if calls == 1 {
			return 0, &deferredError{fmt.Errorf("busy"), until}
		}
		if time.Now().Before(until) {
			t.Fatal("retried early")
		}
		return 1, nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestClassificationAndWorkerDelay(t *testing.T) {
	for _, code := range []int{401, 403, 404, 501} {
		if !IsPermanent(HTTPError(fmt.Errorf("status"), code, nil)) {
			t.Fatal(code)
		}
	}
	for _, code := range []int{429, 500, 502, 503, 504} {
		if IsPermanent(HTTPError(fmt.Errorf("status"), code, nil)) {
			t.Fatal(code)
		}
	}
	if Network(context.Canceled) || !Network(context.DeadlineExceeded) {
		t.Fatal("context classification")
	}
	cfg := Policy{MaxAttempts: 5, InitialInterval: 2 * time.Minute, MaxInterval: 16 * time.Minute}
	for attempt, want := range []time.Duration{2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 16 * time.Minute} {
		if got := Delay(cfg, attempt+1); got != want {
			t.Fatal(got)
		}
	}
}

func TestNotBeforeUsesLatestJoinedFailure(t *testing.T) {
	early := time.Now().Add(time.Minute)
	late := early.Add(time.Hour)
	err := fmt.Errorf("wrapped: %w", errors.Join(&deferredError{fmt.Errorf("first"), early}, fmt.Errorf("nested: %w", &deferredError{fmt.Errorf("second"), late})))
	if got := NotBefore(err); !got.Equal(late) {
		t.Fatalf("got %v want %v", got, late)
	}
}
