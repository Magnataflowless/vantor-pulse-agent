//go:build windows

package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"fyne.io/systray"
)

// Collector roda na sessão de cada usuário do Windows: lê a janela em foco e a atividade,
// mostra o ícone na bandeja e envia os minutos para a API.
type Collector struct {
	Dry bool // modo de teste: imprime os lotes em vez de enviar

	user   string
	dir    string
	client *Client
	queue  *Queue
	agg    *Aggregator
	uia    *UIA

	mu       sync.Mutex
	cfg      *ServerConfig
	state    UserState
	paused   *PauseRec
	pending  Batch // sessões, pausas e aceites ainda não enfileirados
	asking   bool
	lastAsk  time.Time
	lastTick uint32
	locked   bool
	live     Live
	status   string

	tray struct {
		status, resume, term, portal, about *systray.MenuItem
		reasons                             map[string]*systray.MenuItem
	}
}

func (c *Collector) Run() error {
	c.user = WinUser()
	c.dir = UserDir()
	if err := os.MkdirAll(c.dir, 0o700); err != nil {
		return err
	}
	_ = readJSON(filepath.Join(c.dir, "estado.json"), &c.state)
	var cached ServerConfig
	if readJSON(filepath.Join(c.dir, "config.json"), &cached) == nil {
		c.cfg = &cached
	}
	c.agg = NewAggregator(c.user)
	c.uia = NewUIA()

	if !c.Dry {
		key, err := QueueKey(c.dir)
		if err != nil {
			return fmt.Errorf("chave da fila: %w", err)
		}
		if c.queue, err = OpenQueue(filepath.Join(c.dir, "fila.db"), key); err != nil {
			return err
		}
		dev, err := waitDevice()
		if err != nil {
			return err
		}
		c.client = NewClient(dev.Server, dev.Token)
	}
	c.pending.Sessions = append(c.pending.Sessions, SessionEvent{WinUser: c.user, TS: time.Now().UTC().Format(time.RFC3339), Kind: "logon"})
	systray.Run(c.onTrayReady, func() {})
	return nil
}

// waitDevice espera o serviço matricular o PC (primeira execução).
func waitDevice() (*Device, error) {
	for i := 0; ; i++ {
		if d, err := LoadDevice(); err == nil {
			return d, nil
		} else if i == 360 {
			return nil, fmt.Errorf("computador ainda não matriculado: %w", err)
		}
		time.Sleep(10 * time.Second)
	}
}

func (c *Collector) onTrayReady() {
	systray.SetIcon(iconGreen)
	systray.SetTitle("Vantor Pulse")
	c.tray.status = systray.AddMenuItem("Vantor Pulse", "")
	c.tray.status.Disable()
	systray.AddSeparator()
	pause := systray.AddMenuItem("Pausar", "Registrar uma pausa")
	c.tray.reasons = map[string]*systray.MenuItem{}
	for _, r := range c.reasons() {
		item := pause.AddSubMenuItem(r.Label, "")
		c.tray.reasons[r.ID] = item
		go func(r PauseReason) {
			for range item.ClickedCh {
				c.startPause(r)
			}
		}(r)
	}
	c.tray.resume = systray.AddMenuItem("Voltar ao trabalho", "Encerrar a pausa")
	c.tray.resume.Hide()
	systray.AddSeparator()
	c.tray.portal = systray.AddMenuItem("Ver meus números", "Abrir o Portal do Colaborador")
	c.tray.term = systray.AddMenuItem("Ler o Termo de Ciência", "")
	c.tray.about = systray.AddMenuItem("O que é registrado?", "")
	go func() {
		for {
			select {
			case <-c.tray.resume.ClickedCh:
				c.endPause("")
			case <-c.tray.portal.ClickedCh:
				if c.cfg != nil && c.cfg.PortalURL != "" {
					_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", c.cfg.PortalURL).Start()
				}
			case <-c.tray.term.ClickedCh:
				if c.cfg != nil && c.cfg.Term != nil {
					Notify("Termo de Ciência", c.cfg.Term.Body)
				}
			case <-c.tray.about.ClickedCh:
				Notify("O que o Vantor Pulse registra", aboutText)
			}
		}
	}()
	go c.loop()
}

const aboutText = `Só no horário de trabalho, neste computador da empresa:
• programa em uso e título da janela;
• só o domínio do site (ex.: web.whatsapp.com), nunca a página;
• quantos segundos por minuto houve teclado ou mouse (nunca o que foi digitado);
• ligar, desligar, bloquear e desbloquear.

Nunca: conteúdo digitado, senhas, mensagens, áudio, microfone, webcam, arquivos ou localização.`

