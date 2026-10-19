package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type countedReader struct {
	r     io.Reader
	reads int
}

func (c *countedReader) Read(p []byte) (int, error) {
	c.reads++
	return c.r.Read(p)
}

func oneResult(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	return out["results"].([]any)[0].(map[string]any)
}

func checkError(t *testing.T, status int, got map[string]any, wantStatus int, code string) {
	t.Helper()
	if status != wantStatus || got["error"].(map[string]any)["code"] != code {
		t.Fatalf("got %d %v, want %d %s", status, got, wantStatus, code)
	}
}

func TestAgentContextPreservesLegacyFieldsAndAdminHistory(t *testing.T) {
	f := setup(t)
	f.upload([]byte("synthetic attachment"), []string{f.ids["bob"]})
	m := f.s.st.Messages[0]
	legacy := f.must("GET", "/admin/api/channels/"+m.ChannelID+"/messages", testPassword, nil)["messages"].([]any)[0].(map[string]any)
	if _, ok := legacy["context"]; ok {
		t.Fatal("administrator history gained agent context")
	}
	before := f.must("GET", "/api/v1/inbox", f.keys["bob"], nil)
	agent := f.must("GET", "/api/v1/channels/"+m.ChannelID+"/messages", f.keys["bob"], nil)["messages"].([]any)[0].(map[string]any)
	context := agent["context"].(map[string]any)
	channel := context["channel"].(map[string]any)
	if channel["id"] != m.ChannelID || channel["kind"] != "private" || channel["peer_agent_id"] != f.ids["alice"] || len(channel) != 3 {
		t.Fatal(context)
	}
	if !reflect.DeepEqual(context["sender"], map[string]any{"id": f.ids["alice"], "name": "alice"}) {
		t.Fatal(context)
	}
	delete(agent, "context")
	if !reflect.DeepEqual(agent, legacy) {
		t.Fatalf("legacy representation changed:\nagent %v\nadmin %v", agent, legacy)
	}
	if _, ok := legacy["created_at"].(string); !ok {
		t.Fatal(legacy)
	}
	if legacy["attachment"].(map[string]any)["filename"] != "example.bin" {
		t.Fatal(legacy)
	}
	after := f.must("GET", "/api/v1/inbox", f.keys["bob"], nil)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("history modified inbox: %v %v", before, after)
	}
	claimed := f.must("POST", "/api/v1/inbox/claim", f.keys["bob"], map[string]any{})["messages"].([]any)[0].(map[string]any)
	if !reflect.DeepEqual(claimed["context"], context) {
		t.Fatal(claimed)
	}
	delete(claimed, "context")
	delete(claimed, "claim_token")
	delete(claimed, "claim_expires_at")
	if !reflect.DeepEqual(claimed, legacy) {
		t.Fatalf("claim changed old fields: %v %v", claimed, legacy)
	}
	features := f.must("GET", "/api/v1/limits", f.keys["bob"], nil)["features"].(map[string]any)
	if !reflect.DeepEqual(features, map[string]any{"message_context_v1": true, "route_reply_v1": true, "ack_read_v1": true}) {
		t.Fatal(features)
	}
	var version int
	if err := f.s.db.QueryRow("SELECT version FROM schema_version").Scan(&version); err != nil || version != 1 {
		t.Fatal(version, err)
	}
}

