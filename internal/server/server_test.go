package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testPassword = "synthetic-admin-password"

type fixture struct {
	t    *testing.T
	s    *Server
	keys map[string]string
	ids  map[string]string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	s, e := Open(Options{DataDir: t.TempDir(), AdminPassword: testPassword})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	f := &fixture{t, s, map[string]string{}, map[string]string{}}
	_, v := f.call("POST", "/admin/api/hosts", testPassword, map[string]any{"name": "example", "os": "test", "ips": []string{"192.0.2.10"}})
	h := v["host"].(map[string]any)["id"].(string)
	for _, name := range []string{"alice", "bob", "carol"} {
		_, v = f.call("POST", "/admin/api/agents", testPassword, map[string]any{"name": name, "host_id": h})
		f.keys[name] = v["api_key"].(string)
		f.ids[name] = v["agent"].(map[string]any)["id"].(string)
	}
	return f
}
func (f *fixture) call(method, path, key string, data any) (int, map[string]any) {
	f.t.Helper()
	var body io.Reader
	if data != nil {
		b, _ := json.Marshal(data)
		body = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, path, body)
	r.RemoteAddr = "192.0.2.10:5555"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+key)
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	out := map[string]any{}
	if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
		f.t.Fatalf("invalid response %s: %s", path, w.Body.String())
	}
	return w.Code, out
}
func (f *fixture) must(method, path, key string, data any) map[string]any {
	f.t.Helper()
	status, out := f.call(method, path, key, data)
	if status != 200 {
		f.t.Fatalf("%s: status %d: %v", path, status, out)
	}
	return out
}
func (f *fixture) send(body string, to ...string) map[string]any {
	return f.must("POST", "/api/v1/messages/direct", f.keys["alice"], map[string]any{"body": body, "to_agent_ids": to})
}
func TestAuthenticationAndAdminIsolation(t *testing.T) {
	f := setup(t)
	r := httptest.NewRequest("GET", "/api/v1/me", nil)
	r.RemoteAddr = "198.51.100.20:5"
	r.Header.Set("Authorization", "Bearer "+f.keys["alice"])
	r.Header.Set("X-Forwarded-For", "192.0.2.10")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	if status, _ := f.call("GET", "/admin/api/state", f.keys["alice"], nil); status != 401 {
		t.Fatal(status)
	}
	if status, _ := f.call("GET", "/api/v1/me", testPassword, nil); status != 401 {
		t.Fatal(status)
	}
	f.must("POST", "/admin/api/agents/"+f.ids["alice"]+"/revoke-key", testPassword, nil)
	if status, _ := f.call("GET", "/api/v1/me", f.keys["alice"], nil); status != 401 {
		t.Fatal(status)
	}
}
func TestDirectDedupClaimsAndFencing(t *testing.T) {
	f := setup(t)
	out := f.send("hello", f.ids["bob"], f.ids["carol"], f.ids["alice"], "missing")
	results := out["results"].([]any)
	if len(results) != 4 {
		t.Fatal(out)
	}
	if results[2].(map[string]any)["status"] != "rejected" {
		t.Fatal(out)
	}
	out = f.send("hello", f.ids["bob"])
	if out["results"].([]any)[0].(map[string]any)["status"] != "duplicate" {
		t.Fatal(out)
	}
	inbox := f.must("GET", "/api/v1/inbox", f.keys["bob"], nil)
	if inbox["claimable"] != float64(1) {
		t.Fatal(inbox)
	}
	claimed := f.must("POST", "/api/v1/inbox/claim", f.keys["bob"], map[string]any{})
	m := claimed["messages"].([]any)[0].(map[string]any)
	again := f.must("POST", "/api/v1/inbox/claim", f.keys["bob"], map[string]any{})
	if len(again["messages"].([]any)) != 0 {
		t.Fatal(again)
	}
	ack := map[string]any{"acks": []any{map[string]any{"message_id": m["id"], "claim_token": m["claim_token"]}}}
	f.s.now = func() time.Time { return time.Now().Add(61 * time.Second) }
	expired := f.must("POST", "/api/v1/inbox/ack", f.keys["bob"], ack)
	if expired["results"].([]any)[0].(map[string]any)["status"] != "claim_expired" {
		t.Fatal(expired)
	}
	claimed = f.must("POST", "/api/v1/inbox/claim", f.keys["bob"], map[string]any{})
	replaced := f.must("POST", "/api/v1/inbox/ack", f.keys["bob"], ack)
	if replaced["results"].([]any)[0].(map[string]any)["status"] != "claim_replaced" {
		t.Fatal(replaced)
	}
	m = claimed["messages"].([]any)[0].(map[string]any)
	ack = map[string]any{"acks": []any{map[string]any{"message_id": m["id"], "claim_token": m["claim_token"]}}}
	f.must("POST", "/api/v1/inbox/ack", f.keys["bob"], ack)
	inbox = f.must("GET", "/api/v1/inbox", f.keys["bob"], nil)
	if inbox["unread"] != float64(0) {
		t.Fatal(inbox)
	}
}
func TestConcurrentClaimAndSend(t *testing.T) {
	f := setup(t)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); f.send("concurrent", f.ids["bob"]) }()
	}
	wg.Wait()
	if len(f.s.st.Messages) != 1 {
		t.Fatal(len(f.s.st.Messages))
	}
	var count int
	var mu sync.Mutex
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out := f.must("POST", "/api/v1/inbox/claim", f.keys["bob"], map[string]any{})
			mu.Lock()
			count += len(out["messages"].([]any))
			mu.Unlock()
		}()
	}
	wg.Wait()
	if count != 1 {
		t.Fatal(count)
	}
}
func TestGroupsMembershipHistoryAndLeave(t *testing.T) {
	f := setup(t)
	out := f.must("POST", "/api/v1/groups", f.keys["alice"], map[string]any{"name": "team", "invitee_ids": []string{f.ids["bob"]}})
	id := out["channel_id"].(string)
	f.must("POST", "/api/v1/groups/"+id+"/messages", f.keys["alice"], map[string]any{"body": "before joining"})
	if status, _ := f.call("GET", "/api/v1/channels/"+id+"/messages", f.keys["bob"], nil); status != 403 {
		t.Fatal(status)
	}
	if status, _ := f.call("POST", "/api/v1/groups/"+id+"/join", f.keys["carol"], nil); status != 403 {
		t.Fatal(status)
	}
	f.must("POST", "/api/v1/groups/"+id+"/join", f.keys["bob"], nil)
	history := f.must("GET", "/api/v1/channels/"+id+"/messages", f.keys["bob"], nil)
	if len(history["messages"].([]any)) != 1 {
		t.Fatal(history)
	}
	inbox := f.must("GET", "/api/v1/inbox", f.keys["bob"], nil)
	if inbox["unread"] != float64(0) {
		t.Fatal(inbox)
	}
	f.must("POST", "/api/v1/groups/"+id+"/messages", f.keys["alice"], map[string]any{"body": "after joining"})
	f.must("POST", "/api/v1/inbox/claim", f.keys["bob"], map[string]any{})
	f.must("POST", "/api/v1/groups/"+id+"/leave", f.keys["bob"], nil)
	if len(f.s.st.Deliveries) != 0 {
		t.Fatal(f.s.st.Deliveries)
	}
	if status, _ := f.call("GET", "/api/v1/channels/"+id+"/messages", f.keys["bob"], nil); status != 403 {
		t.Fatal(status)
	}
	if status, _ := f.call("POST", "/api/v1/groups/"+id+"/leave", f.keys["alice"], nil); status != 409 {
		t.Fatal(status)
	}
}
func TestRetentionNoDedupFallbackAndPagination(t *testing.T) {
	f := setup(t)
	f.send("x", f.ids["bob"])
	f.send("oversized", f.ids["bob"])
	settings := f.s.st.Settings
	settings.BodyCodepoints = 1
	f.must("PUT", "/admin/api/settings", testPassword, settings)
	out := f.send("x", f.ids["bob"])
	if out["results"].([]any)[0].(map[string]any)["status"] != "sent" {
		t.Fatal(out)
	}
	ch := f.s.st.Messages[0].ChannelID
	first := f.must("GET", "/api/v1/channels/"+ch+"/messages?limit=1", f.keys["bob"], nil)
	next := first["next_cursor"].(string)
	if next == "" {
		t.Fatal(first)
	}
	second := f.must("GET", "/api/v1/channels/"+ch+"/messages?limit=1&cursor="+next, f.keys["bob"], nil)
	if len(second["messages"].([]any)) != 1 {
		t.Fatal(second)
	}
	settings.MessageCount = 1
	f.must("PUT", "/admin/api/settings", testPassword, settings)
	if len(f.s.st.Messages) != 1 {
		t.Fatal(len(f.s.st.Messages))
	}
	if status, _ := f.call("POST", "/api/v1/messages/direct", f.keys["alice"], map[string]any{"body": "😀😀", "to_agent_ids": []string{f.ids["bob"]}}); status != 400 {
		t.Fatal(status)
	}
}
func (f *fixture) upload(data []byte, to []string) map[string]any {
	f.t.Helper()
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	meta, _ := mw.CreateFormField("metadata")
	json.NewEncoder(meta).Encode(map[string]any{"body": "binary", "to_agent_ids": to})
	part, _ := mw.CreateFormFile("file", "example.bin")
	part.Write(data)
	mw.Close()
	r := httptest.NewRequest("POST", "/api/v1/messages/direct", &b)
	r.RemoteAddr = "192.0.2.10:1"
	r.Header.Set("Content-Type", mw.FormDataContentType())
	r.Header.Set("Authorization", "Bearer "+f.keys["alice"])
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		f.t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	v := map[string]any{}
	json.Unmarshal(w.Body.Bytes(), &v)
	return v
}
func TestAttachmentsIndependentCleanupAndRestart(t *testing.T) {
	f := setup(t)
	data := []byte{0, 1, 2, 255}
	f.upload(data, []string{f.ids["bob"], f.ids["carol"]})
	if len(f.s.st.Messages) != 2 {
		t.Fatal("messages")
	}
	first, second := f.s.st.Messages[0], f.s.st.Messages[1]
	if first.Attachment.ID == second.Attachment.ID {
		t.Fatal("shared attachment")
	}
	for _, m := range []*Message{first, second} {
		b, e := os.ReadFile(filepath.Join(f.s.opts.DataDir, "files", m.Attachment.ID))
		if e != nil || !bytes.Equal(b, data) {
			t.Fatal(e, b)
		}
	}
	settings := f.s.st.Settings
	settings.MessageCount = 1
	f.must("PUT", "/admin/api/settings", testPassword, settings)
	f.send("evict", first.ChannelID) // invalid target is isolated
	// Send directly to the participant owning the first channel.
	var target string
	for _, id := range f.s.st.Channels[first.ChannelID].AgentIDs {
		if id != f.ids["alice"] {
			target = id
		}
	}
	f.send("evict", target)
	if _, e := os.Stat(filepath.Join(f.s.opts.DataDir, "files", first.Attachment.ID)); !os.IsNotExist(e) {
		t.Fatal("first file retained", e)
	}
	if _, e := os.Stat(filepath.Join(f.s.opts.DataDir, "files", second.Attachment.ID)); e != nil {
		t.Fatal("second file removed", e)
	}
	dir := f.s.opts.DataDir
	if _, e := Open(Options{DataDir: dir, AdminPassword: testPassword}); e == nil {
		t.Fatal("second process allowed")
	}
	f.s.Close()
	s, e := Open(Options{DataDir: dir, AdminPassword: testPassword})
	if e != nil {
		t.Fatal(e)
	}
	f.s = s
	t.Cleanup(func() { s.Close() })
	if len(s.st.Messages) != 2 {
		t.Fatal(len(s.st.Messages))
	}
	if _, e := Open(Options{DataDir: dir, AdminPassword: "incorrect-password"}); e == nil {
		t.Fatal("wrong password accepted")
	}
}
func TestBrowserSessionCSRF(t *testing.T) {
	f := setup(t)
	b := strings.NewReader(`{"password":"` + testPassword + `"}`)
	r := httptest.NewRequest("POST", "/admin/api/login", b)
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal(cookies)
	}
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	for _, csrf := range []string{"", out["csrf_token"].(string)} {
		r = httptest.NewRequest("POST", "/admin/api/cleanup", nil)
		r.AddCookie(cookies[0])
		r.Header.Set("X-CSRF-Token", csrf)
		w = httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		want := 200
		if csrf == "" {
			want = 403
		}
		if w.Code != want {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
func TestCleanupFailureIsVisibleAndRecoverable(t *testing.T) {
	f := setup(t)
	f.upload([]byte("test"), []string{f.ids["bob"]})
	file := f.s.st.Messages[0].Attachment.ID
	path := filepath.Join(f.s.opts.DataDir, "files", file)
	os.Remove(path)
	os.Mkdir(path, 0700)
	os.WriteFile(filepath.Join(path, "busy"), []byte("busy"), 0600)
	settings := f.s.st.Settings
	settings.FileCount = 0
	out := f.must("PUT", "/admin/api/settings", testPassword, settings)
	_ = out
	if len(f.s.st.Messages) != 0 || len(f.s.st.Deletions) != 1 || f.s.cleanupError == "" {
		t.Fatal("cleanup not visible")
	}
	settings.FileCount = 1
	f.must("PUT", "/admin/api/settings", testPassword, settings)
	if _, e := os.Stat(path); e != nil {
		t.Fatal(e)
	}
	os.Remove(filepath.Join(path, "busy"))
	f.must("POST", "/admin/api/cleanup", testPassword, nil)
	if f.s.cleanupError != "" || len(f.s.st.Deletions) != 0 {
		t.Fatal("cleanup not recovered")
	}
}
func ExampleSettings() {
	fmt.Println(defaults().ClaimSeconds) // Output: 60
}