func (c *Collector) reasons() []PauseReason {
	if c.cfg != nil && len(c.cfg.PauseReasons) > 0 {
		return c.cfg.PauseReasons
	}
	return []PauseReason{{ID: "almoco", Label: "Almoço", DailyLimitMin: 90}, {ID: "cafe", Label: "Café", DailyLimitMin: 30}, {ID: "pessoal", Label: "Pessoal", DailyLimitMin: 30}}
}

func (c *Collector) loop() {
	sec := time.NewTicker(time.Second)
	var n int
	for now := range sec.C {
		n++
		c.step(now, n%5 == 0)
		if n%60 == 0 {
			c.flush(now)
		}
	}
}

// step roda a cada segundo; a cada 5 s também lê a janela em foco.
func (c *Collector) step(now time.Time, sample bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensureConsent()
	collecting := c.collecting(now)

	if tick := LastInputTick(); tick != c.lastTick {
		c.lastTick = tick
		if collecting {
			c.agg.Input(now)
		}
	}
	if !sample {
		return
	}
	exe, title, hwnd := Foreground()
	locked := exe == "" || exe == "lockapp.exe" || exe == "logonui.exe"
	if locked != c.locked && collecting {
		kind := "unlock"
		if locked {
			kind = "lock"
		}
		c.pending.Sessions = append(c.pending.Sessions, SessionEvent{WinUser: c.user, TS: now.UTC().Format(time.RFC3339), Kind: kind})
	}
	c.locked = locked
	if !collecting || locked {
		c.agg.Discard(now)
		c.setStatus(now, "")
		return
	}
	domain := ""
	if IsBrowser(exe) {
		domain = c.uia.Domain(hwnd)
	}
	s := Sample{At: now, App: exe, Domain: domain, Title: title, Fullscreen: IsFullscreen(hwnd)}
	c.agg.Focus(s, 5)

	state := "active"
	idle := 5
	if c.cfg != nil && c.cfg.IdleMinutes > 0 {
		idle = c.cfg.IdleMinutes
	}
	if IdleSeconds() >= idle*60 && !s.Fullscreen {
		state = "idle"
	}
	if state != c.live.State || exe != c.live.App || domain != c.live.Domain {
		c.live = Live{WinUser: c.user, State: state, App: exe, Domain: domain, Since: now.UTC().Format(time.RFC3339)}
	}
	c.live.Title = truncate(title, 300)
	c.setStatus(now, "Monitorando (horário de trabalho)")
}

// collecting: só com o termo aceito, dentro do expediente e fora de pausa.
func (c *Collector) collecting(now time.Time) bool {
	if c.Dry {
		return c.paused == nil
	}
	if c.cfg == nil || c.cfg.Term == nil || !c.state.Accepted(c.cfg.Term.Version) {
		return false
	}
	return c.paused == nil && c.cfg.InWindow(c.user, now)
}

func (c *Collector) setStatus(now time.Time, s string) {
	switch {
	case c.paused != nil:
		s = "Em pausa"
	case s != "":
	case c.cfg == nil:
		s = "Conectando ao servidor…"
	case c.cfg.Term == nil:
		s = "Aguardando a empresa publicar o Termo de Ciência"
	case !c.state.Accepted(c.cfg.Term.Version):
		s = "Parado: Termo de Ciência não aceito"
	case !c.cfg.InWindow(c.user, now):
		s = "Fora do expediente: nada é registrado"
	case c.locked:
		s = "Computador bloqueado"
	}
	if s == c.status || c.tray.status == nil {
		return
	}
	c.status = s
	c.tray.status.SetTitle(s)
	systray.SetTooltip("Vantor Pulse: " + s)
	switch {
	case c.paused != nil:
		systray.SetIcon(iconBlue)
	case s == "Monitorando (horário de trabalho)":
		systray.SetIcon(iconGreen)
	default:
		systray.SetIcon(iconGray)
	}
}

// ensureConsent mostra o Termo de Ciência na primeira vez (e a cada nova versão).
func (c *Collector) ensureConsent() {
	if c.Dry || c.asking || c.cfg == nil || c.cfg.Term == nil || c.state.Accepted(c.cfg.Term.Version) || time.Since(c.lastAsk) < time.Hour {
		return
	}
	c.asking = true
	term := *c.cfg.Term
	go func() {
		ok := Ask("Vantor Pulse: Termo de Ciência", term.Body+"\n\nVocê declara que leu e está ciente? (Sim = aceito)")
		c.mu.Lock()
		defer c.mu.Unlock()
		c.asking = false
		c.lastAsk = time.Now()
		if ok {
			c.state.AcceptedTerms = append(c.state.AcceptedTerms, term.Version)
			c.pending.Consents = append(c.pending.Consents, Consent{WinUser: c.user, TermVersion: term.Version, AcceptedAt: time.Now().UTC().Format(time.RFC3339)})
			c.saveState()
		}
	}()
}