func TestRouteReplyRoutingAndRetention(t *testing.T) {
	f := setup(t)
	first := oneResult(t, f.send("parent one", f.ids["bob"]))
	second := oneResult(t, f.send("parent two", f.ids["bob"]))
	parent1, parent2 := first["message_id"].(string), second["message_id"].(string)
	path := func(id string) string { return "/api/v1/messages/" + id + "/reply" }
	// Reply to one's own old message uses the other participant, not the sender.
	own := f.must("POST", path(parent1), f.keys["alice"], map[string]any{"body": "own reply"})
	if own["status"] != "sent" || own["channel_id"] != first["channel_id"] || len(own) != 3 {
		t.Fatal(own)
	}
	if f.s.st.Deliveries[f.ids["bob"]+"/"+own["message_id"].(string)] == nil {
		t.Fatal("reply went to wrong peer")
	}
	bob := f.must("POST", path(parent1), f.keys["bob"], map[string]any{"body": "done"})
	deliveries := len(f.s.st.Deliveries)
	dup := f.must("POST", path(parent2), f.keys["bob"], map[string]any{"body": "done"})
	if bob["status"] != "sent" || dup["status"] != "duplicate" || dup["message_id"] != bob["message_id"] || dup["channel_id"] != bob["channel_id"] || len(dup) != 3 {
		t.Fatal(bob, dup)
	}
	if len(f.s.st.Deliveries) != deliveries {
		t.Fatal("duplicate created a delivery")
	}
	f.must("POST", path(parent1), f.keys["bob"], map[string]any{"body": "other"})
	if result := f.must("POST", path(parent2), f.keys["bob"], map[string]any{"body": "done"}); result["status"] != "sent" {
		t.Fatal(result)
	}
	status, out := f.call("POST", path(parent1), f.keys["carol"], map[string]any{"body": "x"})
	checkError(t, status, out, 404, "message_unavailable")
	status, out = f.call("POST", path("missing"), f.keys["carol"], map[string]any{"body": "x"})
	checkError(t, status, out, 404, "message_unavailable")
	status, out = f.call("POST", path(parent1), "wrong-key", map[string]any{"body": "x"})
	checkError(t, status, out, 401, "unauthorized")
	target := f.s.st.Agents[f.ids["alice"]]
	target.Enabled = false
	status, out = f.call("POST", path(parent1), f.keys["bob"], map[string]any{"body": "x"})
	checkError(t, status, out, 400, "invalid_target")
	target.Enabled = true
	host := f.s.st.Hosts[target.HostID]
	host.Enabled = false
	// The fixture shares one host. Put Bob on a separate enabled host so his authentication remains valid.
	f.s.st.Hosts["host_other"] = &Host{ID: "host_other", Enabled: true, IPs: []string{"192.0.2.10"}}
	f.s.st.Agents[f.ids["bob"]].HostID = "host_other"
	status, out = f.call("POST", path(parent1), f.keys["bob"], map[string]any{"body": "x"})
	checkError(t, status, out, 400, "invalid_target")
	host.Enabled = true
	settings := f.s.st.Settings
	settings.MessageCount = 1
	f.must("PUT", "/admin/api/settings", testPassword, settings)
	status, out = f.call("POST", path(parent1), f.keys["bob"], map[string]any{"body": "x"})
	checkError(t, status, out, 404, "message_unavailable")
}

func TestRouteReplyGroupPermissionAndContext(t *testing.T) {
	f := setup(t)
	group := f.must("POST", "/api/v1/groups", f.keys["alice"], map[string]any{"name": "team", "invitee_ids": []string{f.ids["bob"]}})["channel_id"].(string)
	parent := f.must("POST", "/api/v1/groups/"+group+"/messages", f.keys["alice"], map[string]any{"body": "before"})["message_id"].(string)
	path := "/api/v1/messages/" + parent + "/reply"
	status, out := f.call("POST", path, f.keys["bob"], map[string]any{"body": "pending"})
	checkError(t, status, out, 404, "message_unavailable")
	f.must("POST", "/api/v1/groups/"+group+"/join", f.keys["bob"], nil)
	ctx := f.must("GET", "/api/v1/channels/"+group+"/messages", f.keys["bob"], nil)["messages"].([]any)[0].(map[string]any)["context"].(map[string]any)
	if !reflect.DeepEqual(ctx["channel"], map[string]any{"id": group, "kind": "group", "group_id": group}) {
		t.Fatal(ctx)
	}
	ack := func(id string) map[string]any {
		return oneResult(t, f.must("POST", "/api/v1/inbox/ack", f.keys["bob"], map[string]any{"acks": []any{map[string]any{"message_id": id, "claim_token": "opaque"}}}))
	}
	if result := ack(parent); result["status"] != "message_purged" || result["read"] != nil {
		t.Fatal("pre-join history has no delivery", result)
	}
	if result := f.must("POST", path, f.keys["bob"], map[string]any{"body": "joined"}); result["status"] != "sent" || result["channel_id"] != group {
		t.Fatal(result)
	}
	f.must("POST", "/api/v1/groups/"+group+"/leave", f.keys["bob"], nil)
	status, out = f.call("POST", path, f.keys["bob"], map[string]any{"body": "left"})
	checkError(t, status, out, 404, "message_unavailable")
	if result := ack(parent); result["status"] != "message_purged" || result["read"] != nil {
		t.Fatal("left member has no delivery", result)
	}
}

