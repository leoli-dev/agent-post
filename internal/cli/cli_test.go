package cli

import (
	"bytes"
	"context"
	"github.com/leoli-dev/agent-post/internal/client"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCommandsPreserveAPIOutcomes(t *testing.T) {
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if r.URL.Path == "/api/v1/inbox/claim" {
			w.Write([]byte(`{"messages":[{"message_id":"m","body":"peer data","claim_token":"t","context":{"channel":{"id":"dm_example","kind":"private","peer_agent_id":"b"},"sender":{"id":"b","name":"Bob"}}}]}`))
		} else if r.URL.Path == "/api/v1/messages/m/reply" {
			w.Write([]byte(`{"status":"sent","message_id":"new","channel_id":"dm_example"}`))
		} else {
			w.Write([]byte(`{"results":[{"status":"sent"},{"status":"rejected"}]}`))
		}
	}))
	defer server.Close()
	c, _ := client.New(client.Config{URL: server.URL, APIKey: "test"})
	var out, errOut bytes.Buffer
	if code := Run(context.Background(), c, []string{"claim", "--limit", "1"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "claim_token") {
		t.Fatalf("%d %s %s", code, out.String(), errOut.String())
	}
	if len(calls) != 1 || calls[0] != "POST /api/v1/inbox/claim" {
		t.Fatal("claim performed extra operation", calls)
	}
	out.Reset()
	if code := Run(context.Background(), c, []string{"send", "--to-agent-ids", "b,c", "--body", "hi"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), "rejected") {
		t.Fatal("lost target results")
	}
	out.Reset()
	if code := Run(context.Background(), c, []string{"reply", "--message-id", "m", "--body", "hello"}, &out, &errOut); code != 0 || !strings.Contains(out.String(), `"status":"sent"`) || strings.Contains(out.String(), `"results"`) {
		t.Fatalf("reply result: %d %s %s", code, out.String(), errOut.String())
	}
}
func TestParseValidation(t *testing.T) {
	for _, args := range [][]string{{"ack"}, {"claim", "--order", "random"}, {"claim", "--limit", "0"}, {"send", "--to-agent-ids", "b", "--body", "x", "--body", "y"}, {"me", "--json", "null"}, {"me", "--json", "{} {}"}, {"group", "join", "--group-id", ""}} {
		if _, _, err := Parse(args); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
	if _, _, err := Parse([]string{"ack", "--acks", `[{"message_id":"m","claim_token":"t"}]`}); err != nil {
		t.Fatal(err)
	}
}
