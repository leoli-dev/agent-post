// Package client is the shared HTTP transport for Agent Post's local adapters.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const MaxJSONBytes = 16 << 20
const MaxDownloadBytes int64 = 1 << 30

type Config struct {
	URL    string `json:"url"`
	APIKey string `json:"api_key"`
}

// LoadConfig reads a user-wide optional configuration, then environment overrides.
func LoadConfig(path string) (Config, error) {
	var c Config
	explicit := path != ""
	if path == "" {
		path = os.Getenv("AGENT_POST_CONFIG")
		explicit = path != ""
	}
	if path == "" {
		d, err := os.UserConfigDir()
		if err != nil {
			return c, err
		}
		path = filepath.Join(d, "agent-post", "config.json")
	}
	st, err := os.Stat(path)
	if err == nil {
		if !st.Mode().IsRegular() {
			return c, errors.New("config must be a regular file")
		}
		if st.Mode().Perm()&0077 != 0 {
			return c, errors.New("config permissions must restrict access to its owner (chmod 600)")
		}
		f, e := os.Open(path)
		if e != nil {
			return c, e
		}
		defer f.Close()
		dec := json.NewDecoder(io.LimitReader(f, 65537))
		dec.DisallowUnknownFields()
		if e = dec.Decode(&c); e != nil {
			return c, fmt.Errorf("invalid config: %w", e)
		}
		var extra any
		if e = dec.Decode(&extra); e != io.EOF {
			return c, errors.New("config must contain one JSON object")
		}
	} else if explicit || !os.IsNotExist(err) {
		return c, err
	}
	if v := os.Getenv("AGENT_POST_URL"); v != "" {
		c.URL = v
	}
	if v := os.Getenv("AGENT_POST_API_KEY"); v != "" {
		c.APIKey = v
	}
	return c, nil
}

type Client struct {
	base string
	key  string
	http *http.Client
}

func New(c Config) (*Client, error) {
	u, err := url.Parse(c.URL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(c.URL, "#") || (u.EscapedPath() != "" && u.EscapedPath() != "/") {
		return nil, errors.New("url must be an absolute http(s) service origin without credentials, path, query or fragment")
	}
	if c.APIKey == "" || strings.ContainsAny(c.APIKey, "\r\n\t ") {
		return nil, errors.New("a nonempty API key without whitespace is required")
	}
	return &Client{base: strings.TrimRight(c.URL, "/"), key: c.APIKey, http: &http.Client{Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

type APIError struct {
	Status  int
	Payload json.RawMessage
}

func (e *APIError) Error() string { return fmt.Sprintf("API returned HTTP %d", e.Status) }

func (c *Client) request(ctx context.Context, method, path, contentType string, body io.Reader) (*http.Response, error) {
	if !strings.HasPrefix(path, "/api/v1/") {
		return nil, errors.New("invalid API path")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return c.http.Do(req)
}
func decode(res *http.Response) (json.RawMessage, error) {
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, MaxJSONBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxJSONBytes {
		return nil, errors.New("API response exceeds client JSON bound")
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("API returned invalid JSON (HTTP %d)", res.StatusCode)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return data, &APIError{res.StatusCode, data}
	}
	return data, nil
}
func (c *Client) JSON(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	res, err := c.request(ctx, method, path, "application/json", r)
	if err != nil {
		return nil, errors.New("HTTP request failed (check connection and service URL)")
	}
	return decode(res)
}

// Send streams one local attachment, never buffering its bytes in memory.
func (c *Client) Send(ctx context.Context, path string, metadata any, filePath string) (json.RawMessage, error) {
	if filePath == "" {
		return c.JSON(ctx, http.MethodPost, path, metadata)
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("attachment must be a regular file")
	}
	meta, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	pr, pw := io.Pipe()
	defer pr.Close()
	w := multipart.NewWriter(pw)
	done := make(chan error, 1)
	go func() {
		err := w.WriteField("metadata", string(meta))
		if err == nil {
			var dst io.Writer
			dst, err = w.CreateFormFile("file", filepath.Base(filePath))
			if err == nil {
				_, err = io.Copy(dst, f)
			}
		}
		if err == nil {
			err = w.Close()
		}
		pw.CloseWithError(err)
		done <- err
	}()
	res, err := c.request(ctx, http.MethodPost, path, w.FormDataContentType(), pr)
	pr.Close()
	writeErr := <-done
	if err != nil {
		return nil, errors.New("HTTP upload failed (check connection and service URL)")
	}
	// A server may reject an upload before reading it; preserve that API outcome.
	if res.StatusCode >= 200 && res.StatusCode < 300 && writeErr != nil {
		res.Body.Close()
		return nil, writeErr
	}
	return decode(res)
}

// Download requires a caller-chosen destination and atomically installs a bounded
// stream without replacing any existing path, including symlinks.
func (c *Client) Download(ctx context.Context, id, destination string, maxBytes int64) (json.RawMessage, error) {
	if destination == "" {
		return nil, errors.New("an explicit destination is required")
	}
	if maxBytes <= 0 || maxBytes > MaxDownloadBytes {
		return nil, errors.New("max_bytes must be between 1 and 1073741824")
	}
	if _, err := os.Lstat(destination); err == nil {
		return nil, errors.New("destination already exists")
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	res, err := c.request(ctx, http.MethodGet, "/api/v1/files/"+url.PathEscape(id)+"/content", "", nil)
	if err != nil {
		return nil, errors.New("HTTP download failed")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return decode(res)
	}
	defer res.Body.Close()
	if res.ContentLength > maxBytes {
		return nil, errors.New("download exceeds max_bytes")
	}
	f, err := os.CreateTemp(filepath.Dir(destination), ".agent-post-download-*")
	if err != nil {
		return nil, err
	}
	temp := f.Name()
	defer os.Remove(temp)
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(res.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if n > maxBytes {
		return nil, errors.New("download exceeds max_bytes")
	}
	if res.ContentLength >= 0 && n != res.ContentLength {
		return nil, errors.New("incomplete download")
	}
	if err = f.Sync(); err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err = os.Link(temp, destination); err != nil {
		return nil, fmt.Errorf("cannot install destination without replacement: %w", err)
	}
	return json.Marshal(map[string]any{"file_id": id, "destination": destination, "bytes": n})
}
