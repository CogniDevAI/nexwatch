package checks

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// execHTTP performs one HTTP probe attempt: follows up to maxHTTPRedirects
// redirects, records status code and latency, and — when the connection
// used TLS — the leaf certificate's NotAfter as TLSExpiresAt, regardless of
// whether verify_tls is enabled (skipping verification does not prevent Go
// from exposing the negotiated certificate chain). A body-contains check
// reads at most maxHTTPBodyReadBytes of the response body.
func execHTTP(check *core.Record) probeResult {
	timeout := timeoutDuration(check)
	target := check.GetString("target")

	transport := &http.Transport{}
	if !check.GetBool("verify_tls") {
		// User-opted-in per check (verify_tls=false) — still records
		// certificate expiry from the unverified chain.
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}

	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxHTTPRedirects {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	req, err := http.NewRequest(httpMethod(check), target, nil)
	if err != nil {
		return probeResult{Success: false, Error: fmt.Sprintf("invalid target: %v", err)}
	}

	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return probeResult{Success: false, LatencyMs: msFloat(latency), Error: err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()

	var tlsExpiresAt time.Time
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		tlsExpiresAt = resp.TLS.PeerCertificates[0].NotAfter
	}

	want := expectedStatus(check)
	success := resp.StatusCode == want
	errMsg := ""
	if !success {
		errMsg = fmt.Sprintf("unexpected status code %d (expected %d)", resp.StatusCode, want)
	}

	if success {
		if needle := check.GetString("expected_body_contains"); needle != "" {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, maxHTTPBodyReadBytes))
			if !strings.Contains(string(body), needle) {
				success = false
				errMsg = "response body did not contain the expected text"
			}
		}
	}

	return probeResult{
		Success:      success,
		LatencyMs:    msFloat(latency),
		StatusCode:   resp.StatusCode,
		Error:        errMsg,
		TLSExpiresAt: tlsExpiresAt,
	}
}