func (c *Collector) startPause(r PauseReason) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.paused != nil {
		return
	}
	c.resetPauseDay()
	if r.DailyLimitMin > 0 && c.state.PauseUsedMin[r.ID] >= r.DailyLimitMin {
		go Notify("Vantor Pulse", fmt.Sprintf("O limite diário de %s (%d min) já foi usado hoje.", r.Label, r.DailyLimitMin))
		return
	}
	now := time.Now()
	c.agg.Discard(now)
	c.paused = &PauseRec{WinUser: c.user, Reason: r.ID, StartedAt: now.UTC().Format(time.RFC3339)}
	c.pending.Pauses = append(c.pending.Pauses, *c.paused)
	c.live = Live{WinUser: c.user, State: "paused", Since: c.paused.StartedAt, PauseReason: r.ID}
	c.tray.resume.Show()
	c.setStatus(now, "")
	if r.DailyLimitMin > 0 {
		go func(started string, left int) {
			time.Sleep(time.Duration(left) * time.Minute)
			c.endPause(started)
		}(c.paused.StartedAt, r.DailyLimitMin-c.state.PauseUsedMin[r.ID])
	}
}

// endPause encerra a pausa atual. Com `onlyStartedAt`, só se ainda for aquela pausa (fim automático pelo limite).
func (c *Collector) endPause(onlyStartedAt string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.paused == nil || (onlyStartedAt != "" && c.paused.StartedAt != onlyStartedAt) {
		return
	}
	reason := onlyStartedAt
	now := time.Now()
	end := now.UTC().Format(time.RFC3339)
	c.paused.EndedAt = &end
	c.pending.Pauses = append(c.pending.Pauses, *c.paused)
	if start, err := time.Parse(time.RFC3339, c.paused.StartedAt); err == nil {
		c.resetPauseDay()
		c.state.PauseUsedMin[c.paused.Reason] += int(now.Sub(start).Minutes())
		c.saveState()
	}
	if reason != "" {
		go Notify("Vantor Pulse", "O limite diário desta pausa acabou. O registro voltou.")
	}
	c.paused = nil
	c.live = Live{}
	c.tray.resume.Hide()
	c.setStatus(now, "")
}

func (c *Collector) resetPauseDay() {
	today := time.Now().Format("2006-01-02")
	if c.state.PauseUsedDay != today || c.state.PauseUsedMin == nil {
		c.state.PauseUsedDay = today
		c.state.PauseUsedMin = map[string]int{}
	}
}

func (c *Collector) saveState() { _ = writeJSON(filepath.Join(c.dir, "estado.json"), c.state) }

// flush fecha os minutos, enfileira o lote e tenta enviar a fila.
func (c *Collector) flush(now time.Time) {
	c.mu.Lock()
	b := c.pending
	c.pending = Batch{}
	b.BatchID = newID()
	b.AgentVersion = Version
	b.Minutes = c.agg.Flush(now)
	if c.paused != nil || (c.collecting(now) && !c.locked && c.live.State != "") {
		b.Live = []Live{c.live}
	}
	c.mu.Unlock()

	if c.Dry {
		out, _ := json.MarshalIndent(b, "", "  ")
		fmt.Println(string(out))
		return
	}
	if err := c.queue.Push(&b); err != nil {
		log.Printf("fila: %v", err)
	}
	go func() {
		err := c.queue.Flush(c.client, func(cfg *ServerConfig) {
			c.mu.Lock()
			c.cfg = cfg
			c.mu.Unlock()
			_ = writeJSON(filepath.Join(c.dir, "config.json"), cfg)
		})
		if err == ErrRevoked {
			c.mu.Lock()
			c.cfg = nil
			c.mu.Unlock()
		} else if err != nil {
			log.Printf("envio adiado (%d lotes na fila): %v", c.queue.Len(), err)
		}
	}()
}

// LoadDevice lê a identidade do PC gravada pelo serviço.
func LoadDevice() (*Device, error) {
	enc, err := os.ReadFile(devicePath())
	if err != nil {
		return nil, err
	}
	raw, err := Unprotect(enc)
	if err != nil {
		return nil, err
	}
	var d Device
	return &d, json.Unmarshal(raw, &d)
}

func SaveDevice(d *Device) error {
	raw, _ := json.Marshal(d)
	enc, err := Protect(raw)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(MachineDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(devicePath(), enc, 0o644)
}
