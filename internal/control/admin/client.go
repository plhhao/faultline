package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

func Call(ctx context.Context, socket, operation, config string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	defer transport.CloseIdleConnections()
	method := http.MethodPost
	var body io.Reader
	if operation == "status" {
		method = http.MethodGet
	} else if operation == "reload" {
		data, err := json.Marshal(struct {
			Config string `json:"config"`
		}{config})
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	request, err := http.NewRequestWithContext(ctx, method, "http://faultline/"+operation, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		if operation == "status" {
			return nil, fmt.Errorf("admin status failed: %w", err)
		}
		return nil, fmt.Errorf("admin %s failed (mutation may already have applied; check status): %w", operation, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 4<<20+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 4<<20 || !json.Valid(data) {
		return nil, fmt.Errorf("invalid admin response")
	}
	if response.StatusCode != http.StatusOK {
		var failure struct {
			Error string `json:"error"`
		}
		json.Unmarshal(data, &failure)
		return nil, fmt.Errorf("admin %s: %s", operation, failure.Error)
	}
	return data, nil
}
