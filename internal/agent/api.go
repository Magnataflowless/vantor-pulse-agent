package agent

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Version é preenchida no build (-ldflags "-X .../agent.Version=0.1.0").
var Version = "0.1.0-dev"

// ErrRevoked: o token do PC foi revogado no painel; o agente para de enviar.
var ErrRevoked = errors.New("token do computador revogado")

type Inventory struct {
	Hostname    string `json:"hostname"`
	OS          string `json:"os,omitempty"`
	OSVersion   string `json:"osVersion,omitempty"`
	CPU         string `json:"cpu,omitempty"`
	RAMMb       int    `json:"ramMb,omitempty"`
	DiskTotalMb int    `json:"diskTotalMb,omitempty"`
	DiskFreeMb  int    `json:"diskFreeMb,omitempty"`
	IP          string `json:"ip,omitempty"`
}

type Live struct {
	WinUser     string `json:"winUser"`
	State       string `json:"state"` // active | idle | paused
	App         string `json:"app,omitempty"`
	Domain      string `json:"domain,omitempty"`
	Title       string `json:"title,omitempty"`
	Since       string `json:"since"`
	PauseReason string `json:"pauseReason,omitempty"`
}

type SessionEvent struct {
	WinUser string `json:"winUser,omitempty"`
	TS      string `json:"ts"`
	Kind    string `json:"kind"`
}

type PauseRec struct {
	WinUser   string  `json:"winUser"`
	Reason    string  `json:"reason"`
	StartedAt string  `json:"startedAt"`
	EndedAt   *string `json:"endedAt"`
}

type Consent struct {
	WinUser     string `json:"winUser"`
	TermVersion int    `json:"termVersion"`
	AcceptedAt  string `json:"acceptedAt"`
}

// Batch é um envio para /agent/sync. Guardado na fila até a API confirmar.
type Batch struct {
	BatchID      string         `json:"batchId"`
	AgentVersion string         `json:"agentVersion"`
	Inventory    *Inventory     `json:"inventory,omitempty"`
	Live         []Live         `json:"live,omitempty"`
	Minutes      []Minute       `json:"minutes,omitempty"`
	Sessions     []SessionEvent `json:"sessions,omitempty"`
	Pauses       []PauseRec     `json:"pauses,omitempty"`
	Consents     []Consent      `json:"consents,omitempty"`
}

type Window struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

type PauseReason struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	DailyLimitMin int    `json:"dailyLimitMin"`
}

// ServerConfig é a resposta do /agent/sync: regras da empresa para este PC.
type ServerConfig struct {
	ServerTime   time.Time     `json:"serverTime"`
	IdleMinutes  int           `json:"idleMinutes"`
	PauseReasons []PauseReason `json:"pauseReasons"`
	Term         *struct {
		Version int
		Body    string
	} `json:"term"`
	DefaultWindows []Window            `json:"defaultWindows"`
	UserWindows    map[string][]Window `json:"userWindows"`
	Update         *struct {
		Version string `json:"version"`
		URL     string `json:"url"`
		SHA256  string `json:"sha256"`
	} `json:"update"`
	PortalURL string `json:"portalUrl"`
}

// InWindow diz se agora é horário de coleta para o usuário (fora disso nada é coletado).
func (c *ServerConfig) InWindow(winUser string, now time.Time) bool {
	ws, ok := c.UserWindows[winUser]
	if !ok {
		ws = c.DefaultWindows
	}
	for _, w := range ws {
		if !now.Before(w.From) && now.Before(w.To) {
			return true
		}
	}
	return false
}

type Client struct {
	Server string
	Token  string
	HTTP   *http.Client
}

func NewClient(server, token string) *Client {
	return &Client{Server: server, Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) post(path string, body any, gz bool, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if gz {
		w := gzip.NewWriter(&buf)
		_, _ = w.Write(raw)
		_ = w.Close()
	} else {
		buf.Write(raw)
	}
	req, err := http.NewRequest(http.MethodPost, c.Server+"/api"+path, &buf)
	if err != nil {
		return err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("user-agent", "vantor-agent/"+Version)
	if gz {
		req.Header.Set("content-encoding", "gzip")
	}
	if c.Token != "" {
		req.Header.Set("authorization", "Bearer "+c.Token)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode == http.StatusUnauthorized && path == "/agent/sync" {
		return ErrRevoked
	}
	if res.StatusCode >= 300 {
		return fmt.Errorf("API respondeu %d: %s", res.StatusCode, bytes.TrimSpace(data))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// Enroll troca o token da empresa pelo token individual deste PC.
func (c *Client) Enroll(enrollToken string, inv Inventory) (deviceID, token string, err error) {
	var r struct{ DeviceID, Token string }
	err = c.post("/agent/enroll", map[string]any{"enrollToken": enrollToken, "inventory": inv}, false, &r)
	return r.DeviceID, r.Token, err
}

func (c *Client) Sync(b *Batch) (*ServerConfig, error) {
	var cfg ServerConfig
	if err := c.post("/agent/sync", b, true, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
