package server

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Each identity, message and delivery is an independent record. A transaction writes
// only changed records; reading a directory never rewrites retained payloads.
func (s *Server) records() map[string]string {
	out := map[string]string{}
	put := func(kind, id string, v any) { b, _ := json.Marshal(v); out[kind+"/"+id] = string(b) }
	put("meta", "settings", s.st.Settings)
	put("meta", "sequence", s.st.Sequence)
	put("meta", "deletions", s.st.Deletions)
	for id, v := range s.st.Hosts {
		put("host", id, v)
	}
	for id, v := range s.st.Agents {
		put("agent", id, v)
	}
	for id, v := range s.st.Groups {
		put("group", id, v)
	}
	for id, v := range s.st.Channels {
		put("channel", id, v)
	}
	for _, v := range s.st.Messages {
		put("message", v.ID, v)
	}
	for id, v := range s.st.Deliveries {
		put("delivery", id, v)
	}
	for id, v := range s.st.Latest {
		put("latest", id, v)
	}
	return out
}
func (s *Server) save() error {
	next := s.records()
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for k, v := range next {
		if s.persisted[k] == v {
			continue
		}
		parts := strings.SplitN(k, "/", 2)
		if _, e = tx.Exec("INSERT INTO records(kind,id,payload) VALUES(?,?,?) ON CONFLICT(kind,id) DO UPDATE SET payload=excluded.payload", parts[0], parts[1], v); e != nil {
			return e
		}
	}
	for k := range s.persisted {
		if _, ok := next[k]; !ok {
			parts := strings.SplitN(k, "/", 2)
			if _, e = tx.Exec("DELETE FROM records WHERE kind=? AND id=?", parts[0], parts[1]); e != nil {
				return e
			}
		}
	}
	if e = tx.Commit(); e == nil {
		s.persisted = next
	}
	return e
}
func (s *Server) load() error {
	s.persisted = map[string]string{}
	rows, e := s.db.Query("SELECT kind,id,payload FROM records")
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var kind, id, p string
		if e = rows.Scan(&kind, &id, &p); e != nil {
			return e
		}
		s.persisted[kind+"/"+id] = p
		var target any
		switch kind {
		case "meta":
			switch id {
			case "settings":
				target = &s.st.Settings
			case "sequence":
				target = &s.st.Sequence
			case "deletions":
				target = &s.st.Deletions
			}
		case "host":
			v := new(Host)
			s.st.Hosts[id] = v
			target = v
		case "agent":
			v := new(storedAgent)
			s.st.Agents[id] = v
			target = v
		case "group":
			v := new(Group)
			s.st.Groups[id] = v
			target = v
		case "channel":
			v := new(Channel)
			s.st.Channels[id] = v
			target = v
		case "message":
			v := new(Message)
			s.st.Messages = append(s.st.Messages, v)
			target = v
		case "delivery":
			v := new(Delivery)
			s.st.Deliveries[id] = v
			target = v
		case "latest":
			var v string
			if e = json.Unmarshal([]byte(p), &v); e != nil {
				return e
			}
			s.st.Latest[id] = v
		}
		if target != nil {
			if e = json.Unmarshal([]byte(p), target); e != nil {
				return e
			}
		}
	}
	sort.Slice(s.st.Messages, func(i, j int) bool { return s.st.Messages[i].Sequence < s.st.Messages[j].Sequence })
	return rows.Err()
}
func passwordDigest(password, salt string) string {
	b, e := pbkdf2.Key(sha256.New, password, []byte(salt), 600000, 32)
	if e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}
func (s *Server) bootstrap() error {
	e := s.db.QueryRow("SELECT salt,verifier FROM administrator").Scan(&s.adminSalt, &s.adminDigest)
	if errors.Is(e, sql.ErrNoRows) {
		s.adminSalt = secret("salt_")
		s.adminDigest = passwordDigest(s.opts.AdminPassword, s.adminSalt)
		_, e = s.db.Exec("INSERT INTO administrator(salt,verifier) VALUES(?,?)", s.adminSalt, s.adminDigest)
	} else if e == nil {
		if !s.validAdmin(s.opts.AdminPassword) {
			return fmt.Errorf("administrator password does not match existing data directory")
		}
	}
	return e
}
func (s *Server) validAdmin(password string) bool {
	value := passwordDigest(password, s.adminSalt)
	return subtle.ConstantTimeCompare([]byte(value), []byte(s.adminDigest)) == 1
}
