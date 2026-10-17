package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"
)

type preparedKey struct{}
type preparedSend struct {
	in sendInput
	u  *upload
}
type sendInput struct {
	To   []string `json:"to_agent_ids"`
	Body string   `json:"body"`
}
type upload struct {
	Prepared    []*File
	AllPrepared []*File
	File
	Path string
}

func (s *Server) parseSend(r *http.Request) (in sendInput, u *upload, err error) {
	if p, ok := r.Context().Value(preparedKey{}).(preparedSend); ok {
		if utf8.RuneCountInString(p.in.Body) > s.st.Settings.BodyCodepoints {
			return p.in, p.u, &apiError{Code: "text_too_long", Limit: s.st.Settings.BodyCodepoints, SettingsRevision: s.st.Settings.Revision}
		}
		if p.u != nil && s.st.Settings.FileCount == 0 {
			return p.in, p.u, &apiError{Code: "files_disabled", Limit: 0, SettingsRevision: s.st.Settings.Revision}
		}
		if p.u != nil && p.u.Size > s.st.Settings.FileBytes {
			return p.in, p.u, &apiError{Code: "file_too_large", Limit: s.st.Settings.FileBytes, SettingsRevision: s.st.Settings.Revision}
		}
		if s.cleanupError != "" {
			return p.in, p.u, bad("cleanup_unavailable")
		}
		return p.in, p.u, nil
	}

	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		mr, e := r.MultipartReader()
		if e != nil {
			return in, nil, bad("invalid_multipart")
		}
		hasMeta := false
		defer func() {
			if err != nil && u != nil {
				os.Remove(u.Path)
			}
		}()
		for {
			p, e := mr.NextPart()
			if e == io.EOF {
				break
			}
			if e != nil {
				return in, u, bad("invalid_multipart")
			}
			switch p.FormName() {
			case "metadata":
				if hasMeta {
					return in, u, bad("invalid_multipart")
				}
				hasMeta = true
				b, e := ioReadBounded(p, maxJSONBytes)
				if e != nil {
					return in, u, e
				}
				if e = strictJSON(b, &in); e != nil {
					return in, u, e
				}
			case "file":
				if u != nil {
					return in, u, bad("multiple_files")
				}
				if s.cleanupError != "" || len(s.st.Deletions) > 0 {
					return in, u, bad("cleanup_unavailable")
				}
				f, e := os.CreateTemp(filepath.Join(s.opts.DataDir, "staging"), "upload-")
				if e != nil {
					return in, u, e
				}
				u = &upload{File: File{Filename: p.FileName(), MediaType: p.Header.Get("Content-Type")}, Path: f.Name()}
				if u.MediaType == "" {
					u.MediaType = "application/octet-stream"
				}
				if len(u.Filename) > 255 || strings.ContainsAny(u.Filename, "\r\n") || len(u.MediaType) > 255 {
					f.Close()
					return in, u, bad("invalid_file_metadata")
				}
				if _, _, e = mime.ParseMediaType(u.MediaType); e != nil {
					f.Close()
					return in, u, bad("invalid_media_type")
				}
				h := sha256.New()
				n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(p, s.st.Settings.FileBytes+1))
				u.Size = n
				u.SHA256 = hex.EncodeToString(h.Sum(nil))
				if e == nil {
					e = f.Sync()
				}
				ce := f.Close()
				if e == nil {
					e = ce
				}
				if e != nil {
					return in, u, e
				}
				if n > s.st.Settings.FileBytes {
					return in, u, &apiError{Code: "file_too_large", Limit: s.st.Settings.FileBytes, SettingsRevision: s.st.Settings.Revision}
				}
			default:
				return in, u, bad("invalid_multipart")
			}
		}
		if !hasMeta {
			return in, u, bad("missing_metadata")
		}
	} else {
		err = decode(r, &in)
		if err != nil {
			return in, nil, err
		}
	}
	if !utf8.ValidString(in.Body) {
		return in, u, bad("invalid_unicode")
	}
	if utf8.RuneCountInString(in.Body) > s.st.Settings.BodyCodepoints {
		return in, u, &apiError{Code: "text_too_long", Limit: s.st.Settings.BodyCodepoints, SettingsRevision: s.st.Settings.Revision}
	}
	if in.Body == "" && u == nil {
		return in, u, bad("empty_message")
	}
	if u != nil && s.st.Settings.FileCount == 0 {
		return in, u, bad("files_disabled")
	}
	return in, u, nil
}
func (s *Server) authorized(a, c string) bool {
	ch := s.st.Channels[c]
	if ch == nil {
		return false
	}
	if ch.Kind == "private" {
		for _, id := range ch.AgentIDs {
			if id == a {
				return true
			}
		}
		return false
	}
	g := s.st.Groups[ch.GroupID]
	return g != nil && g.Members[a] == "active"
}
func (s *Server) send(a *storedAgent, ch *Channel, body string, u *upload) (map[string]any, error) {
	latest := s.st.Latest[a.ID+"/"+ch.ID]
	for _, m := range s.st.Messages {
		if m.ID == latest && m.Body == body {
			same := m.Attachment == nil && u == nil
			if m.Attachment != nil && u != nil {
				x := *m.Attachment
				y := u.File
				x.ID = ""
				y.ID = ""
				same = x == y
			}
			if same {
				return map[string]any{"status": "duplicate", "message_id": m.ID, "channel_id": ch.ID}, nil
			}
		}
	}
	var file *File
	if u != nil {
		if len(u.Prepared) == 0 {
			return nil, bad("storage_unavailable")
		}
		file = u.Prepared[0]
		u.Prepared = u.Prepared[1:]
		if file == nil {
			return nil, bad("storage_unavailable")
		}

	}
	s.st.Sequence++
	m := &Message{ID: secret("msg_"), Sequence: s.st.Sequence, ChannelID: ch.ID, SenderID: a.ID, Body: body, CreatedAt: s.now().UTC(), Attachment: file}
	s.st.Channels[ch.ID] = ch
	s.st.Messages = append(s.st.Messages, m)
	s.st.Latest[a.ID+"/"+ch.ID] = m.ID
	ids := ch.AgentIDs
	if ch.Kind == "group" {
		ids = []string{}
		for id, status := range s.st.Groups[ch.GroupID].Members {
			if status == "active" {
				ids = append(ids, id)
			}
		}
	}
	for _, id := range ids {
		if id != a.ID {
			s.st.Deliveries[id+"/"+m.ID] = &Delivery{MessageID: m.ID, AgentID: id}
		}
	}
	s.retain()
	return map[string]any{"status": "sent", "message_id": m.ID, "channel_id": ch.ID}, nil
}
func (s *Server) retain() {
	remove := map[string]bool{}
	counts, files := map[string]int{}, map[string]int{}
	// The newest-N window is fixed before coupled file/oversize deletion. A later
	// purge never brings an older message back into that retention window.
	for i := len(s.st.Messages) - 1; i >= 0; i-- {
		m := s.st.Messages[i]
		counts[m.ChannelID]++
		if counts[m.ChannelID] > s.st.Settings.MessageCount {
			remove[m.ID] = true
			continue
		}
		if utf8.RuneCountInString(m.Body) > s.st.Settings.BodyCodepoints || m.Attachment != nil && m.Attachment.Size > s.st.Settings.FileBytes {
			remove[m.ID] = true
			continue
		}
		if m.Attachment != nil {
			files[m.ChannelID]++
			if files[m.ChannelID] > s.st.Settings.FileCount {
				remove[m.ID] = true
			}
		}
	}
	kept := []*Message{}
	for _, m := range s.st.Messages {
		if !remove[m.ID] {
			kept = append(kept, m)
			continue
		}
		if m.Attachment != nil {
			s.queueDelete(m.Attachment.ID)
		}
		for k, d := range s.st.Deliveries {
			if d.MessageID == m.ID {
				delete(s.st.Deliveries, k)
			}
		}
		for k, id := range s.st.Latest {
			if id == m.ID {
				delete(s.st.Latest, k)
			}
		}
	}
	s.st.Messages = kept
}
func (s *Server) direct(r *http.Request, a *storedAgent) (any, error) {
	in, u, e := s.parseSend(r)
	if u != nil {
		defer os.Remove(u.Path)
	}
	if e != nil {
		return nil, e
	}
	if len(in.To) == 0 || len(in.To) > s.st.Settings.DirectRecipients {
		return nil, bad("invalid_recipients")
	}
	results := []any{}
	seen := map[string]bool{}
	for _, id := range in.To {
		if seen[id] {
			continue
		}
		seen[id] = true
		var re error
		if id == a.ID {
			re = bad("self_send_forbidden")
		} else if target := s.st.Agents[id]; target == nil || !target.Enabled || s.st.Hosts[target.HostID] == nil || !s.st.Hosts[target.HostID].Enabled {
			re = bad("invalid_target")
		}
		if re != nil {
			results = append(results, map[string]any{"target_agent_id": id, "status": "rejected", "error": re})
			continue
		}
		left, right := a.ID, id
		if left > right {
			left, right = right, left
		}
		ch := &Channel{ID: "dm_" + digest(left+"/"+right), Kind: "private", AgentIDs: []string{left, right}}
		result, err := s.send(a, ch, in.Body, u)
		if err != nil {
			results = append(results, map[string]any{"target_agent_id": id, "status": "rejected", "error": &apiError{Code: "storage_unavailable"}})
			continue
		}
		result["target_agent_id"] = id
		results = append(results, result)
	}
	return map[string]any{"request_id": secret("req_"), "results": results}, nil
}
func messageMap(m *Message) map[string]any {
	b, _ := json.Marshal(m)
	v := map[string]any{}
	json.Unmarshal(b, &v)
	return v
}

