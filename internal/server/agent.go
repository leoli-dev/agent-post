package server

import (
	"encoding/base64"

	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type download struct {
	file        *os.File
	metadata    *File
	disposition string
	created     time.Time
}

func page(r *http.Request, max int) (int, string, error) {
	n := max
	if v := r.URL.Query().Get("limit"); v != "" {
		var e error
		n, e = strconv.Atoi(v)
		if e != nil || n < 1 || n > max {
			return 0, "", bad("invalid_limit")
		}
	}
	cursor := ""
	if c := r.URL.Query().Get("cursor"); c != "" {
		b, e := base64.RawURLEncoding.DecodeString(c)
		if e != nil {
			return 0, "", bad("invalid_cursor")
		}
		cursor = string(b)
	}
	return n, cursor, nil
}
func cursor(v string) string { return base64.RawURLEncoding.EncodeToString([]byte(v)) }
func (s *Server) agent(w http.ResponseWriter, r *http.Request, a *storedAgent) (any, error) {
	p := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	if p == "me" && r.Method == "GET" {
		return a.Agent, nil
	}
	if p == "limits" && r.Method == "GET" {
		b := map[string]any{"settings": s.st.Settings, "deduplication": "latest retained sender/channel message; exact content; no time window; no fallback after purge", "attachment_deletion": "whole message and file deleted together", "retention_order": "oldest first", "rate_limit": nil, "total_storage_limit": nil}
		return b, nil
	}
	if p == "directory" && r.Method == "GET" {
		n, c, e := page(r, s.st.Settings.PageSize)
		if e != nil {
			return nil, e
		}
		ids := []string{}
		for id, v := range s.st.Agents {
			h := s.st.Hosts[v.HostID]
			if v.Enabled && h != nil && h.Enabled && id > c && (r.URL.Query().Get("host_id") == "" || r.URL.Query().Get("host_id") == v.HostID) && (r.URL.Query().Get("name") == "" || strings.Contains(strings.ToLower(v.Name), strings.ToLower(r.URL.Query().Get("name")))) {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		out := []any{}
		next := ""
		for i, id := range ids {
			if i == n {
				next = cursor(ids[i-1])
				break
			}
			v := s.st.Agents[id]
			out = append(out, map[string]any{"id": id, "name": v.Name, "host_id": v.HostID, "host": s.st.Hosts[v.HostID], "last_access": v.LastAccess})
		}
		return map[string]any{"agents": out, "next_cursor": next}, nil
	}
	if p == "channels" && r.Method == "GET" {
		n, c, e := page(r, s.st.Settings.PageSize)
		if e != nil {
			return nil, e
		}
		out := []*Channel{}
		for id, ch := range s.st.Channels {
			if s.authorized(a.ID, id) && id > c {
				out = append(out, ch)
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		next := ""
		if len(out) > n {
			next = cursor(out[n-1].ID)
			out = out[:n]
		}
		return map[string]any{"channels": out, "next_cursor": next}, nil
	}
	if p == "messages/direct" && r.Method == "POST" {
		return s.direct(r, a)
	}
	if strings.HasPrefix(p, "groups") {
		return s.groups(r, a, p)
	}
	if p == "inbox" && r.Method == "GET" {
		return s.inbox(r, a)
	}
	if p == "inbox/claim" && r.Method == "POST" {
		return s.claim(r, a)
	}
	if p == "inbox/ack" && r.Method == "POST" {
		return s.ack(r, a)
	}
	parts := strings.Split(p, "/")
	if len(parts) == 3 && parts[0] == "channels" && r.Method == "GET" {
		ch := parts[1]
		if !s.authorized(a.ID, ch) {
			return nil, bad("not_group_member")
		}
		if parts[2] != "messages" && parts[2] != "files" {
			return nil, bad("not_found")
		}
		return s.history(r, ch, parts[2] == "files")
	}
	if len(parts) == 3 && parts[0] == "files" && parts[2] == "content" && r.Method == "GET" {
		for _, m := range s.st.Messages {
			if m.Attachment != nil && m.Attachment.ID == parts[1] && s.authorized(a.ID, m.ChannelID) {
				f, e := os.Open(filepath.Join(s.opts.DataDir, "files", m.Attachment.ID))
				if e != nil {
					return nil, bad("not_found")
				}
				return &download{file: f, metadata: m.Attachment, disposition: mime.FormatMediaType("attachment", map[string]string{"filename": m.Attachment.Filename}), created: m.CreatedAt}, nil
			}
		}
		return nil, bad("not_found")
	}
	return nil, bad("not_found")
}

// history reads only retained public message data; it never visits delivery or
// claim state. Callers establish channel access before invoking it.
func (s *Server) history(r *http.Request, ch string, files bool) (any, error) {
	return s.historyMatching(r, ch, files, "")
}

func (s *Server) historyMatching(r *http.Request, ch string, files bool, query string) (any, error) {
	n, c, e := page(r, s.st.Settings.PageSize)
	if e != nil {
		return nil, e
	}
	order := r.URL.Query().Get("order")
	if order == "" {
		order = "oldest"
	}
	if order != "oldest" && order != "newest" {
		return nil, bad("invalid_order")
	}
	seq := int64(0)
	if order == "newest" {
		seq = 1<<63 - 1
	}
	if c != "" {
		seq, e = strconv.ParseInt(c, 10, 64)
		if e != nil {
			return nil, bad("invalid_cursor")
		}
	}
	out := []any{}
	last := int64(0)
	next := ""
	for i := 0; i < len(s.st.Messages); i++ {
		index := i
		if order == "newest" {
			index = len(s.st.Messages) - 1 - i
		}
		m := s.st.Messages[index]
		if m.ChannelID != ch || (order == "oldest" && m.Sequence <= seq || order == "newest" && m.Sequence >= seq) || files && m.Attachment == nil || !messageBodyMatches(m, query) {
			continue
		}
		if len(out) == n {
			next = cursor(strconv.FormatInt(last, 10))
			break
		}
		if files {
			out = append(out, map[string]any{"message_id": m.ID, "file": m.Attachment})
		} else {
			out = append(out, m)
		}
		last = m.Sequence
	}
	field := "messages"
	if files {
		field = "files"
	}
	return map[string]any{field: out, "next_cursor": next}, nil
}

type filter struct {
	Kind      string `json:"kind"`
	ChannelID string `json:"channel_id"`
}

func (s *Server) match(m *Message, f filter) bool {
	ch := s.st.Channels[m.ChannelID]
	return (f.Kind == "" || ch.Kind == f.Kind) && (f.ChannelID == "" || m.ChannelID == f.ChannelID)
}
func validFilter(f filter) bool { return f.Kind == "" || f.Kind == "private" || f.Kind == "group" }
func (s *Server) inbox(r *http.Request, a *storedAgent) (any, error) {
	f := filter{r.URL.Query().Get("kind"), r.URL.Query().Get("channel_id")}
	if !validFilter(f) {
		return nil, bad("invalid_kind")
	}
	unread, claimable, leased := 0, 0, 0
	by := map[string]map[string]any{}
	for _, m := range s.st.Messages {
		d := s.st.Deliveries[a.ID+"/"+m.ID]
		if d == nil || d.Read || !s.authorized(a.ID, m.ChannelID) || !s.match(m, f) {
			continue
		}
		unread++
		active := d.Expires.After(s.now())
		if active {
			leased++
		} else {
			claimable++
		}
		v := by[m.ChannelID]
		if v == nil {
			v = map[string]any{"channel_id": m.ChannelID, "kind": s.st.Channels[m.ChannelID].Kind, "unread": 0, "claimable": 0, "leased": 0}
			by[m.ChannelID] = v
		}
		v["unread"] = v["unread"].(int) + 1
		if active {
			v["leased"] = v["leased"].(int) + 1
		} else {
			v["claimable"] = v["claimable"].(int) + 1
		}
	}
	n, c, e := page(r, s.st.Settings.PageSize)
	if e != nil {
		return nil, e
	}
	channels := []any{}
	for id, v := range by {
		if id > c {
			channels = append(channels, v)
		}
	}
	sort.Slice(channels, func(i, j int) bool {
		return channels[i].(map[string]any)["channel_id"].(string) < channels[j].(map[string]any)["channel_id"].(string)
	})
	next := ""
	if len(channels) > n {
		next = cursor(channels[n-1].(map[string]any)["channel_id"].(string))
		channels = channels[:n]
	}
	return map[string]any{"unread": unread, "claimable": claimable, "leased": leased, "channels": channels, "next_cursor": next}, nil
}
func (s *Server) claim(r *http.Request, a *storedAgent) (any, error) {
	in := struct {
		Limit     int    `json:"limit"`
		Order     string `json:"order"`
		Kind      string `json:"kind"`
		ChannelID string `json:"channel_id"`
	}{Limit: 1, Order: "oldest"}
	if e := decode(r, &in); e != nil {
		return nil, e
	}
	f := filter{in.Kind, in.ChannelID}
	if in.Limit < 1 || in.Limit > s.st.Settings.ClaimBatch || in.Order != "oldest" && in.Order != "newest" || !validFilter(f) {
		return nil, bad("invalid_claim")
	}
	out := []any{}
	for i := 0; i < len(s.st.Messages) && len(out) < in.Limit; i++ {
		index := i
		if in.Order == "newest" {
			index = len(s.st.Messages) - 1 - i
		}
		m := s.st.Messages[index]
		d := s.st.Deliveries[a.ID+"/"+m.ID]
		if d == nil || d.Read || d.Expires.After(s.now()) || !s.authorized(a.ID, m.ChannelID) || !s.match(m, f) {
			continue
		}
		token := secret("claim_")
		d.Token = digest(token)
		d.Expires = s.now().Add(time.Duration(s.st.Settings.ClaimSeconds) * time.Second).UTC()
		v := messageMap(m)
		v["claim_token"] = token
		v["claim_expires_at"] = d.Expires
		out = append(out, v)
	}
	return map[string]any{"messages": out}, nil
}
func (s *Server) ack(r *http.Request, a *storedAgent) (any, error) {
	var in struct {
		Acks []struct {
			MessageID string `json:"message_id"`
			Token     string `json:"claim_token"`
		} `json:"acks"`
	}
	if e := decode(r, &in); e != nil {
		return nil, e
	}
	if len(in.Acks) < 1 || len(in.Acks) > s.st.Settings.ClaimBatch {
		return nil, bad("invalid_ack_batch")
	}
	out := []any{}
	for _, v := range in.Acks {
		d := s.st.Deliveries[a.ID+"/"+v.MessageID]
		code := "read"
		if d == nil {
			code = "message_purged"
		} else if d.Token != digest(v.Token) {
			code = "claim_replaced"
		} else if !d.Expires.After(s.now()) {
			code = "claim_expired"
		} else if d.Read {
			code = "already_read"
		} else {
			d.Read = true
		}
		out = append(out, map[string]any{"message_id": v.MessageID, "status": code})
	}
	return map[string]any{"results": out}, nil
}