func TestRouteReplyInputAndAttachmentPreprocessing(t *testing.T) {
	f := setup(t)
	parent := oneResult(t, f.send("parent", f.ids["bob"]))["message_id"].(string)
	path := "/api/v1/messages/" + parent + "/reply"
	for _, tc := range []struct{ body, code string }{
		{`{"body":""}`, "empty_message"}, {`{"body":"ok","to_agent_ids":["x"]}`, "invalid_json"}, {`{"body":"ok","file_path":"x"}`, "invalid_json"}, {`{"body":"ok","parent_id":"x"}`, "invalid_json"}, {`{}`, "invalid_json"}, {`{"body":null}`, "invalid_json"}, {`null`, "invalid_json"}, {`[]`, "invalid_json"}, {`{"body":"\ud800"}`, "invalid_unicode"}, {string([]byte{0xff}), "invalid_unicode"}, {`{"body":"one","body":"two"}`, "invalid_json"}, {`{"Body":"text"}`, "invalid_json"},
	} {
		r := httptest.NewRequest("POST", path, strings.NewReader(tc.body))
		r.RemoteAddr = "192.0.2.10:1"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+f.keys["bob"])
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		var out map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		checkError(t, w.Code, out, 400, tc.code)
	}
	settings := f.s.st.Settings
	settings.BodyCodepoints = 1
	f.must("PUT", "/admin/api/settings", testPassword, settings)
	status, out := f.call("POST", path, f.keys["bob"], map[string]any{"body": "😀😀"})
	checkError(t, status, out, 400, "text_too_long")
	if e := out["error"].(map[string]any); e["limit"] != float64(1) || e["settings_revision"] == nil {
		t.Fatal(e)
	}
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	_ = mw.WriteField("metadata", `{"body":"x"}`)
	part, _ := mw.CreateFormFile("file", "example.bin")
	_, _ = part.Write([]byte("data"))
	_ = mw.Close()
	source := &countedReader{r: &b}
	r := httptest.NewRequest("POST", path, source)
	r.RemoteAddr = "192.0.2.10:1"
	r.Header.Set("Authorization", "Bearer "+f.keys["bob"])
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	var response map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &response)
	checkError(t, w.Code, response, 400, "invalid_json")
	if source.reads != 0 {
		t.Fatalf("multipart reply was read before rejection: %d reads", source.reads)
	}
	for _, tc := range []struct {
		key, addr, code string
		status          int
	}{
		{"invalid-key", "192.0.2.10:1", "unauthorized", 401},
		{f.keys["bob"], "198.51.100.20:1", "source_ip_denied", 403},
	} {
		unauthorized := httptest.NewRequest("POST", path, strings.NewReader("unread multipart body"))
		unauthorized.RemoteAddr = tc.addr
		unauthorized.Header.Set("Authorization", "Bearer "+tc.key)
		unauthorized.Header.Set("Content-Type", mw.FormDataContentType())
		recorder := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(recorder, unauthorized)
		var body map[string]any
		_ = json.Unmarshal(recorder.Body.Bytes(), &body)
		checkError(t, recorder.Code, body, tc.status, tc.code)
	}
	for _, name := range []string{"staging", "files"} {
		entries, err := os.ReadDir(filepath.Join(f.s.opts.DataDir, name))
		if err != nil || len(entries) != 0 {
			t.Fatal(name, entries, err)
		}
	}
	// Existing direct and group uploads continue to materialize attachments.
	settings.BodyCodepoints = 8192
	f.must("PUT", "/admin/api/settings", testPassword, settings)
	f.upload([]byte("direct"), []string{f.ids["bob"]})
	group := f.must("POST", "/api/v1/groups", f.keys["alice"], map[string]any{"name": "team"})["channel_id"].(string)
	b.Reset()
	mw = multipart.NewWriter(&b)
	_ = mw.WriteField("metadata", `{"body":"group"}`)
	part, _ = mw.CreateFormFile("file", "example.bin")
	_, _ = part.Write([]byte("group"))
	_ = mw.Close()
	r = httptest.NewRequest("POST", "/api/v1/groups/"+group+"/messages", &b)
	r.RemoteAddr = "192.0.2.10:1"
	r.Header.Set("Authorization", "Bearer "+f.keys["alice"])
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w = httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if m := f.s.st.Messages[len(f.s.st.Messages)-1]; m.Attachment == nil || m.ChannelID != group {
		t.Fatal(m)
	}
}

