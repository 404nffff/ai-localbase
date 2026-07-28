package service

import (
	"context"
	"errors"
	"time"
)

func retryWithBackoff(ctx context.Context, attempts int, baseDelay time.Duration, fn func() error) error {
	if attempts <= 0 {
		attempts = 1
	}
	if baseDelay <= 0 {
		baseDelay = 150 * time.Millisecond
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		if ctx != nil {
			if err := ctx.Err(); err != nil {
				if lastErr != nil {
					return errors.Join(lastErr, err)
				}
				return err
			}
		}

		if err := fn(); err == nil {
			return nil
		} else {
			lastErr = err
			var stopper interface{ StopRetry() bool }
			if errors.As(err, &stopper) && stopper.StopRetry() {
				return errors.Unwrap(err)
			}
		}

		if i == attempts-1 {
			break
		}

		delay := baseDelay * time.Duration(1<<i)
		if ctx == nil {
			time.Sleep(delay)
			continue
		}

		select {
		case <-time.After(delay):
		case <-ctx.Done():
			if lastErr != nil {
				return errors.Join(lastErr, ctx.Err())
			}
			return ctx.Err()
		}
	}

	return lastErr
}