// Materialization precedes the state critical section. Final authorization and
// deduplication are checked again before committing any prepared copy.
func (s *Server) prepareFiles(u *upload, n int) {
	for i := 0; i < n; i++ {
		var result *File
		id := secret("file_")
		func() {
			in, e := os.Open(u.Path)
			if e != nil {
				return
			}
			defer in.Close()
			out, e := os.OpenFile(filepath.Join(s.opts.DataDir, "files", id), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
			if e != nil {
				return
			}
			v := u.File
			v.ID = id
			u.AllPrepared = append(u.AllPrepared, &v)
			_, e = io.Copy(out, in)
			if e == nil {
				e = out.Sync()
			}
			ce := out.Close()
			if e == nil {
				e = ce
			}
			if e != nil {
				os.Remove(filepath.Join(s.opts.DataDir, "files", id))
				return
			}
			result = &v
		}()
		u.Prepared = append(u.Prepared, result)
	}
	if runtime.GOOS != "windows" {
		dir, e := os.Open(filepath.Join(s.opts.DataDir, "files"))
		if e == nil {
			e = dir.Sync()
			dir.Close()
		}
		if e != nil {
			for _, f := range u.AllPrepared {
				os.Remove(filepath.Join(s.opts.DataDir, "files", f.ID))
			}
			for i := range u.Prepared {
				u.Prepared[i] = nil
			}
		}
	}
}
func (s *Server) releasePrepared(u *upload) {
	s.mu.Lock()
	defer s.mu.Unlock()
	owned := map[string]bool{}
	for _, m := range s.st.Messages {
		if m.Attachment != nil {
			owned[m.Attachment.ID] = true
		}
	}
	for _, f := range u.AllPrepared {
		if !owned[f.ID] {
			if e := os.Remove(filepath.Join(s.opts.DataDir, "files", f.ID)); e != nil && !os.IsNotExist(e) {
				s.queueDelete(f.ID)
				s.cleanupError = "physical file deletion failed"
			}
		}
	}
	s.save()
}

func (s *Server) releaseStaging(path string) {
	if e := os.Remove(path); e != nil && !os.IsNotExist(e) {
		s.mu.Lock()
		s.cleanupError = "staging cleanup failed"
		s.mu.Unlock()
	}
}
