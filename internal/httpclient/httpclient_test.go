package httpclient

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newFastTransport returns a RetryTransport with a very short base delay for tests.
func newFastTransport(base http.RoundTripper) *RetryTransport {
	return &RetryTransport{
		Base:      base,
		BaseDelay: 10 * time.Millisecond,
		MaxRetry:  3,
	}
}

func TestRetry429(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: newFastTransport(http.DefaultTransport)}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestRetry500(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: newFastTransport(http.DefaultTransport)}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetryGatewayStatuses(t *testing.T) {
	for _, status := range []int{502, 503, 504} {
		status := status
		t.Run(http.StatusText(status), func(t *testing.T) {
			attempts := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts++
				if attempts == 1 {
					w.WriteHeader(status)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()

			client := &http.Client{Transport: newFastTransport(http.DefaultTransport)}
			resp, err := client.Get(srv.URL)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Errorf("expected 200, got %d", resp.StatusCode)
			}
		})
	}
}

func TestNoRetryOn400(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	client := &http.Client{Transport: newFastTransport(http.DefaultTransport)}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
	if attempts != 1 {
		t.Errorf("expected exactly 1 attempt for 400, got %d", attempts)
	}
}

func TestNoRetryOn200(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: newFastTransport(http.DefaultTransport)}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if attempts != 1 {
		t.Errorf("expected exactly 1 attempt for 200, got %d", attempts)
	}
}

func TestMaxRetriesExhausted(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := &http.Client{Transport: newFastTransport(http.DefaultTransport)}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", resp.StatusCode)
	}
	// 1 initial + 3 retries = 4 attempts
	if attempts != 4 {
		t.Errorf("expected 4 attempts (1+3 retries), got %d", attempts)
	}
}

func TestRetryAfterHeaderRespected(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "0") // 0s for test speed
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: newFastTransport(http.DefaultTransport)}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestRetryAfterExceedsCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "60") // 60s > 30s cap
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := &http.Client{Transport: newFastTransport(http.DefaultTransport)}
	_, err := client.Get(srv.URL)
	if err == nil {
		t.Fatal("expected error when Retry-After exceeds cap")
	}
	var raErr *RetryAfterExceededError
	// Walk the error chain.
	if !isRetryAfterExceededError(err, &raErr) {
		t.Errorf("expected RetryAfterExceededError, got: %T %v", err, err)
	}
}

func isRetryAfterExceededError(err error, target **RetryAfterExceededError) bool {
	if e, ok := err.(*RetryAfterExceededError); ok {
		*target = e
		return true
	}
	// Check url.Error wrapping.
	type unwrapper interface{ Unwrap() error }
	if u, ok := err.(unwrapper); ok {
		return isRetryAfterExceededError(u.Unwrap(), target)
	}
	return false
}

func TestContextCancelledDuringWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	transport := &RetryTransport{
		Base:      http.DefaultTransport,
		BaseDelay: 200 * time.Millisecond, // longer than context timeout
		MaxRetry:  3,
	}
	client := &http.Client{Transport: transport}
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
	_, err := client.Do(req)
	if err == nil {
		t.Fatal("expected error due to context cancellation")
	}
}

func TestBodyResentOnRetry(t *testing.T) {
	var receivedBodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBodies = append(receivedBodies, string(body))
		if len(receivedBodies) < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: newFastTransport(http.DefaultTransport)}
	bodyContent := `{"key":"value"}`
	resp, err := client.Post(srv.URL, "application/json", bytes.NewBufferString(bodyContent))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if len(receivedBodies) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(receivedBodies))
	}
	for i, b := range receivedBodies {
		if b != bodyContent {
			t.Errorf("attempt %d: expected body %q, got %q", i+1, bodyContent, b)
		}
	}
}
