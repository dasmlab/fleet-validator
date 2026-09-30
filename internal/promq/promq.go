// Package promq runs instant PromQL queries against the hub's Thanos querier with the pod's
// ServiceAccount token (needs the cluster-monitoring-view ClusterRole).
package promq

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrNoData means the query returned an empty vector.
var ErrNoData = errors.New("query returned no data")

// Client is a small instant-query client with a result cache, so metric checks cost at most
// one query per TTL no matter how often clusters are validated.
type Client struct {
	base      string
	tokenFile string
	http      *http.Client
	ttl       time.Duration

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	at  time.Time
	val float64
	err error
}

// New builds a client. caFiles are PEM bundles trusted in addition to the system roots
// (the service CA for *.svc).
func New(base, tokenFile string, caFiles []string, ttl time.Duration) (*Client, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	for _, f := range caFiles {
		if b, err := os.ReadFile(f); err == nil {
			pool.AppendCertsFromPEM(b)
		}
	}
	return &Client{
		base:      strings.TrimRight(base, "/"),
		tokenFile: tokenFile,
		ttl:       ttl,
		cache:     map[string]cached{},
		http: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
			},
		},
	}, nil
}

// Scalar returns the first sample of an instant query.
func (c *Client) Scalar(ctx context.Context, query string) (float64, error) {
	c.mu.Lock()
	if e, ok := c.cache[query]; ok && time.Since(e.at) < c.ttl {
		c.mu.Unlock()
		return e.val, e.err
	}
	c.mu.Unlock()

	v, err := c.query(ctx, query)

	c.mu.Lock()
	c.cache[query] = cached{at: time.Now(), val: v, err: err}
	c.mu.Unlock()
	return v, err
}

func (c *Client) query(ctx context.Context, query string) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.base+"/api/v1/query?query="+url.QueryEscape(query), nil)
	if err != nil {
		return 0, err
	}
	if tok, err := os.ReadFile(c.tokenFile); err == nil {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(tok)))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("prometheus: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Data   struct {
			Result []struct {
				Value [2]any `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, err
	}
	if body.Status != "success" {
		return 0, fmt.Errorf("prometheus: %s", body.Error)
	}
	if len(body.Data.Result) == 0 {
		return 0, ErrNoData
	}
	s, ok := body.Data.Result[0].Value[1].(string)
	if !ok {
		return 0, fmt.Errorf("prometheus: unexpected sample %v", body.Data.Result[0].Value[1])
	}
	return strconv.ParseFloat(s, 64)
}
