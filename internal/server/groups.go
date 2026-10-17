package server

import (
	"net/http"
	"os"
	"sort"
	"strings"
)

func (s *Server) groups(r *http.Request, a *storedAgent, p string) (any, error) {
	parts := strings.Split(p, "/")
	if len(parts) == 1 && r.Method == "GET" {
		out := []any{}
		for _, g := range s.st.Groups {
			if status := g.Members[a.ID]; status == "active" || status == "pending" {
				out = append(out, map[string]any{"id": g.ID, "name": g.Name, "owner_id": g.OwnerID, "membership": status})
			}
		}
		sort.Slice(out, func(i, j int) bool {
			return out[i].(map[string]any)["id"].(string) < out[j].(map[string]any)["id"].(string)
		})
		return map[string]any{"groups": out}, nil
	}
	if len(parts) == 1 && r.Method == "POST" {
		var in struct {
			Name     string   `json:"name"`
			Invitees []string `json:"invitee_ids"`
		}
		if e := decode(r, &in); e != nil {
			return nil, e
		}
		if len(in.Name) < 1 || len(in.Name) > 128 {
			return nil, bad("invalid_group_name")
		}
		owned := 0
		for _, g := range s.st.Groups {
			if g.OwnerID == a.ID {
				owned++
			}
		}
		if owned >= s.st.Settings.GroupsOwned {
			return nil, bad("group_limit")
		}
		g := &Group{ID: secret("grp_"), Name: in.Name, OwnerID: a.ID, Members: map[string]string{a.ID: "active"}}
		if e := s.invite(g, in.Invitees); e != nil {
			return nil, e
		}
		s.st.Groups[g.ID] = g
		s.st.Channels[g.ID] = &Channel{ID: g.ID, Kind: "group", GroupID: g.ID}
		return map[string]any{"group": g, "channel_id": g.ID}, nil
	}
	if len(parts) < 2 {
		return nil, bad("not_found")
	}
	g := s.st.Groups[parts[1]]
	if g == nil {
		return nil, bad("not_found")
	}
	status := g.Members[a.ID]
	if len(parts) == 2 && r.Method == "GET" {
		if status == "pending" {
			return map[string]any{"id": g.ID, "name": g.Name, "owner_id": g.OwnerID, "membership": status}, nil
		}
		if status != "active" {
			return nil, bad("not_group_member")
		}
		return map[string]any{"group": g, "membership": status, "channel_id": g.ID}, nil
	}
	if len(parts) != 3 || r.Method != "POST" {
		return nil, bad("not_found")
	}
	switch parts[2] {
	case "invitations":
		if g.OwnerID != a.ID {
			return nil, bad("forbidden")
		}
		var in struct {
			Invitees []string `json:"invitee_ids"`
		}
		if e := decode(r, &in); e != nil {
			return nil, e
		}
		if e := s.invite(g, in.Invitees); e != nil {
			return nil, e
		}
		return map[string]any{"group": g}, nil
	case "join":
		if status != "pending" {
			return nil, bad("not_invited")
		}
		g.Members[a.ID] = "active"
		return map[string]any{"membership": "active", "channel_id": g.ID}, nil
	case "leave":
		if g.OwnerID == a.ID {
			return nil, bad("owner_cannot_leave")
		}
		if status != "active" {
			return nil, bad("not_group_member")
		}
		g.Members[a.ID] = "left"
		for k, d := range s.st.Deliveries {
			if d.AgentID == a.ID {
				for _, m := range s.st.Messages {
					if m.ID == d.MessageID && m.ChannelID == g.ID {
						delete(s.st.Deliveries, k)
						break
					}
				}
			}
		}
		return map[string]any{"membership": "left"}, nil
	case "messages":
		if status != "active" {
			return nil, bad("not_group_member")
		}
		in, u, e := s.parseSend(r)
		if u != nil {
			defer os.Remove(u.Path)
		}
		if e != nil {
			return nil, e
		}
		if len(in.To) != 0 {
			return nil, bad("invalid_recipients")
		}
		return s.send(a, s.st.Channels[g.ID], in.Body, u)
	}
	return nil, bad("not_found")
}
func (s *Server) invite(g *Group, ids []string) error {
	if len(ids) > s.st.Settings.GroupMembers {
		return bad("group_member_limit")
	}
	for _, id := range ids {
		a := s.st.Agents[id]
		if a == nil || !a.Enabled || s.st.Hosts[a.HostID] == nil || !s.st.Hosts[a.HostID].Enabled {
			return bad("invalid_target")
		}
		if g.Members[id] != "active" {
			g.Members[id] = "pending"
		}
	}
	n := 0
	for _, status := range g.Members {
		if status != "left" {
			n++
		}
	}
	if n > s.st.Settings.GroupMembers {
		return bad("group_member_limit")
	}
	return nil
}