func TestAckReadPreservesFencingOrder(t *testing.T) {
	f := setup(t)
	parent := oneResult(t, f.send("one", f.ids["bob"]))["message_id"].(string)
	claim := f.must("POST", "/api/v1/inbox/claim", f.keys["bob"], map[string]any{})["messages"].([]any)[0].(map[string]any)
	token := claim["claim_token"].(string)
	ack := func(id, token string) map[string]any {
		return oneResult(t, f.must("POST", "/api/v1/inbox/ack", f.keys["bob"], map[string]any{"acks": []any{map[string]any{"message_id": id, "claim_token": token}}}))
	}
	if v := ack(parent, "wrong"); v["status"] != "claim_replaced" || v["read"] != false {
		t.Fatal(v)
	}
	if v := ack(parent, token); v["status"] != "read" || v["read"] != true {
		t.Fatal(v)
	}
	if v := ack(parent, token); v["status"] != "already_read" || v["read"] != true {
		t.Fatal(v)
	}
	if v := ack(parent, "wrong"); v["status"] != "claim_replaced" || v["read"] != true {
		t.Fatal(v)
	}
	f.s.now = func() time.Time { return time.Now().Add(61 * time.Second) }
	if v := ack(parent, token); v["status"] != "claim_expired" || v["read"] != true {
		t.Fatal(v)
	}
	if v := ack("missing", token); v["status"] != "message_purged" || v["read"] != nil {
		t.Fatal(v)
	}
	f.send("two", f.ids["bob"])
	claim = f.must("POST", "/api/v1/inbox/claim", f.keys["bob"], map[string]any{})["messages"].([]any)[0].(map[string]any)
	second, old := claim["id"].(string), claim["claim_token"].(string)
	f.s.now = func() time.Time { return time.Now().Add(122 * time.Second) }
	if v := ack(second, old); v["status"] != "claim_expired" || v["read"] != false {
		t.Fatal(v)
	}
	claim = f.must("POST", "/api/v1/inbox/claim", f.keys["bob"], map[string]any{})["messages"].([]any)[0].(map[string]any)
	if v := ack(second, old); v["status"] != "claim_replaced" || v["read"] != false {
		t.Fatal(v)
	}
	if v := ack(second, claim["claim_token"].(string)); v["status"] != "read" || v["read"] != true {
		t.Fatal(v)
	}
}

type gatedReader struct {
	started chan struct{}
	release chan struct{}
	body    *strings.Reader
	once    sync.Once
}

func (g *gatedReader) Read(p []byte) (int, error) {
	g.once.Do(func() {
		close(g.started)
		<-g.release
	})
	return g.body.Read(p)
}

func TestRouteReplyRechecksAtCommit(t *testing.T) {
	for _, scenario := range []string{"purge", "leave", "disabled_target"} {
		t.Run(scenario, func(t *testing.T) {
			f := setup(t)
			var parent string
			if scenario == "leave" {
				group := f.must("POST", "/api/v1/groups", f.keys["alice"], map[string]any{"name": "team", "invitee_ids": []string{f.ids["bob"]}})["channel_id"].(string)
				f.must("POST", "/api/v1/groups/"+group+"/join", f.keys["bob"], nil)
				parent = f.must("POST", "/api/v1/groups/"+group+"/messages", f.keys["alice"], map[string]any{"body": "parent"})["message_id"].(string)
			} else {
				parent = oneResult(t, f.send("parent", f.ids["bob"]))["message_id"].(string)
			}
			gate := &gatedReader{started: make(chan struct{}), release: make(chan struct{}), body: strings.NewReader(`{"body":"reply"}`)}
			r := httptest.NewRequest("POST", "/api/v1/messages/"+parent+"/reply", gate)
			r.RemoteAddr = "192.0.2.10:1"
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer "+f.keys["bob"])
			w := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { f.s.Handler().ServeHTTP(w, r); close(done) }()
			<-gate.started
			switch scenario {
			case "purge":
				settings := f.s.st.Settings
				settings.MessageCount = 1
				f.must("PUT", "/admin/api/settings", testPassword, settings)
				f.send("evict", f.ids["bob"])
			case "leave":
				group := f.s.st.Messages[0].ChannelID
				f.must("POST", "/api/v1/groups/"+group+"/leave", f.keys["bob"], nil)
			case "disabled_target":
				f.s.mu.Lock()
				f.s.st.Agents[f.ids["alice"]].Enabled = false
				f.s.mu.Unlock()
			}
			close(gate.release)
			<-done
			var out map[string]any
			_ = json.Unmarshal(w.Body.Bytes(), &out)
			if scenario == "disabled_target" {
				checkError(t, w.Code, out, 400, "invalid_target")
			} else {
				checkError(t, w.Code, out, 404, "message_unavailable")
			}
		})
	}
}

