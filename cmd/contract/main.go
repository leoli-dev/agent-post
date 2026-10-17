// Command contract exports the agent HTTP request contract from the operation
// registry shared by the CLI and MCP. Run: go run ./cmd/contract.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/leoli-dev/agent-post/internal/client"
)

func main() {
	paths := map[string]any{}
	for _, op := range client.Operations {
		schema := op.Schema()
		properties := schema["properties"].(map[string]any)
		params := []any{}
		keys := make([]string, 0, len(op.Fields))
		for name := range op.Fields {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			if name == "file_path" || name == "destination" || name == "max_bytes" {
				delete(properties, name)
				continue
			}
			in := ""
			if strings.Contains(op.Path, "{"+name+"}") {
				in = "path"
			} else if op.Method == "GET" {
				in = "query"
			}
			if in != "" {
				params = append(params, map[string]any{"name": name, "in": in, "required": in == "path", "schema": properties[name]})
				delete(properties, name)
			}
		}
		required := []string{}
		for _, name := range op.Required {
			if _, ok := properties[name]; ok {
				required = append(required, name)
			}
		}
		delete(schema, "required")
		if len(required) > 0 {
			schema["required"] = required
		}
		responseType := "application/json"
		if op.Name == "download" {
			responseType = "application/octet-stream"
		}
		responseSchema := map[string]any{"type": "object", "additionalProperties": true}
		if op.Name == "download" {
			responseSchema = map[string]any{"type": "string", "contentEncoding": "binary"}
		}
		method := map[string]any{"operationId": op.Name, "summary": op.Description, "parameters": params, "security": []any{map[string]any{"agentKey": []string{}}}, "responses": map[string]any{"200": map[string]any{"description": "Successful operation. Sends and ACKs preserve per-item outcomes; HTTP success alone does not indicate every target succeeded.", "content": map[string]any{responseType: map[string]any{"schema": responseSchema}}}, "default": map[string]any{"description": "Structured error, including authentication, source IP, membership, limit, and claim failures.", "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object", "required": []string{"error"}, "properties": map[string]any{"error": map[string]any{"type": "object", "required": []string{"code"}, "properties": map[string]any{"code": map[string]any{"type": "string"}, "message": map[string]any{"type": "string"}, "limit": map[string]any{}, "settings_revision": map[string]any{"type": "integer"}}}}}}}}}}
		if op.Method != "GET" {
			content := map[string]any{"application/json": map[string]any{"schema": schema}}
			if op.Name == "send" || op.Name == "group_send" {
				content["multipart/form-data"] = map[string]any{"schema": map[string]any{"type": "object", "required": []string{"metadata", "file"}, "additionalProperties": false, "properties": map[string]any{"metadata": map[string]any{"type": "string", "description": "JSON encoding of the same application/json request object."}, "file": map[string]any{"type": "string", "contentMediaType": "application/octet-stream", "contentEncoding": "binary"}}}}
			}
			method["requestBody"] = map[string]any{"required": true, "content": content}
		}
		path := "/api/v1" + op.Path
		if _, ok := paths[path]; !ok {
			paths[path] = map[string]any{}
		}
		paths[path].(map[string]any)[strings.ToLower(op.Method)] = method
	}
	document := map[string]any{"openapi": "3.1.0", "info": map[string]any{"title": "Agent Post agent API", "version": "0.1.0-dev", "description": "Polling-only agent HTTP request contract, generated from internal/client.Operations shared with CLI and MCP. API keys AND exact registered-host source IPs are required. Response objects are intentionally open; concrete examples are documented in runtime.md. No push or automatic ACK."}, "paths": paths, "components": map[string]any{"securitySchemes": map[string]any{"agentKey": map[string]any{"type": "http", "scheme": "bearer", "description": "Logical agent API key. Socket source IP must also match its registered host."}}}}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(document); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
