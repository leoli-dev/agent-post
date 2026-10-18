package server

import (
	"crypto/subtle"
	"net/http"
	"net/netip"
	"sort"
	"strings"
	"time"
)

func (s *Server) admin(w http.ResponseWriter, r *http.Request) (any, error) {
	p := strings.TrimPrefix(r.URL.Path, "/admin/api/")
	if p == "login" && r.Method == "POST" {
		var in struct {
			Password string `json:"password"`
		}
		if e := decode(r, &in); e != nil {
			return nil, e
		}
		if valid, _ := r.Context().Value(adminVerifiedKey{}).(bool); !valid {
			return nil, bad("unauthorized")
		}
		for id, sess := range s.sessions {
			if !sess.Expires.After(s.now()) {
				delete(s.sessions, id)
			}
		}
		if len(s.sessions) >= 1024 {
			return nil, bad("authentication_capacity")
		}
		id, csrf := secret("ses_"), secret("csrf_")
		s.sessions[id] = session{csrf, s.now().Add(8 * time.Hour)}
		http.SetCookie(w, &http.Cookie{Name: "ap_admin", Value: id, Path: "/admin", HttpOnly: true, Secure: s.opts.SecureCookies, SameSite: http.SameSiteStrictMode, MaxAge: 28800})
		return map[string]any{"csrf_token": csrf}, nil
	}
	bearer := r.Header.Get("Authorization")
	admin := false
	if strings.HasPrefix(bearer, "Bearer ") {
		admin, _ = r.Context().Value(adminVerifiedKey{}).(bool)
	}
	var sess session
	if !admin {
		cookie, e := r.Cookie("ap_admin")
		if e != nil {
			return nil, bad("unauthorized")
		}
		sess = s.sessions[cookie.Value]
		if !sess.Expires.After(s.now()) {
			return nil, bad("unauthorized")
		}
		if r.Method != "GET" && subtle.ConstantTimeCompare([]byte(sess.CSRF), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
			return nil, bad("csrf_required")
		}
	}
	if p == "session" && r.Method == "GET" {
		return map[string]any{"csrf_token": sess.CSRF}, nil
	}
	if p == "system" && r.Method == "GET" {
		return systemMetricsRequest{}, nil
	}
	if p == "logout" && r.Method == "POST" {
		if c, e := r.Cookie("ap_admin"); e == nil {
			delete(s.sessions, c.Value)
		}
		http.SetCookie(w, &http.Cookie{Name: "ap_admin", Path: "/admin", MaxAge: -1, HttpOnly: true, Secure: s.opts.SecureCookies, SameSite: http.SameSiteStrictMode})
		return map[string]any{}, nil
	}
	if p == "state" && r.Method == "GET" {
		hosts := []*Host{}
		agents := []Agent{}
		groups := []*Group{}
		for _, h := range s.st.Hosts {
			hosts = append(hosts, h)
		}
		for _, a := range s.st.Agents {
			agents = append(agents, a.Agent)
		}
		for _, g := range s.st.Groups {
			groups = append(groups, g)
		}
		return map[string]any{"hosts": hosts, "agents": agents, "groups": groups, "settings": s.st.Settings, "cleanup": s.cleanupState()}, nil
	}
	if p == "cleanup" && r.Method == "POST" {
		s.cleanup()
		return s.cleanupState(), nil
	}
	if p == "settings" && r.Method == "PUT" {
		in := s.st.Settings
		if e := decode(r, &in); e != nil {
			return nil, e
		}
		if in.MessageCount < 1 || in.MessageCount > 100000 || in.BodyCodepoints < 1 || in.BodyCodepoints > 1000000 || in.FileBytes < 1 || in.FileBytes > 1<<30 || in.FileCount < 0 || in.FileCount > 100000 || in.ClaimSeconds < 1 || in.ClaimSeconds > 86400 || in.ClaimBatch < 1 || in.ClaimBatch > 1000 || in.PageSize < 1 || in.PageSize > 1000 || in.DirectRecipients < 1 || in.DirectRecipients > 100 || in.GroupMembers < 1 || in.GroupMembers > 1000 || in.GroupsOwned < 1 || in.GroupsOwned > 1000 || in.ConcurrentUploads < 1 || in.ConcurrentUploads > 32 || in.TemporaryUploadBytes < in.FileBytes || in.TemporaryUploadBytes > 4<<30 {
			return nil, bad("invalid_settings")
		}
		in.Revision = s.st.Settings.Revision + 1
		s.st.Settings = in
		s.retain()
		return map[string]any{"settings": in, "cleanup": s.cleanupState()}, nil
	}
	parts := strings.Split(p, "/")
	if parts[0] == "channels" && r.Method == "GET" {
		if len(parts) == 1 {
			return s.adminChannels(r)
		}
		if len(parts) == 3 && parts[2] == "messages" {
			if s.st.Channels[parts[1]] == nil {
				return nil, bad("not_found")
			}
			return s.historyMatching(r, parts[1], false, adminMessageQuery(r))
		}
	}
	if parts[0] == "hosts" && ((len(parts) == 1 && r.Method == "POST") || (len(parts) == 2 && r.Method == "PATCH")) {
		h := Host{Enabled: true, Metadata: map[string]string{}}
		if len(parts) == 2 {
			old := s.st.Hosts[parts[1]]
			if old == nil {
				return nil, bad("not_found")
			}
			h = *old
		}
		if e := decode(r, &h); e != nil {
			return nil, e
		}
		if len(h.Name) < 1 || len(h.Name) > 128 || len(h.OS) > 128 || len(h.IPs) == 0 || len(h.IPs) > 64 {
			return nil, bad("invalid_host")
		}
		if len(h.Metadata) > 64 {
			return nil, bad("invalid_host_metadata")
		}
		for k, v := range h.Metadata {
			if len(k) > 128 || len(v) > 4096 {
				return nil, bad("invalid_host_metadata")
			}
		}
		for i, v := range h.IPs {
			ip, e := netip.ParseAddr(v)
			if e != nil || ip.Zone() != "" {
				return nil, bad("invalid_ip")
			}
			h.IPs[i] = ip.Unmap().String()
		}
		if len(parts) == 1 {
			h.ID = secret("host_")
		} else {
			h.ID = parts[1]
		}
		s.st.Hosts[h.ID] = &h
		return map[string]any{"host": h}, nil
	}
	if parts[0] == "agents" && ((len(parts) == 1 && r.Method == "POST") || (len(parts) == 2 && r.Method == "PATCH")) {
		a := storedAgent{Agent: Agent{Enabled: true}}
		if len(parts) == 2 {
			old := s.st.Agents[parts[1]]
			if old == nil {
				return nil, bad("not_found")
			}
			a = *old
		}
		var in struct {
			Name    string `json:"name"`
			HostID  string `json:"host_id"`
			Enabled *bool  `json:"enabled"`
		}
		in.Name = a.Name
		in.HostID = a.HostID
		if e := decode(r, &in); e != nil {
			return nil, e
		}
		a.Name = in.Name
		a.HostID = in.HostID
		if in.Enabled != nil {
			a.Enabled = *in.Enabled
		}
		if len(a.Name) < 1 || len(a.Name) > 128 || s.st.Hosts[a.HostID] == nil {
			return nil, bad("invalid_agent")
		}
		for _, other := range s.st.Agents {
			if other.ID != a.ID && other.Name == a.Name && other.HostID == a.HostID {
				return nil, bad("agent_name_exists")
			}
		}
		key := ""
		if len(parts) == 1 {
			a.ID = secret("agt_")
			key = secret("apk_")
			a.Digest = digest(key)
		} else {
			a.ID = parts[1]
		}
		s.st.Agents[a.ID] = &a
		out := map[string]any{"agent": a.Agent}
		if key != "" {
			out["api_key"] = key
		}
		return out, nil
	}
	if len(parts) == 3 && parts[0] == "agents" && r.Method == "POST" {
		a := s.st.Agents[parts[1]]
		if a == nil {
			return nil, bad("not_found")
		}
		switch parts[2] {
		case "rotate-key":
			key := secret("apk_")
			a.Digest = digest(key)
			return map[string]any{"api_key": key}, nil
		case "revoke-key":
			a.Digest = ""
			return map[string]any{}, nil
		}
	}
	return nil, bad("not_found")
}

