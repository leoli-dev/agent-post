package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTransportAndStreaming(t *testing.T) {
	data := strings.Repeat("binary\x00", 10000)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing authentication")
		}
		switch r.URL.Path {
		case "/api/v1/messages/direct":
			mr, e := r.MultipartReader()
			if e != nil {
				t.Error(e)
				return
			}
			p, e := mr.NextPart()
			if e != nil {
				t.Error(e)
				return
			}
			b, _ := io.ReadAll(p)
			if p.FormName() != "metadata" || !json.Valid(b) {
				t.Error("bad metadata")
			}
			p, e = mr.NextPart()
			if e != nil {
				t.Error(e)
				return
			}
			b, _ = io.ReadAll(p)
			if string(b) != data || p.FormName() != "file" {
				t.Error("bad attachment")
			}
			io.WriteString(w, `{"results":[{"status":"sent"},{"status":"rejected"}]}`)
		case "/api/v1/files/f/content":
			io.WriteString(w, data)
		default:
			w.WriteHeader(403)
			io.WriteString(w, `{"error":{"code":"source_ip_denied"}}`)
		}
	}))
	defer s.Close()
	c, _ := New(Config{s.URL, "test-key"})
	dir := t.TempDir()
	file := filepath.Join(dir, "file.bin")
	os.WriteFile(file, []byte(data), 0600)
	raw, e := c.Send(context.Background(), "/api/v1/messages/direct", map[string]any{"to_agent_ids": []string{"b", "invalid"}, "body": "text"}, file)
	if e != nil || !strings.Contains(string(raw), "rejected") {
		t.Fatalf("%s %v", raw, e)
	}
	dest := filepath.Join(dir, "download")
	if _, e = c.Download(context.Background(), "f", dest, MaxDownloadBytes); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(dest)
	if string(b) != data {
		t.Fatal("download corruption")
	}
	if _, e = c.Download(context.Background(), "f", dest, MaxDownloadBytes); e == nil {
		t.Fatal("clobbered existing file")
	}
	small := filepath.Join(dir, "small")
	if _, e = c.Download(context.Background(), "f", small, 2); e == nil {
		t.Fatal("unbounded download")
	}
	if _, e = os.Stat(small); !os.IsNotExist(e) {
		t.Fatal("partial file installed")
	}
	raw, e = c.JSON(context.Background(), "GET", "/api/v1/me", nil)
	if _, ok := e.(*APIError); !ok || !strings.Contains(string(raw), "source_ip_denied") {
		t.Fatalf("lost API error: %s %v", raw, e)
	}
}
func TestRedirectDoesNotLeakKey(t *testing.T) {
	hit := false
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer target.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer s.Close()
	c, _ := New(Config{s.URL, "secret"})
	_, _ = c.JSON(context.Background(), "GET", "/api/v1/me", nil)
	if hit {
		t.Fatal("followed redirect")
	}
}
func TestConfigValidation(t *testing.T) {
	for _, u := range []string{"", "https://user:pass@example.com", "https://example.com/path", "ftp://example.com", "https://example.com?secret=x", "https://example.com?", "https://example.com#", "https://example.com/%2F"} {
		if _, e := New(Config{u, "key"}); e == nil {
			t.Errorf("accepted %q", u)
		}
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "config.json")
	os.WriteFile(p, []byte(`{"url":"http://example.test","api_key":"synthetic"}`), 0644)
	if _, e := LoadConfig(p); e == nil {
		t.Fatal("accepted permissive credentials")
	}
	os.Chmod(p, 0600)
	t.Setenv("AGENT_POST_URL", "http://override.test")
	c, e := LoadConfig(p)
	if e != nil || c.URL != "http://override.test" {
		t.Fatalf("%+v %v", c, e)
	}
}
