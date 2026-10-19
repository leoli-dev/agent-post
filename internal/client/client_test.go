package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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
	if runtime.GOOS != "windows" {
		if _, e := LoadConfig(p); e == nil {
			t.Fatal("accepted permissive credentials")
		}
	}
	os.Chmod(p, 0600)
	t.Setenv("AGENT_POST_URL", "http://override.test")
	c, e := LoadConfig(p)
	if e != nil || c.URL != "http://override.test" {
		t.Fatalf("%+v %v", c, e)
	}
}

// The fixture counts only these explicitly scripted legacy client workflows.
// It does not measure a scheduler, model turns, production helpers or latency.
func TestR0aLegacyWorkflowCallCounts(t *testing.T) {
	var calls []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/channels":
			io.WriteString(w, `{"channels":[{"id":"dm_example","kind":"private","agent_ids":["alice","bob"]},{"id":"group_example","kind":"group","group_id":"group_example"}],"next_cursor":""}`)
		case "/api/v1/messages/direct":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["body"] == "unknown" {
				io.WriteString(w, `{"results":`)
				return
			}
			io.WriteString(w, `{"request_id":"req_example","results":[{"status":"sent","message_id":"msg_example","channel_id":"dm_example","target_agent_id":"bob"}]}`)
		case "/api/v1/groups/group_example/messages":
			io.WriteString(w, `{"status":"sent","message_id":"msg_group","channel_id":"group_example"}`)
		case "/api/v1/inbox":
			io.WriteString(w, `{"unread":0,"claimable":0,"leased":0,"channels":[],"next_cursor":""}`)
		case "/api/v1/channels/dm_example/messages":
			io.WriteString(w, `{"messages":[],"next_cursor":""}`)
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
			io.WriteString(w, `{}`)
		}
	}))
	defer s.Close()
	c, err := New(Config{s.URL, "test-key"})
	if err != nil {
		t.Fatal(err)
	}
	execute := func(name string, args map[string]any) error {
		op, ok := FindOperation(name)
		if !ok {
			t.Fatal(name)
		}
		_, err := c.Execute(context.Background(), op, args)
		return err
	}
	check := func(want ...string) {
		t.Helper()
		if len(calls) != len(want) {
			t.Fatalf("calls %v, want %v", calls, want)
		}
		for i := range want {
			if calls[i] != want[i] {
				t.Fatalf("calls %v, want %v", calls, want)
			}
		}
		calls = nil
	}
	if err = execute("channels", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err = execute("send", map[string]any{"to_agent_ids": []any{"bob"}, "body": "private"}); err != nil {
		t.Fatal(err)
	}
	check("GET /api/v1/channels", "POST /api/v1/messages/direct")
	if err = execute("channels", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err = execute("group_send", map[string]any{"group_id": "group_example", "body": "group"}); err != nil {
		t.Fatal(err)
	}
	check("GET /api/v1/channels", "POST /api/v1/groups/group_example/messages")
	if err = execute("inbox", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	check("GET /api/v1/inbox")
	if err = execute("send", map[string]any{"to_agent_ids": []any{"bob"}, "body": "unknown"}); err == nil {
		t.Fatal("truncated response accepted")
	}
	if err = execute("history", map[string]any{"channel_id": "dm_example"}); err != nil {
		t.Fatal(err)
	}
	check("POST /api/v1/messages/direct", "GET /api/v1/channels/dm_example/messages")
}

func TestReplyStandaloneResultParser(t *testing.T) {
	var response string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/messages/msg_example/reply" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request: %s %s %s", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		var args map[string]any
		if err := json.NewDecoder(r.Body).Decode(&args); err != nil || len(args) != 1 || args["body"] != "hello" {
			t.Errorf("body: %v %v", args, err)
		}
		io.WriteString(w, response)
	}))
	defer s.Close()
	c, _ := New(Config{s.URL, "test-key"})
	op, _ := FindOperation("reply")
	for _, tc := range []struct {
		body string
		ok   bool
	}{
		{`{"status":"sent","message_id":"msg_new","channel_id":"dm_example"}`, true},
		{`{"status":"duplicate","message_id":"msg_old","channel_id":"dm_example"}`, true},
		{`{"results":[{"status":"sent","message_id":"msg_new","channel_id":"dm_example"}]}`, false},
		{`{"status":"rejected","message_id":"msg_new","channel_id":"dm_example"}`, false},
		{`{"status":"sent","message_id":"","channel_id":"dm_example"}`, false},
		{`{"status":"sent","message_id":"msg_new"}`, false},
	} {
		response = tc.body
		raw, err := c.Execute(context.Background(), op, map[string]any{"message_id": "msg_example", "body": "hello"})
		if tc.ok {
			if err != nil || string(raw) != tc.body {
				t.Fatal(tc, string(raw), err)
			}
		} else if err == nil || raw != nil || !strings.Contains(err.Error(), "unknown") {
			t.Fatal(tc, string(raw), err)
		}
	}
	if _, err := c.Execute(context.Background(), op, map[string]any{"message_id": "msg_example", "body": ""}); err == nil {
		t.Fatal("empty reply accepted")
	}
	if _, err := c.Execute(context.Background(), op, map[string]any{"message_id": "msg_example", "body": "hello", "file_path": "x"}); err == nil {
		t.Fatal("file override accepted")
	}
}

// This is the corresponding scripted R1 route, not an actual collaboration pilot.
func TestR4SelectedRouteReplyCallCounts(t *testing.T) {
	var calls []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/messages/msg_private/reply", "/api/v1/messages/msg_group/reply":
			io.WriteString(w, `{"status":"sent","message_id":"msg_new","channel_id":"channel_example"}`)
		case "/api/v1/messages/msg_unknown/reply":
			io.WriteString(w, `{"status":"sent"`)
		case "/api/v1/channels/channel_example/messages":
			io.WriteString(w, `{"messages":[],"next_cursor":""}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
			io.WriteString(w, `{}`)
		}
	}))
	defer s.Close()
	c, _ := New(Config{s.URL, "test-key"})
	reply, _ := FindOperation("reply")
	history, _ := FindOperation("history")
	for _, id := range []string{"msg_private", "msg_group"} {
		_, err := c.Execute(context.Background(), reply, map[string]any{"message_id": id, "body": "hello"})
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) != 1 || calls[0] != "POST /api/v1/messages/"+id+"/reply" {
			t.Fatal(calls)
		}
		calls = nil
	}
	if _, err := c.Execute(context.Background(), reply, map[string]any{"message_id": "msg_unknown", "body": "hello"}); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatal("truncated response was not classified as unknown", err)
	}
	if _, err := c.Execute(context.Background(), history, map[string]any{"channel_id": "channel_example"}); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || calls[0] != "POST /api/v1/messages/msg_unknown/reply" || calls[1] != "GET /api/v1/channels/channel_example/messages" {
		t.Fatal(calls)
	}
}
