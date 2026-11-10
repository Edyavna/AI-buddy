// Package providerhttp bounds external model calls without exposing response bodies.
package providerhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/Edyavna/AI-buddy/backend/internal/ports"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	BaseURL, Key string
	HTTP         *http.Client
}

func (c Client) Post(ctx context.Context, path string, in, out any) error {
	u, e := url.Parse(c.BaseURL)
	if e != nil || u.Host == "" || (u.Scheme != "https" && u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") || c.Key == "" {
		return ports.ErrUnavailable
	}
	b, e := json.Marshal(in)
	if e != nil {
		return ports.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(b))
	if e != nil {
		return ports.ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	resp, e := client.Do(req)
	if e != nil {
		return ports.ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ports.ErrUnavailable
	}
	data, e := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if e != nil || len(data) > 2<<20 || json.Unmarshal(data, out) != nil {
		return ports.ErrUnavailable
	}
	return nil
}
