package webhook

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	domain "github.com/arisone/redcapital/internal/domain/notification"
)

type Adapter struct {
	URL     string
	Headers map[string]string
	Client  *http.Client
}

func New(url string, headers map[string]string, timeout time.Duration) *Adapter {
	copy := make(map[string]string, len(headers))
	for key, value := range headers {
		copy[key] = value
	}
	return &Adapter{URL: url, Headers: copy, Client: &http.Client{Timeout: timeout}}
}

func (a *Adapter) Deliver(ctx context.Context, n domain.Notification) domain.DeliveryResult {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.URL, bytesReader(n.Payload))
	if err != nil {
		return domain.DeliveryResult{Outcome: domain.DeliveryPermanent, Err: err}
	}
	for key, value := range a.Headers {
		req.Header.Set(key, value)
	}
	resp, err := a.Client.Do(req)
	if err != nil {
		return domain.DeliveryResult{Outcome: domain.DeliveryRetryable, Err: err}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return domain.DeliveryResult{Outcome: domain.DeliverySucceeded, StatusCode: resp.StatusCode}
	}
	if resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return domain.DeliveryResult{Outcome: domain.DeliveryRetryable, StatusCode: resp.StatusCode, Err: fmt.Errorf("retryable provider response")}
	}
	return domain.DeliveryResult{Outcome: domain.DeliveryPermanent, StatusCode: resp.StatusCode, Err: fmt.Errorf("permanent provider response")}
}

type payloadReader struct {
	payload []byte
	offset  int
}

func bytesReader(payload []byte) *payloadReader { return &payloadReader{payload: payload} }
func (r *payloadReader) Read(p []byte) (int, error) {
	if r.offset >= len(r.payload) {
		return 0, io.EOF
	}
	n := copy(p, r.payload[r.offset:])
	r.offset += n
	return n, nil
}
