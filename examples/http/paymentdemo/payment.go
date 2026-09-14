// Package paymentdemo provides an in-memory dependency and retry driver for the lost-response demo.
package paymentdemo

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type Payment struct {
	ID        int    `json:"id"`
	Operation string `json:"operation"`
}

type Service struct {
	mu       sync.Mutex
	payments []Payment
	keys     map[string]Payment
}

func NewService() *Service { return &Service{keys: make(map[string]Payment)} }

func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method == "GET" && r.URL.Path == "/healthz" {
		io.WriteString(w, "{}\n")
		return
	}
	if r.URL.Path != "/payments" {
		http.NotFound(w, r)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch r.Method {
	case "GET":
		payments := []Payment{}
		for _, p := range s.payments {
			if p.Operation == r.URL.Query().Get("operation") {
				payments = append(payments, p)
			}
		}
		json.NewEncoder(w).Encode(payments)
	case "POST":
		var input struct {
			Operation string `json:"operation"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil || input.Operation == "" {
			http.Error(w, "operation is required", http.StatusBadRequest)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		p, exists := s.keys[key]
		if exists && p.Operation != input.Operation {
			http.Error(w, "idempotency key reused for another operation", http.StatusConflict)
			return
		}
		if !exists || key == "" {
			p = Payment{ID: len(s.payments) + 1, Operation: input.Operation}
			s.payments = append(s.payments, p)
			if key != "" {
				s.keys[key] = p
			}
		}
		// Persist in the demo store before sending headers; the test reads this store independently.
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(p)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

type Result struct {
	Operation  string `json:"operation"`
	Idempotent bool   `json:"idempotent"`
	Attempts   int    `json:"attempts"`
	Timeouts   int    `json:"timeouts"`
	Payments   int    `json:"payments"`
}

// Run retries the same operation twice, then queries the dependency directly.
// Assertions belong to this demo driver, not the proxy or recorder.
func Run(ctx context.Context, proxyURL, upstreamURL string, idempotent bool, timeout time.Duration) (Result, error) {
	result := Result{Operation: rand.Text(), Idempotent: idempotent}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DisableKeepAlives = true
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: timeout}
	data, _ := json.Marshal(map[string]string{"operation": result.Operation})
	for range 2 {
		req, err := http.NewRequestWithContext(ctx, "POST", proxyURL+"/payments", bytes.NewReader(data))
		if err != nil {
			return result, err
		}
		req.Header.Set("Content-Type", "application/json")
		if idempotent {
			req.Header.Set("Idempotency-Key", result.Operation)
		}
		result.Attempts++
		resp, err := client.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		var networkError net.Error
		if !errors.As(err, &networkError) || !networkError.Timeout() || ctx.Err() != nil {
			return result, fmt.Errorf("attempt %d: expected client timeout, got %v", result.Attempts, err)
		}
		result.Timeouts++
	}
	client.Timeout = 5 * time.Second
	req, err := http.NewRequestWithContext(ctx, "GET", upstreamURL+"/payments?operation="+url.QueryEscape(result.Operation), nil)
	if err != nil {
		return result, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return result, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return result, fmt.Errorf("payment inspection returned %d", resp.StatusCode)
	}
	var payments []Payment
	if err := json.NewDecoder(resp.Body).Decode(&payments); err != nil {
		return result, err
	}
	result.Payments = len(payments)
	want := 2
	if idempotent {
		want = 1
	}
	if result.Payments != want {
		return result, fmt.Errorf("expected %d stored payments, got %d", want, result.Payments)
	}
	return result, nil
}
