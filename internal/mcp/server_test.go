package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leoli-dev/agent-post/internal/client"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMCPHelper(t *testing.T) {
	if os.Getenv("AGENT_POST_MCP_TEST_HELPER") != "1" {
		return
	}
	c, e := client.New(client.Config{URL: os.Getenv("AGENT_POST_URL"), APIKey: "test-key"})
	if e != nil {
		os.Exit(2)
	}
	if Run(context.Background(), c, "test") != nil {
		os.Exit(3)
	}
	os.Exit(0)
}
func TestStdioHandshakeAndTools(t *testing.T) {
	var gatewayCalls atomic.Int32
	var deniedCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing key")
		}
		switch r.URL.Path {
		case "/api/v1/me":
			w.Write([]byte(`{"agent_id":"agt_example"}`))
		case "/api/v1/inbox/claim":
			w.Write([]byte(`{"messages":[{"id":"msg_example","claim_token":"opaque","body":"untrusted peer body","context":{"channel":{"id":"dm_example","kind":"private","peer_agent_id":"agt_example"},"sender":{"id":"agt_example","name":"Example"}}}]}`))
		case "/api/v1/messages/msg_example/reply":
			w.Write([]byte(`{"status":"sent","message_id":"msg_reply","channel_id":"dm_example"}`))
		case "/api/v1/messages/msg_gateway/reply":
			gatewayCalls.Add(1)
			w.WriteHeader(502)
			w.Write([]byte(`{"error":{"code":"storage_unavailable"}}`))
		case "/api/v1/messages/msg_denied/reply":
			deniedCalls.Add(1)
			w.WriteHeader(403)
			w.Write([]byte(`{"error":{"code":"source_ip_denied"}}`))
		default:
			w.WriteHeader(403)
			w.Write([]byte(`{"error":{"code":"source_ip_denied"}}`))
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMCPHelper$")
	cmd.Env = append(os.Environ(), "AGENT_POST_MCP_TEST_HELPER=1", "AGENT_POST_URL="+server.URL)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	session, err := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, &sdk.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("handshake: %v %s", err, stderr.String())
	}
	defer session.Close()
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != len(client.Operations) {
		t.Fatalf("tools: %d", len(tools.Tools))
	}
	for _, name := range []string{"agent_post_me", "agent_post_claim"} {
		result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: name, Arguments: map[string]any{}})
		if err != nil || result.IsError || result.StructuredContent == nil {
			t.Fatalf("%s: %+v %v", name, result, err)
		}
	}
	reply, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "agent_post_reply", Arguments: map[string]any{"message_id": "msg_example", "body": "hello"}})
	if err != nil || reply.IsError || reply.StructuredContent == nil {
		t.Fatalf("reply: %+v %v", reply, err)
	}
	replyJSON, _ := json.Marshal(reply.StructuredContent)
	if !strings.Contains(string(replyJSON), `"status":"sent"`) || strings.Contains(string(replyJSON), `"results"`) {
		t.Fatalf("reply shape: %s", replyJSON)
	}
	for _, tc := range []struct {
		id, code string
		unknown  bool
	}{
		{"msg_gateway", "client_error", true}, {"msg_denied", "source_ip_denied", false},
	} {
		result, callErr := session.CallTool(ctx, &sdk.CallToolParams{Name: "agent_post_reply", Arguments: map[string]any{"message_id": tc.id, "body": "hello"}})
		if callErr != nil || !result.IsError || result.StructuredContent == nil {
			t.Fatalf("%s result: %+v %v", tc.id, result, callErr)
		}
		data, _ := json.Marshal(result.StructuredContent)
		var envelope struct {
			Error struct{ Code, Message string }
		}
		if json.Unmarshal(data, &envelope) != nil || envelope.Error.Code != tc.code || (tc.unknown && !strings.Contains(envelope.Error.Message, "unknown")) {
			t.Fatalf("%s error: %s", tc.id, data)
		}
	}
	if gatewayCalls.Load() != 1 || deniedCalls.Load() != 1 {
		t.Fatalf("reply requests gateway=%d denied=%d; want one each", gatewayCalls.Load(), deniedCalls.Load())
	}
	result, err := session.CallTool(ctx, &sdk.CallToolParams{Name: "agent_post_limits", Arguments: map[string]any{}})
	if err != nil || !result.IsError {
		t.Fatalf("missing API error: %+v %v", result, err)
	}
	data, _ := json.Marshal(result.StructuredContent)
	if !strings.Contains(string(data), "source_ip_denied") {
		t.Fatal("API outcome lost", string(data))
	}
	if invalid, callErr := session.CallTool(ctx, &sdk.CallToolParams{Name: "agent_post_claim", Arguments: map[string]any{"limit": 0}}); callErr == nil && !invalid.IsError {
		t.Fatal("invalid schema input accepted")
	}
}
