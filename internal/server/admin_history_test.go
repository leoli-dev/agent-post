package server

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestAdminHistoryAuthorizationAndReadOnly(t *testing.T) {
	f := setup(t)
	for _, body := range []string{"claimed", "read", "unclaimed"} {
		f.send(body, f.ids["bob"])
	}
	channel := f.s.st.Messages[0].ChannelID
	f.must("POST", "/api/v1/inbox/claim", f.keys["bob"], map[string]any{})
	claimed := f.must("POST", "/api/v1/inbox/claim", f.keys["bob"], map[string]any{})["messages"].([]any)[0].(map[string]any)
	f.must("POST", "/api/v1/inbox/ack", f.keys["bob"], map[string]any{"acks": []any{map[string]any{"message_id": claimed["id"], "claim_token": claimed["claim_token"]}}})
	f.upload([]byte("retained attachment"), []string{f.ids["bob"]})
	group := f.must("POST", "/api/v1/groups", f.keys["alice"], map[string]any{"name": "Review room", "invitee_ids": []string{f.ids["bob"], f.ids["carol"]}})["channel_id"].(string)
	f.must("POST", "/api/v1/groups/"+group+"/messages", f.keys["alice"], map[string]any{"body": "group history"})
	f.must("POST", "/api/v1/groups/"+group+"/join", f.keys["carol"], nil)
	f.must("POST", "/api/v1/groups/"+group+"/leave", f.keys["carol"], nil)
	if status, _ := f.call("GET", "/api/v1/channels/"+channel+"/messages", f.keys["carol"], nil); status != 403 {
		t.Fatalf("unrelated agent history: %d", status)
	}
	if status, _ := f.call("GET", "/api/v1/channels/"+group+"/messages", f.keys["bob"], nil); status != 403 {
		t.Fatalf("pending member history: %d", status)
	}
	if status, _ := f.call("GET", "/api/v1/channels/"+group+"/messages", f.keys["carol"], nil); status != 403 {
		t.Fatalf("departed member history: %d", status)
	}
	paths := []string{"/admin/api/channels", "/admin/api/channels/" + channel + "/messages", "/admin/api/channels/" + group + "/messages"}
	for _, path := range paths {
		for _, key := range []string{"", f.keys["alice"], "incorrect-admin-password"} {
			if status, out := f.call("GET", path, key, nil); status != 401 || len(out) != 1 || out["error"] == nil {
				t.Fatalf("unauthorized history %s: %d %v", path, status, out)
			}
		}
	}
	// Administrator access remains available after agents and their host are disabled.
	f.must("PATCH", "/admin/api/agents/"+f.ids["bob"], testPassword, map[string]any{"enabled": false})
	host := f.s.st.Agents[f.ids["alice"]].HostID
	f.must("PATCH", "/admin/api/hosts/"+host, testPassword, map[string]any{"enabled": false})
	login := httptest.NewRequest("POST", "/admin/api/login", strings.NewReader(`{"password":"`+testPassword+`"}`))
	loggedIn := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(loggedIn, login)
	if loggedIn.Code != 200 || len(loggedIn.Result().Cookies()) != 1 {
		t.Fatalf("login: %d %s", loggedIn.Code, loggedIn.Body.String())
	}
	cookie := loggedIn.Result().Cookies()[0]
	before, _ := json.Marshal(f.s.st)
	for _, path := range paths {
		f.must("GET", path, testPassword, nil)
		request := httptest.NewRequest("GET", path, nil)
		request.AddCookie(cookie) // Read-only browser requests require no CSRF header.
		response := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatalf("cookie history %s: %d %s", path, response.Code, response.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if messages, ok := out["messages"].([]any); ok {
			for _, value := range messages {
				message := value.(map[string]any)
				for _, field := range []string{"claim_token", "claim_expires_at", "token", "read"} {
					if _, exists := message[field]; exists {
						t.Fatalf("private delivery field %s exposed: %v", field, message)
					}
				}
			}
		}
	}
	after, _ := json.Marshal(f.s.st)
	if !bytes.Equal(before, after) {
		t.Fatal("administrator history changed retained state, delivery claims, or read state")
	}
	history := f.must("GET", paths[1], testPassword, nil)["messages"].([]any)
	if len(history) != 4 || history[3].(map[string]any)["attachment"].(map[string]any)["filename"] != "example.bin" {
		t.Fatalf("attachment metadata missing: %v", history)
	}
	f.s.now = func() time.Time { return time.Now().Add(9 * time.Hour) }
	expired := httptest.NewRequest("GET", paths[1], nil)
	expired.AddCookie(cookie)
	response := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(response, expired)
	if response.Code != 401 {
		t.Fatalf("expired cookie history: %d", response.Code)
	}
}

func TestAdminChannelListPaginationAndSearch(t *testing.T) {
	f := setup(t)
	f.send("first", f.ids["bob"], f.ids["carol"])
	bobChannel := f.s.st.Messages[0].ChannelID
	f.upload([]byte("attachment"), []string{f.ids["bob"]})
	review := f.must("POST", "/api/v1/groups", f.keys["alice"], map[string]any{"name": "Review room", "invitee_ids": []string{f.ids["bob"]}})["channel_id"].(string)
	quiet := f.must("POST", "/api/v1/groups", f.keys["alice"], map[string]any{"name": "Quiet room", "invitee_ids": []string{f.ids["carol"]}})["channel_id"].(string)
	f.must("PATCH", "/admin/api/agents/"+f.ids["bob"], testPassword, map[string]any{"enabled": false})
	var ids []string
	byID := map[string]map[string]any{}
	next := ""
	for {
		out := f.must("GET", "/admin/api/channels?limit=1&cursor="+next, testPassword, nil)
		channels := out["channels"].([]any)
		if len(channels) != 1 {
			t.Fatalf("channel page: %v", out)
		}
		channel := channels[0].(map[string]any)
		id := channel["id"].(string)
		if byID[id] != nil {
			t.Fatalf("repeated channel: %s", id)
		}
		byID[id] = channel
		ids = append(ids, id)
		next = out["next_cursor"].(string)
		if next == "" {
			break
		}
	}
	if len(ids) != 4 || !sort.StringsAreSorted(ids) {
		t.Fatalf("channel order: %v", ids)
	}
	if got := byID[bobChannel]; got["message_count"] != float64(2) || got["file_count"] != float64(1) || got["last_message_at"] == nil || len(got["agent_ids"].([]any)) != 2 {
		t.Fatalf("private summary: %v", got)
	}
	for _, id := range []string{review, quiet} {
		if got := byID[id]; got["group_id"] != id || got["message_count"] != float64(0) || got["file_count"] != float64(0) || got["last_message_at"] != nil {
			t.Fatalf("empty group summary: %v", got)
		}
	}
	for _, test := range []struct {
		query string
		want  []string
	}{
		{"kind=private", []string{bobChannel, f.s.st.Messages[1].ChannelID}},
		{"kind=group", []string{review, quiet}},
		{"q=rEvIeW", []string{review}},
		{"q=" + strings.ToUpper(review), []string{review}},
		{"q=" + strings.ToUpper(bobChannel), []string{bobChannel}},
		{"q=BOB", []string{bobChannel, review}},
		{"q=" + strings.ToUpper(f.ids["bob"]), []string{bobChannel, review}},
		{"kind=group&q=BOB", []string{review}},
		{"q=missing-channel", []string{}},
	} {
		out := f.must("GET", "/admin/api/channels?"+test.query, testPassword, nil)
		got := []string{}
		for _, value := range out["channels"].([]any) {
			got = append(got, value.(map[string]any)["id"].(string))
		}
		sort.Strings(test.want)
		if !reflect.DeepEqual(got, test.want) || out["next_cursor"] != "" {
			t.Fatalf("query %s: %v, want %v", test.query, out, test.want)
		}
	}
	first := f.must("GET", "/admin/api/channels?kind=group&q=ROOM&limit=1", testPassword, nil)
	second := f.must("GET", "/admin/api/channels?kind=group&q=ROOM&limit=1&cursor="+first["next_cursor"].(string), testPassword, nil)
	if first["next_cursor"] == "" || len(second["channels"].([]any)) != 1 || second["next_cursor"] != "" || first["channels"].([]any)[0].(map[string]any)["id"] == second["channels"].([]any)[0].(map[string]any)["id"] {
		t.Fatalf("filtered pagination: %v %v", first, second)
	}
	for _, test := range []struct{ query, code string }{{"limit=0", "invalid_limit"}, {"limit=101", "invalid_limit"}, {"cursor=%", "invalid_cursor"}, {"kind=unknown", "invalid_kind"}} {
		path := "/admin/api/channels?" + test.query
		if test.query == "cursor=%" {
			path = "/admin/api/channels?cursor=" + url.QueryEscape("%")
		}
		status, out := f.call("GET", path, testPassword, nil)
		if status != 400 || out["error"].(map[string]any)["code"] != test.code {
			t.Fatalf("invalid list query %s: %d %v", test.query, status, out)
		}
	}
}

func TestAdminHistoryPaginationAndRetention(t *testing.T) {
	f := setup(t)
	for _, body := range []string{"old", "middle", "new"} {
		f.send(body, f.ids["bob"])
	}
	f.send("other channel", f.ids["carol"])
	f.upload([]byte("file"), []string{f.ids["bob"]})
	channel := f.s.st.Messages[0].ChannelID
	path := "/admin/api/channels/" + channel + "/messages"
	for _, order := range []string{"oldest", "newest"} {
		bodies := []string{}
		next := ""
		for {
			out := f.must("GET", path+"?limit=1&order="+order+"&cursor="+next, testPassword, nil)
			messages := out["messages"].([]any)
			if len(messages) != 1 {
				t.Fatalf("history page: %v", out)
			}
			message := messages[0].(map[string]any)
			if message["channel_id"] != channel {
				t.Fatalf("cross-channel history: %v", message)
			}
			bodies = append(bodies, message["body"].(string))
			next = out["next_cursor"].(string)
			if next == "" {
				break
			}
		}
		want := []string{"old", "middle", "new", "binary"}
		if order == "newest" {
			want = []string{"binary", "new", "middle", "old"}
		}
		if !reflect.DeepEqual(bodies, want) {
			t.Fatalf("%s history: %v", order, bodies)
		}
	}
	for _, test := range []struct{ query, code string }{{"limit=101", "invalid_limit"}, {"order=unknown", "invalid_order"}, {"cursor=" + cursor("not-a-sequence"), "invalid_cursor"}, {"cursor=" + url.QueryEscape("%"), "invalid_cursor"}} {
		status, out := f.call("GET", path+"?"+test.query, testPassword, nil)
		if status != 400 || out["error"].(map[string]any)["code"] != test.code {
			t.Fatalf("invalid history query %s: %d %v", test.query, status, out)
		}
	}
	if status, _ := f.call("GET", "/admin/api/channels/missing/messages", testPassword, nil); status != 404 {
		t.Fatalf("missing channel: %d", status)
	}
	first := f.must("GET", path+"?limit=1", testPassword, nil)
	settings := f.s.st.Settings
	settings.MessageCount = 2
	f.must("PUT", "/admin/api/settings", testPassword, settings)
	page := f.must("GET", path+"?cursor="+first["next_cursor"].(string), testPassword, nil)
	if messages := page["messages"].([]any); len(messages) != 2 || messages[0].(map[string]any)["body"] != "new" || messages[1].(map[string]any)["body"] != "binary" {
		t.Fatalf("cursor after retention: %v", page)
	}
	settings.FileCount = 0
	f.must("PUT", "/admin/api/settings", testPassword, settings)
	retained := f.must("GET", path, testPassword, nil)["messages"].([]any)
	if len(retained) != 1 || retained[0].(map[string]any)["body"] != "new" {
		t.Fatalf("coupled file/message deletion: %v", retained)
	}
	settings.BodyCodepoints = 1
	f.must("PUT", "/admin/api/settings", testPassword, settings)
	if messages := f.must("GET", path, testPassword, nil)["messages"].([]any); len(messages) != 0 {
		t.Fatalf("deleted messages remain visible: %v", messages)
	}
	summary := f.must("GET", "/admin/api/channels?q="+channel, testPassword, nil)["channels"].([]any)[0].(map[string]any)
	if summary["message_count"] != float64(0) || summary["file_count"] != float64(0) || summary["last_message_at"] != nil {
		t.Fatalf("retained counts after deletion: %v", summary)
	}
}
