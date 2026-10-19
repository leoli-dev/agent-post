// Package cli implements machine-readable operations over the shared HTTP client.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/leoli-dev/agent-post/internal/client"
	"io"
	"strconv"
	"strings"
)

const Help = `agent-post: polling mailboxes for logical AI agents

Usage:
  agent-post serve [--listen 127.0.0.1:8080] [--data-dir DIR] [--secure-cookies]
  agent-post version
  agent-post [--config PATH] mcp
  agent-post [--config PATH] COMMAND [--FIELD VALUE ...]

Commands: me directory limits channels groups inbox claim ack send reply
          group get|create|invite|join|leave|send history files download

Fields use kebab-case: --to-agent-ids 'b,c' --body 'hello' --file-path PATH
  --acks '[{"message_id":"msg_example","claim_token":"token_example"}]'
  --group-id ID --channel-id ID --message-id ID --file-id ID --destination PATH
  --limit N --cursor CURSOR --kind private|group --order oldest|newest
  --max-bytes N bounds a download (default and maximum 1073741824).
  --json OBJECT supplies operation arguments as JSON instead of field flags.

Configuration: AGENT_POST_URL, AGENT_POST_API_KEY, AGENT_POST_CONFIG;
optional owner-only user config agent-post/config.json with url and api_key.
Normal output is JSON; errors preserve API JSON on stdout and diagnostics on stderr.
Polling is immediate. Claim never auto-ACKs. ACK means read, not task completion.
`

func ExtractConfig(args []string) (string, []string, error) {
	var path string
	out := []string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--config" {
			if i+1 >= len(args) {
				return "", nil, errors.New("--config requires a path")
			}
			i++
			path = args[i]
		} else if strings.HasPrefix(a, "--config=") {
			path = strings.TrimPrefix(a, "--config=")
		} else {
			out = append(out, a)
		}
	}
	return path, out, nil
}
func Parse(args []string) (client.Operation, map[string]any, error) {
	if len(args) == 0 {
		return client.Operation{}, nil, errors.New("a command is required")
	}
	name := args[0]
	args = args[1:]
	if name == "group" {
		if len(args) == 0 {
			return client.Operation{}, nil, errors.New("group requires a subcommand")
		}
		name = "group_" + args[0]
		args = args[1:]
	}
	op, ok := client.FindOperation(name)
	if !ok {
		return op, nil, fmt.Errorf("unknown command %q", name)
	}
	values := map[string]any{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			return op, nil, fmt.Errorf("expected --field, got %q", arg)
		}
		pair := strings.SplitN(strings.TrimPrefix(arg, "--"), "=", 2)
		key := strings.ReplaceAll(pair[0], "-", "_")
		var value string
		if len(pair) == 2 {
			value = pair[1]
		} else {
			if i+1 >= len(args) {
				return op, nil, fmt.Errorf("%s requires a value", arg)
			}
			i++
			value = args[i]
		}
		if key == "json" {
			if len(values) != 0 || i != len(args)-1 {
				return op, nil, errors.New("--json cannot be combined with field flags")
			}
			dec := json.NewDecoder(strings.NewReader(value))
			dec.UseNumber()
			if err := dec.Decode(&values); err != nil {
				return op, nil, err
			}
			var extra any
			if err := dec.Decode(&extra); err != io.EOF {
				return op, nil, errors.New("--json requires exactly one object")
			}
			if values == nil {
				return op, nil, errors.New("--json requires an object")
			}
			break
		}
		typ, ok := op.Fields[key]
		if !ok {
			return op, nil, fmt.Errorf("unknown field %q", key)
		}
		if _, ok = values[key]; ok {
			return op, nil, fmt.Errorf("repeated field %s", key)
		}
		switch typ {
		case "integer":
			n, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return op, nil, fmt.Errorf("%s requires an integer", arg)
			}
			values[key] = n
		case "array":
			var list []any
			if strings.HasPrefix(strings.TrimSpace(value), "[") {
				if err := json.Unmarshal([]byte(value), &list); err != nil {
					return op, nil, err
				}
			} else if key == "acks" {
				return op, nil, errors.New("--acks requires a JSON array")
			} else {
				for _, v := range strings.Split(value, ",") {
					list = append(list, v)
				}
			}
			values[key] = list
		default:
			values[key] = value
		}
	}
	return op, values, op.Validate(values)
}
func Run(ctx context.Context, c *client.Client, args []string, stdout, stderr io.Writer) int {
	op, values, err := Parse(args)
	if err != nil {
		return Failure(stdout, stderr, err)
	}
	raw, err := c.Execute(ctx, op, values)
	if len(raw) > 0 {
		fmt.Fprintln(stdout, string(raw))
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		if len(raw) == 0 {
			writeError(stdout, err)
		}
		return 1
	}
	return 0
}
func writeError(w io.Writer, err error) {
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "client_error", "message": err.Error()}})
}
func Failure(stdout, stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, err)
	writeError(stdout, err)
	return 1
}
