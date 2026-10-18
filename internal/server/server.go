package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	_ "modernc.org/sqlite"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Options struct {
	DataDir       string
	AdminPassword string
	SecureCookies bool
	AdminHandler  http.Handler
}
type adminVerifiedKey struct{}
type session struct {
	CSRF    string
	Expires time.Time
}
type Server struct {
	authSlots         chan struct{}
	activeUploads     int
	temporaryReserved int64
	persisted         map[string]string
	adminDigest       string
	adminSalt         string
	mu                sync.Mutex
	db, lock          *sql.DB
	lockConn          *sql.Conn
	opts              Options
	st                state
	sessions          map[string]session
	cleanupError      string
	now               func() time.Time
	system            *systemMonitor
}

func secret(prefix string) string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return prefix + hex.EncodeToString(b)
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func Open(o Options) (*Server, error) {
	if o.DataDir == "" || len(o.AdminPassword) < 12 {
		return nil, fmt.Errorf("data directory and admin password of at least 12 characters required")
	}
	if e := os.MkdirAll(o.DataDir, 0700); e != nil {
		return nil, e
	}
	if e := os.Chmod(o.DataDir, 0700); e != nil {
		return nil, e
	}
	s := &Server{opts: o, sessions: map[string]session{}, now: time.Now, authSlots: make(chan struct{}, 4), system: newSystemMonitor(o.DataDir)}
	var e error
	s.lock, e = sql.Open("sqlite", filepath.Join(o.DataDir, "process-lock.sqlite"))
	if e != nil {
		return nil, e
	}
	s.lock.SetMaxOpenConns(1)
	s.lockConn, e = s.lock.Conn(contextBackground())
	if e != nil {
		s.Close()
		return nil, e
	}
	if _, e = s.lockConn.ExecContext(contextBackground(), "BEGIN EXCLUSIVE"); e != nil {
		s.Close()
		return nil, fmt.Errorf("data directory is already in use: %w", e)
	}
	s.db, e = sql.Open("sqlite", filepath.Join(o.DataDir, "agent-post.sqlite"))
	if e != nil {
		s.Close()
		return nil, e
	}
	s.db.SetMaxOpenConns(1)
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL", "PRAGMA secure_delete=ON", "PRAGMA busy_timeout=5000", "CREATE TABLE IF NOT EXISTS schema_version(version INTEGER NOT NULL)", "INSERT INTO schema_version SELECT 1 WHERE NOT EXISTS(SELECT 1 FROM schema_version)", "CREATE TABLE IF NOT EXISTS records(kind TEXT NOT NULL,id TEXT NOT NULL,payload BLOB NOT NULL,PRIMARY KEY(kind,id))", "CREATE TABLE IF NOT EXISTS administrator(salt TEXT NOT NULL, verifier TEXT NOT NULL)"} {
		if _, e = s.db.Exec(q); e != nil {
			s.Close()
			return nil, e
		}
	}
	var version int
	if e = s.db.QueryRow("SELECT version FROM schema_version").Scan(&version); e != nil || version != 1 {
		s.Close()
		return nil, fmt.Errorf("unsupported database schema")
	}
	s.st = initial()
	if e = s.load(); e != nil {
		s.Close()
		return nil, e
	}
	if e = s.bootstrap(); e != nil {
		s.Close()
		return nil, e
	}
	for _, dir := range []string{"files", "staging"} {
		if e = os.MkdirAll(filepath.Join(o.DataDir, dir), 0700); e != nil {
			s.Close()
			return nil, e
		}
	}
	for _, m := range s.st.Messages {
		if m.Attachment != nil {
			info, err := os.Lstat(filepath.Join(o.DataDir, "files", m.Attachment.ID))
			if err != nil || !info.Mode().IsRegular() || info.Size() != m.Attachment.Size {
				s.Close()
				return nil, fmt.Errorf("retained attachment is missing or incomplete; repair storage before reopening")
			}
		}
	}
	// Nothing in staging is committed; unowned files are interrupted materializations.
	for _, dir := range []string{"staging", "files"} {
		entries, err := os.ReadDir(filepath.Join(o.DataDir, dir))
		if err != nil {
			s.Close()
			return nil, err
		}
		owned := map[string]bool{}
		for _, m := range s.st.Messages {
			if m.Attachment != nil {
				owned[m.Attachment.ID] = true
			}
		}
		for _, f := range entries {
			if dir == "staging" || !owned[f.Name()] {
				if err = os.Remove(filepath.Join(o.DataDir, dir, f.Name())); err != nil {
					if dir == "files" {
						s.queueDelete(f.Name())
					} else {
						s.cleanupError = "staging cleanup failed"
					}
				}
			}
		}
	}
	if e = s.save(); e != nil {
		s.Close()
		return nil, e
	}
	s.cleanup()
	return s, nil
}
func (s *Server) Close() error {
	var e error
	if s.db != nil {
		e = s.db.Close()
	}
	if s.lockConn != nil {
		s.lockConn.ExecContext(contextBackground(), "ROLLBACK")
		s.lockConn.Close()
	}
	if s.lock != nil {
		s.lock.Close()
	}
	return e
}
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serve) }
func output(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, e error) {
	a, ok := e.(*apiError)
	if !ok {
		a = &apiError{Code: "internal_error", Message: "operation could not be persisted"}
	}
	status := 400
	switch a.Code {
	case "internal_error":
		status = 500
	case "authentication_capacity", "storage_unavailable":
		status = 503
	case "unauthorized":
		status = 401
	case "source_ip_denied", "forbidden", "not_group_member", "not_invited", "csrf_required":
		status = 403
	case "not_found", "message_purged":
		status = 404
	case "cleanup_unavailable", "upload_capacity":
		status = 503
	case "claim_expired", "claim_replaced", "owner_cannot_leave":
		status = 409
	}
	output(w, status, map[string]any{"error": a})
}
func decode(r *http.Request, v any) error {
	b, e := ioReadBounded(r.Body, maxJSONBytes)
	if e != nil {
		return bad("invalid_json")
	}
	return strictJSON(b, v)
}
func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; frame-ancestors 'none'; base-uri 'none'")
	if (r.URL.Path == "/admin" || strings.HasPrefix(r.URL.Path, "/admin/")) && !strings.HasPrefix(r.URL.Path, "/admin/api/") {
		if s.opts.AdminHandler != nil {
			s.opts.AdminHandler.ServeHTTP(w, r)
		} else {
			http.NotFound(w, r)
		}
		return
	}
	// Read request bodies before entering the state critical section. Uploads first
	// authenticate and reserve a bounded amount of staging capacity.
	if r.Body != nil && r.Method != "GET" {
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			s.mu.Lock()
			_, authErr := s.authenticate(r)
			settings := s.st.Settings
			cleanupErr := s.cleanupError
			if authErr == nil && (s.activeUploads >= settings.ConcurrentUploads || s.temporaryReserved+settings.FileBytes > settings.TemporaryUploadBytes) {
				authErr = bad("upload_capacity")
			}
			if authErr == nil {
				s.activeUploads++
				s.temporaryReserved += settings.FileBytes
			}
			s.mu.Unlock()
			if authErr != nil {
				fail(w, authErr)
				return
			}
			defer func() { s.mu.Lock(); s.activeUploads--; s.temporaryReserved -= settings.FileBytes; s.mu.Unlock() }()
			staging := &Server{opts: s.opts, st: state{Settings: settings}, cleanupError: cleanupErr}
			in, u, e := staging.parseSend(r)
			if u != nil {
				defer s.releaseStaging(u.Path)
			}
			if e != nil {
				fail(w, e)
				return
			}
			if u != nil {
				n := 1
				if r.URL.Path == "/api/v1/messages/direct" {
					if len(in.To) == 0 || len(in.To) > settings.DirectRecipients {
						fail(w, bad("invalid_recipients"))
						return
					}
					n = len(in.To)
				}
				staging.prepareFiles(u, n)
				defer s.releasePrepared(u)
			}
			r = r.WithContext(context.WithValue(r.Context(), preparedKey{}, preparedSend{in, u}))
		} else {
			b, e := ioReadBounded(r.Body, maxJSONBytes)
			if e != nil {
				fail(w, e)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(b))
		}
	}
	if strings.HasPrefix(r.URL.Path, "/admin/api/") {
		password := ""
		verify := false
		if r.URL.Path == "/admin/api/login" && r.Method == "POST" {
			var in struct {
				Password string `json:"password"`
			}
			b, e := ioReadBounded(r.Body, maxJSONBytes)
			if e != nil {
				fail(w, e)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(b))
			if e = strictJSON(b, &in); e != nil {
				fail(w, e)
				return
			}
			password = in.Password
			verify = true
		} else if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
			password = strings.TrimPrefix(h, "Bearer ")
			verify = true
		}
		if verify {
			select {
			case s.authSlots <- struct{}{}:
			default:
				fail(w, bad("authentication_capacity"))
				return
			}
			valid := s.validAdmin(password)
			<-s.authSlots
			r = r.WithContext(context.WithValue(r.Context(), adminVerifiedKey{}, valid))
		}
	}
	s.mu.Lock()
	held := true
	defer func() {
		if held {
			s.mu.Unlock()
		}
	}()
	if r.URL.Path == "/healthz" && r.Method == "GET" {
		v := map[string]any{"status": "ok", "cleanup": s.cleanupState()}
		held = false
		s.mu.Unlock()
		output(w, 200, v)
		return
	}
	// Roll back in-memory mutations if persistence fails. No successful response precedes save.
	before, _ := json.Marshal(&s.st)
	var result any
	var err error
	if strings.HasPrefix(r.URL.Path, "/admin/api/") {
		result, err = s.admin(w, r)
	} else if strings.HasPrefix(r.URL.Path, "/api/v1/") {
		var a *storedAgent
		a, err = s.authenticate(r)
		if err == nil {
			result, err = s.agent(w, r, a)
		}
	} else {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		var restored state
		json.Unmarshal(before, &restored)
		s.st = restored
		held = false
		s.mu.Unlock()
		fail(w, err)
		return
	}
	if _, ok := result.(systemMetricsRequest); ok {
		// Host probes never hold the message-state lock or write runtime state.
		held = false
		s.mu.Unlock()
		output(w, 200, s.system.snapshot(r.Context()))
		return
	}
	if err = s.save(); err != nil {
		if d, ok := result.(*download); ok {
			d.file.Close()
		}
		var restored state
		json.Unmarshal(before, &restored)
		s.st = restored
		held = false
		s.mu.Unlock()
		fail(w, err)
		return
	}
	s.cleanup()
	if v, ok := result.(map[string]any); ok {
		if _, has := v["cleanup"]; has {
			v["cleanup"] = s.cleanupState()
		}
	}
	if d, ok := result.(*download); ok {
		held = false
		s.mu.Unlock()
		defer d.file.Close()
		w.Header().Set("Content-Type", d.metadata.MediaType)
		w.Header().Set("Content-Disposition", d.disposition)
		http.ServeContent(w, r, d.metadata.Filename, d.created, d.file)
		return
	}
	if result != nil {
		b, e := json.Marshal(result)
		held = false
		s.mu.Unlock()
		if e != nil {
			fail(w, e)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write(append(b, '\n'))
	}
}
func (s *Server) authenticate(r *http.Request) (*storedAgent, error) {
	if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		return nil, bad("unauthorized")
	}
	h := digest(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	var a *storedAgent
	for _, v := range s.st.Agents {
		if v.Digest != "" && subtle.ConstantTimeCompare([]byte(h), []byte(v.Digest)) == 1 {
			a = v
			break
		}
	}
	if a == nil || !a.Enabled {
		return nil, bad("unauthorized")
	}
	host := s.st.Hosts[a.HostID]
	if host == nil || !host.Enabled {
		return nil, bad("unauthorized")
	}
	ipstr, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		return nil, bad("source_ip_denied")
	}
	ip, e := netip.ParseAddr(ipstr)
	if e != nil {
		return nil, bad("source_ip_denied")
	}
	allowed := false
	for _, v := range host.IPs {
		p, _ := netip.ParseAddr(v)
		if p.Unmap() == ip.Unmap() {
			allowed = true
		}
	}
	if !allowed {
		return nil, bad("source_ip_denied")
	}
	t := s.now().UTC()
	a.LastAccess = &t
	return a, nil
}
func (s *Server) cleanupState() any {
	return map[string]any{"pending_files": len(s.st.Deletions), "last_error": s.cleanupError}
}
func (s *Server) cleanup() {
	if s.activeUploads == 0 {
		entries, e := os.ReadDir(filepath.Join(s.opts.DataDir, "staging"))
		failed := e != nil
		for _, f := range entries {
			if e = os.Remove(filepath.Join(s.opts.DataDir, "staging", f.Name())); e != nil {
				failed = true
			}
		}
		if failed {
			s.cleanupError = "staging cleanup failed"
		} else if s.cleanupError == "staging cleanup failed" {
			s.cleanupError = ""
		}
	}

	pending := []string{}
	previous := append([]string{}, s.st.Deletions...)
	for _, id := range s.st.Deletions {
		e := os.Remove(filepath.Join(s.opts.DataDir, "files", id))
		if e != nil && !os.IsNotExist(e) {
			pending = append(pending, id)
		}
	}
	if len(previous) > 0 && runtime.GOOS != "windows" {
		dir, e := os.Open(filepath.Join(s.opts.DataDir, "files"))
		if e == nil {
			e = dir.Sync()
			dir.Close()
		}
		if e != nil {
			pending = previous
		}
	}
	s.st.Deletions = pending
	if len(pending) > 0 {
		s.cleanupError = "physical file deletion failed"
	} else if s.cleanupError == "physical file deletion failed" {
		s.cleanupError = ""
	}
	s.save()
}

func (s *Server) queueDelete(id string) {
	for _, v := range s.st.Deletions {
		if v == id {
			return
		}
	}
	s.st.Deletions = append(s.st.Deletions, id)
}