func TestRouteReplyPurgeAndSourceIP(t *testing.T) {
	f := setup(t)
	parent := oneResult(t, f.send("a", f.ids["bob"]))["message_id"].(string)
	path := "/api/v1/messages/" + parent + "/reply"
	first := f.must("POST", path, f.keys["bob"], map[string]any{"body": "long"})
	settings := f.s.st.Settings
	settings.BodyCodepoints = 1
	f.must("PUT", "/admin/api/settings", testPassword, settings)
	settings.BodyCodepoints = 8192
	f.must("PUT", "/admin/api/settings", testPassword, settings)
	again := f.must("POST", path, f.keys["bob"], map[string]any{"body": "long"})
	if again["status"] != "sent" || again["message_id"] == first["message_id"] {
		t.Fatal(first, again)
	}
	r := httptest.NewRequest("POST", path, strings.NewReader(`{"body":"hi"}`))
	r.RemoteAddr = "198.51.100.20:1"
	r.Header.Set("Authorization", "Bearer "+f.keys["bob"])
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Forwarded-For", "192.0.2.10")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	checkError(t, w.Code, out, 403, "source_ip_denied")
}

func TestLegacyRepresentationFreeze(t *testing.T) {
	f := setup(t)
	f.s.now = func() time.Time { return time.Date(2025, 1, 2, 3, 4, 5, 123000000, time.UTC) }
	// Exercise integer representation beyond the exact float64 range.
	f.s.st.Sequence = 9007199254740992
	f.upload([]byte("synthetic attachment"), []string{f.ids["bob"]})
	m := f.s.st.Messages[0]
	r := httptest.NewRequest("GET", "/api/v1/channels/"+m.ChannelID+"/messages", nil)
	r.RemoteAddr = "192.0.2.10:1"
	r.Header.Set("Authorization", "Bearer "+f.keys["bob"])
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	dec := json.NewDecoder(bytes.NewReader(w.Body.Bytes()))
	dec.UseNumber()
	var result map[string]any
	if err := dec.Decode(&result); err != nil {
		t.Fatal(err)
	}
	got := result["messages"].([]any)[0].(map[string]any)
	if got["id"] != m.ID || got["sequence"] != json.Number("9007199254740993") || got["channel_id"] != m.ChannelID || got["sender_id"] != f.ids["alice"] || got["created_at"] != "2025-01-02T03:04:05.123Z" || got["body"] != "binary" {
		t.Fatal(got)
	}
	file := got["attachment"].(map[string]any)
	if len(file) != 5 || file["id"] != m.Attachment.ID || file["filename"] != "example.bin" || file["media_type"] != "application/octet-stream" || file["size"] != json.Number("20") || file["sha256"] != m.Attachment.SHA256 {
		t.Fatal(file)
	}
	channels := f.must("GET", "/api/v1/channels", f.keys["bob"], nil)["channels"].([]any)
	ch := channels[0].(map[string]any)
	if ch["id"] != m.ChannelID || ch["kind"] != "private" || len(ch["agent_ids"].([]any)) != 2 || ch["group_id"] != nil {
		t.Fatal(ch)
	}
	resultSend := f.send("two", f.ids["bob"], "missing")
	if oneResult(t, resultSend)["status"] != "sent" || resultSend["results"].([]any)[1].(map[string]any)["status"] != "rejected" {
		t.Fatal(resultSend)
	}
}

