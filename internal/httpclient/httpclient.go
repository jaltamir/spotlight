package httpclient

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/jaltamir/spotlight/internal/log"
)

// retryStatuses are the HTTP status codes that trigger a retry.
var retryStatuses = map[int]bool{
	http.StatusTooManyRequests:     true,
	http.StatusInternalServerError: true,
	http.StatusBadGateway:          true,
	http.StatusServiceUnavailable:  true,
	http.StatusGatewayTimeout:      true,
}

// maxRetryAfter is the cap on Retry-After header values. If the server requests
// a wait longer than this, RetryTransport returns the error immediately.
const maxRetryAfter = 30 * time.Second

// RetryTransport is an http.RoundTripper that retries transient failures with
// exponential backoff. It retries on status codes 429, 500, 502, 503, 504.
type RetryTransport struct {
	Base      http.RoundTripper
	BaseDelay time.Duration // base delay for exponential backoff; default 1s
	MaxRetry  int           // maximum number of retries; default 3
}

// NewClient returns an *http.Client with RetryTransport configured.
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &RetryTransport{
			Base:      http.DefaultTransport,
			BaseDelay: time.Second,
			MaxRetry:  3,
		},
	}
}

func (t *RetryTransport) base() http.RoundTripper {
	if t.Base != nil {
		return t.Base
	}
	return http.DefaultTransport
}

func (t *RetryTransport) baseDelay() time.Duration {
	if t.BaseDelay > 0 {
		return t.BaseDelay
	}
	return time.Second
}

func (t *RetryTransport) maxRetry() int {
	if t.MaxRetry > 0 {
		return t.MaxRetry
	}
	return 3
}

func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Read the body once so we can replay it on retry.
	var bodyBytes []byte
	if req.Body != nil && req.Body != http.NoBody {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
	}

	var lastResp *http.Response
	var lastErr error

	for attempt := 0; attempt <= t.maxRetry(); attempt++ {
		// Restore body for each attempt.
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			req.ContentLength = int64(len(bodyBytes))
		}

		resp, err := t.base().RoundTrip(req)
		if err != nil {
			// Network error — only retry if context is still alive.
			if req.Context().Err() != nil {
				return nil, err
			}
			lastErr = err
			if attempt < t.maxRetry() {
				if waitErr := t.wait(req.Context(), attempt, nil); waitErr != nil {
					return nil, waitErr
				}
			}
			continue
		}

		if !retryStatuses[resp.StatusCode] {
			return resp, nil
		}

		log.Debug("retrying request", "attempt", attempt+1, "status", resp.StatusCode, "url", req.URL.String())

		// Drain and close body before retrying so the connection can be reused.
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()

		if attempt >= t.maxRetry() {
			lastResp = resp
			break
		}

		if waitErr := t.wait(req.Context(), attempt, resp); waitErr != nil {
			return nil, waitErr
		}
		lastResp = resp
	}

	if lastErr != nil {
		return nil, lastErr
	}
	// Re-open with empty body so callers get a valid response object.
	lastResp.Body = io.NopCloser(bytes.NewReader(nil))
	return lastResp, nil
}

// wait sleeps for the appropriate backoff duration, respecting context
// cancellation and Retry-After headers. Returns an error if the context
// is cancelled or if Retry-After exceeds maxRetryAfter.
func (t *RetryTransport) wait(ctx context.Context, attempt int, resp *http.Response) error {
	delay := t.baseDelay() * (1 << uint(attempt)) // 1s, 2s, 4s, ...

	if resp != nil {
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(ra); err == nil {
				raDelay := time.Duration(secs) * time.Second
				if raDelay > maxRetryAfter {
					// Server is asking us to wait too long — bail out.
					return &RetryAfterExceededError{Requested: raDelay, Cap: maxRetryAfter}
				}
				delay = raDelay
			}
		}
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}

// RetryAfterExceededError is returned when the server's Retry-After header
// requests a wait longer than maxRetryAfter.
type RetryAfterExceededError struct {
	Requested time.Duration
	Cap       time.Duration
}

func (e *RetryAfterExceededError) Error() string {
	return "Retry-After " + e.Requested.String() + " exceeds cap " + e.Cap.String()
}
