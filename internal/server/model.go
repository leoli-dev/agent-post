package server

import "time"

type Settings struct {
	Revision             int   `json:"revision"`
	MessageCount         int   `json:"message_count"`
	BodyCodepoints       int   `json:"body_codepoints"`
	FileBytes            int64 `json:"file_bytes"`
	FileCount            int   `json:"file_count"`
	ClaimSeconds         int   `json:"claim_seconds"`
	ClaimBatch           int   `json:"claim_batch"`
	PageSize             int   `json:"page_size"`
	DirectRecipients     int   `json:"direct_recipients"`
	GroupMembers         int   `json:"group_members"`
	GroupsOwned          int   `json:"groups_owned"`
	ConcurrentUploads    int   `json:"concurrent_uploads"`
	TemporaryUploadBytes int64 `json:"temporary_upload_bytes"`
}

func defaults() Settings {
	return Settings{1, 100, 8192, 50_000_000, 10, 60, 50, 100, 16, 64, 32, 4, 100 << 20}
}

type Host struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	OS       string            `json:"os"`
	IPs      []string          `json:"ips"`
	Metadata map[string]string `json:"metadata"`
	Enabled  bool              `json:"enabled"`
}
type Agent struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	HostID     string     `json:"host_id"`
	Enabled    bool       `json:"enabled"`
	LastAccess *time.Time `json:"last_access"`
	KeyHash    string     `json:"-"`
}
type storedAgent struct {
	Agent
	Digest string `json:"digest"`
}
type Group struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	OwnerID string            `json:"owner_id"`
	Members map[string]string `json:"members"`
}
type Channel struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"`
	AgentIDs []string `json:"agent_ids,omitempty"`
	GroupID  string   `json:"group_id,omitempty"`
}
type File struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	MediaType string `json:"media_type"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
}
type Message struct {
	ID         string    `json:"id"`
	Sequence   int64     `json:"sequence"`
	ChannelID  string    `json:"channel_id"`
	SenderID   string    `json:"sender_id"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
	Attachment *File     `json:"attachment,omitempty"`
}
type Delivery struct {
	MessageID string    `json:"message_id"`
	AgentID   string    `json:"agent_id"`
	Read      bool      `json:"read"`
	Token     string    `json:"token"`
	Expires   time.Time `json:"expires"`
}
type state struct {
	Settings   Settings
	Hosts      map[string]*Host
	Agents     map[string]*storedAgent
	Groups     map[string]*Group
	Channels   map[string]*Channel
	Messages   []*Message
	Deliveries map[string]*Delivery
	Latest     map[string]string
	Deletions  []string
	Sequence   int64
}

func initial() state {
	return state{Settings: defaults(), Hosts: map[string]*Host{}, Agents: map[string]*storedAgent{}, Groups: map[string]*Group{}, Channels: map[string]*Channel{}, Messages: []*Message{}, Deliveries: map[string]*Delivery{}, Latest: map[string]string{}, Deletions: []string{}}
}

type apiError struct {
	Code             string `json:"code"`
	Message          string `json:"message,omitempty"`
	Field            string `json:"field,omitempty"`
	Limit            any    `json:"limit,omitempty"`
	SettingsRevision int    `json:"settings_revision,omitempty"`
}

func (e *apiError) Error() string { return e.Code }
func bad(code string) error       { return &apiError{Code: code} }