type adminChannel struct {
	*Channel
	MessageCount  int        `json:"message_count"`
	FileCount     int        `json:"file_count"`
	LastMessageAt *time.Time `json:"last_message_at"`
}

func (s *Server) adminChannels(r *http.Request) (any, error) {
	n, c, e := page(r, s.st.Settings.PageSize)
	if e != nil {
		return nil, e
	}
	kind := r.URL.Query().Get("kind")
	if !validFilter(filter{Kind: kind}) {
		return nil, bad("invalid_kind")
	}
	query := adminMessageQuery(r)
	participant := r.URL.Query().Get("participant_id")
	owner := r.URL.Query().Get("owner_id")
	matchingContent := map[string]bool{}
	if query != "" {
		for _, message := range s.st.Messages {
			if messageBodyMatches(message, query) {
				matchingContent[message.ChannelID] = true
			}
		}
	}
	out := []*adminChannel{}
	for id, ch := range s.st.Channels {
		if id > c && (kind == "" || ch.Kind == kind) && s.adminChannelMatches(ch, participant, owner) && (query == "" || matchingContent[id]) {
			out = append(out, &adminChannel{Channel: ch})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	next := ""
	if len(out) > n {
		next = cursor(out[n-1].ID)
		out = out[:n]
	}
	byID := map[string]*adminChannel{}
	for _, ch := range out {
		byID[ch.ID] = ch
	}
	for _, m := range s.st.Messages {
		if ch := byID[m.ChannelID]; ch != nil {
			ch.MessageCount++
			if m.Attachment != nil {
				ch.FileCount++
			}
			ch.LastMessageAt = &m.CreatedAt
		}
	}
	return map[string]any{"channels": out, "next_cursor": next}, nil
}

func adminMessageQuery(r *http.Request) string {
	return strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
}

func messageBodyMatches(message *Message, query string) bool {
	return query == "" || strings.Contains(strings.ToLower(message.Body), query)
}

func (s *Server) adminChannelMatches(ch *Channel, participant, owner string) bool {
	group := s.st.Groups[ch.GroupID]
	if owner != "" && (group == nil || group.OwnerID != owner) {
		return false
	}
	if participant == "" {
		return true
	}
	if ch.Kind == "group" {
		return group != nil && group.Members[participant] == "active"
	}
	for _, id := range ch.AgentIDs {
		if id == participant {
			return true
		}
	}
	return false
}