func TestAckReadConcurrentSessions(t *testing.T) {
	f := setup(t)
	parent := oneResult(t, f.send("hello", f.ids["bob"]))["message_id"].(string)
	message := f.must("POST", "/api/v1/inbox/claim", f.keys["bob"], map[string]any{})["messages"].([]any)[0].(map[string]any)
	body, _ := json.Marshal(map[string]any{"acks": []any{map[string]any{"message_id": parent, "claim_token": message["claim_token"]}}})
	responses := make(chan map[string]any, 2)
	for i := 0; i < 2; i++ {
		go func() {
			r := httptest.NewRequest("POST", "/api/v1/inbox/ack", bytes.NewReader(body))
			r.RemoteAddr = "192.0.2.10:1"
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Authorization", "Bearer "+f.keys["bob"])
			w := httptest.NewRecorder()
			f.s.Handler().ServeHTTP(w, r)
			var v map[string]any
			_ = json.Unmarshal(w.Body.Bytes(), &v)
			responses <- v
		}()
	}
	statuses := map[string]int{}
	for i := 0; i < 2; i++ {
		v := oneResult(t, <-responses)
		if v["read"] != true {
			t.Fatal(v)
		}
		statuses[v["status"].(string)]++
	}
	if statuses["read"] != 1 || statuses["already_read"] != 1 {
		t.Fatal(statuses)
	}
	own := oneResult(t, f.must("POST", "/api/v1/inbox/ack", f.keys["alice"], map[string]any{"acks": []any{map[string]any{"message_id": parent, "claim_token": "anything"}}}))
	if own["status"] != "message_purged" || own["read"] != nil {
		t.Fatal(own)
	}
}

func TestRouteReplyRollbackOnSaveFailure(t *testing.T) {
	f := setup(t)
	parent := oneResult(t, f.send("parent", f.ids["bob"]))["message_id"].(string)
	before, _ := json.Marshal(f.s.st)
	if _, err := f.s.db.Exec("CREATE TRIGGER reject_reply BEFORE INSERT ON records WHEN NEW.kind='message' BEGIN SELECT RAISE(ABORT,'synthetic failure'); END"); err != nil {
		t.Fatal(err)
	}
	status, out := f.call("POST", "/api/v1/messages/"+parent+"/reply", f.keys["bob"], map[string]any{"body": "reply"})
	checkError(t, status, out, 500, "internal_error")
	after, _ := json.Marshal(f.s.st)
	if !bytes.Equal(before, after) {
		t.Fatal("failed reply mutated state")
	}
	if _, err := f.s.db.Exec("DROP TRIGGER reject_reply"); err != nil {
		t.Fatal(err)
	}
	if out := f.must("POST", "/api/v1/messages/"+parent+"/reply", f.keys["bob"], map[string]any{"body": "reply"}); out["status"] != "sent" {
		t.Fatal(out)
	}
}

func TestAgentContextFanoutPrivacyAndGroupDelivery(t *testing.T) {
	f := setup(t)
	results := f.send("fanout", f.ids["bob"], f.ids["carol"])["results"].([]any)
	for i, name := range []string{"bob", "carol"} {
		other := "carol"
		if name == "carol" {
			other = "bob"
		}
		channel := results[i].(map[string]any)["channel_id"].(string)
		message := f.must("GET", "/api/v1/channels/"+channel+"/messages", f.keys[name], nil)["messages"].([]any)[0].(map[string]any)
		data, _ := json.Marshal(message["context"])
		if strings.Contains(string(data), f.ids[other]) || strings.Contains(string(data), other) {
			t.Fatal("fanout identity leaked", string(data))
		}
	}
	group := f.must("POST", "/api/v1/groups", f.keys["alice"], map[string]any{"name": "team", "invitee_ids": []string{f.ids["bob"]}})["channel_id"].(string)
	f.must("POST", "/api/v1/groups/"+group+"/join", f.keys["bob"], nil)
	parent := f.must("POST", "/api/v1/groups/"+group+"/messages", f.keys["alice"], map[string]any{"body": "parent"})["message_id"].(string)
	f.s.st.Agents[f.ids["bob"]].Enabled = false
	result := f.must("POST", "/api/v1/messages/"+parent+"/reply", f.keys["alice"], map[string]any{"body": "reply"})
	if f.s.st.Deliveries[f.ids["bob"]+"/"+result["message_id"].(string)] == nil {
		t.Fatal("group reply filtered active disabled member")
	}
}
