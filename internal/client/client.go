package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/quantcli/withings-export-cli/internal/auth"
)

const BaseURL = "https://wbsapi.withings.net"

type Client struct {
	http    *http.Client
	baseURL string
}

// New returns a client pointed at Withings' production API. Setting
// WITHINGS_API_BASE redirects requests to that origin instead — used by
// the compat-test fixture to point the binary at a stub server so the
// §4 data-path subtests can run without live OAuth.
func New() *Client {
	base := BaseURL
	if v := strings.TrimSpace(os.Getenv("WITHINGS_API_BASE")); v != "" {
		base = strings.TrimRight(v, "/")
	}
	return &Client{http: &http.Client{}, baseURL: base}
}

// Call posts form-encoded params to a Withings API path and decodes the `body` field
// of the standard envelope {"status":N,"body":{...}} into out.
// The access token is injected automatically.
func (c *Client) Call(path string, params url.Values, out any) error {
	token, err := auth.GetToken()
	if err != nil {
		return err
	}
	if params == nil {
		params = url.Values{}
	}

	req, err := http.NewRequest("POST", c.baseURL+path, strings.NewReader(params.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("withings API returned HTTP %d: %s", resp.StatusCode, string(body))
	}

	var envelope struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body"`
		Error  string          `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("failed to parse response envelope: %w\nbody: %s", err, string(body))
	}
	if envelope.Status != 0 {
		msg := envelope.Error
		if msg == "" {
			msg = string(envelope.Body)
		}
		return fmt.Errorf("withings API status %d: %s", envelope.Status, msg)
	}
	if out != nil {
		return json.Unmarshal(envelope.Body, out)
	}
	return nil
}
