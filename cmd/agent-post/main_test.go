package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Build and exercise the shipped entry point, including its stdio discipline.
func TestExecutableAdapters(t *testing.T) {
	dir := t.TempDir()
	name := "agent-post"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(dir, name)
	build := exec.Command("go", "build", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	var mu sync.Mutex
	paths := []string{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer synthetic-test-key" {
			t.Error("wrong auth")
		}
		switch r.URL.Path {
		case "/api/v1/me":
			w.Write([]byte(`{"agent_id":"agt_example"}`))
		case "/api/v1/inbox/claim":
			w.Write([]byte(`{"messages":[{"id":"msg_example","claim_token":"opaque"}]}`))
		case "/api/v1/messages/direct":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["body"] != "hello" {
				t.Error("body lost")
			}
			w.Write([]byte(`{"results":[{"target_agent_id":"agt_b","status":"sent"},{"target_agent_id":"agt_invalid","status":"rejected"}]}`))
		default:
			w.WriteHeader(403)
			w.Write([]byte(`{"error":{"code":"source_ip_denied","settings_revision":7}}`))
		}
	}))
	defer api.Close()
	config := filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, []byte(`{"url":"`+api.URL+`","api_key":"synthetic-test-key"}`), 0600); err != nil {
		t.Fatal(err)
	}
	env := []string{}
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "AGENT_POST_") {
			env = append(env, v)
		}
	}
	env = append(env, "AGENT_POST_CONFIG="+config)
	call := func(args ...string) (string, string, error) {
		cmd := exec.Command(binary, args...)
		cmd.Env = env
		var out, stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		err := cmd.Run()
		return out.String(), stderr.String(), err
	}
	for _, args := range [][]string{{"version"}, {"me"}, {"claim", "--limit", "1"}, {"send", "--to-agent-ids", "agt_b,agt_invalid", "--body", "hello"}} {
		out, stderr, err := call(args...)
		if err != nil || !json.Valid([]byte(out)) || stderr != "" {
			t.Fatalf("%v: %q %q %v", args, out, stderr, err)
		}
	}
	out, stderr, err := call("limits")
	if err == nil || !strings.Contains(out, "source_ip_denied") || !strings.Contains(out, "settings_revision") || stderr == "" {
		t.Fatalf("error outcome: %q %q %v", out, stderr, err)
	}
	mu.Lock()
	for _, p := range paths {
		if p == "/api/v1/inbox/ack" {
			t.Error("auto ACK")
		}
	}
	mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "mcp")
	cmd.Env = env
	var mcpErr bytes.Buffer
	cmd.Stderr = &mcpErr
	session, err := sdk.NewClient(&sdk.Implementation{Name: "executable-smoke", Version: "1"}, nil).Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("MCP handshake: %v %s", err, mcpErr.String())
	}
	defer session.Close()
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "agent_post_me", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("MCP call: %+v %v", result, err)
	}
}
