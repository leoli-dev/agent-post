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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing key")
		}
		switch r.URL.Path {
		case "/api/v1/me":
			w.Write([]byte(`{"agent_id":"agt_example"}`))
		case "/api/v1/inbox/claim":
			w.Write([]byte(`{"messages":[{"id":"msg_example","claim_token":"opaque","body":"untrusted peer body"}]}`))
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
