// Package agent é o coletor do Vantor Pulse. Este arquivo agrega amostras em minutos,
// no formato que a API espera (uma linha por minuto, com o tempo de cada app). Não depende do Windows.
package agent

import (
	"net/url"
	"sort"
	"strings"
	"time"
)

// App é o tempo de um programa (e domínio, se for navegador) dentro de um minuto.
type App struct {
	App     string `json:"app"`
	Domain  string `json:"domain,omitempty"`
	Title   string `json:"title,omitempty"`
	Seconds int    `json:"seconds"`
}

// Minute é o que vai para a API: segundos com teclado/mouse, trocas de janela e apps.
type Minute struct {
	WinUser  string    `json:"winUser"`
	TS       time.Time `json:"ts"`
	Input    int       `json:"input"`
	Switches int       `json:"switches"`
	Media    bool      `json:"media"`
	Apps     []App     `json:"apps"`
}

// Sample é uma leitura da janela em foco, a cada 5 s.
type Sample struct {
	At         time.Time
	App        string // executável em minúsculas
	Domain     string // só o domínio do site, quando é navegador
	Title      string
	Fullscreen bool
}

// Aggregator junta as leituras de 1 s (atividade) e 5 s (janela em foco) por minuto.
type Aggregator struct {
	WinUser string
	minutes map[int64]*minuteAcc
	lastKey string
}

type minuteAcc struct {
	inputSecs map[int]bool
	switches  int
	media     bool
	apps      map[string]*App
	order     []string
}

func NewAggregator(winUser string) *Aggregator {
	return &Aggregator{WinUser: winUser, minutes: map[int64]*minuteAcc{}}
}

func (a *Aggregator) acc(t time.Time) *minuteAcc {
	k := t.Truncate(time.Minute).Unix()
	m := a.minutes[k]
	if m == nil {
		m = &minuteAcc{inputSecs: map[int]bool{}, apps: map[string]*App{}}
		a.minutes[k] = m
	}
	return m
}

// Input registra que houve teclado ou mouse naquele segundo (só a contagem, nunca o conteúdo).
func (a *Aggregator) Input(t time.Time) {
	a.acc(t).inputSecs[t.Second()] = true
}

// Focus atribui `secs` segundos ao app em foco.
func (a *Aggregator) Focus(s Sample, secs int) {
	if s.App == "" || secs <= 0 {
		return
	}
	m := a.acc(s.At)
	key := s.App + "|" + s.Domain
	if key != a.lastKey {
		if a.lastKey != "" {
			m.switches++
		}
		a.lastKey = key
	}
	if s.Fullscreen && isMediaApp(s.App, s.Domain) {
		m.media = true
	}
	ap := m.apps[key]
	if ap == nil {
		ap = &App{App: s.App, Domain: s.Domain}
		m.apps[key] = ap
		m.order = append(m.order, key)
	}
	ap.Title = truncate(s.Title, 300)
	ap.Seconds += secs
}

// Flush devolve os minutos fechados (anteriores a `now`) e os remove da memória.
func (a *Aggregator) Flush(now time.Time) []Minute {
	cur := now.Truncate(time.Minute).Unix()
	var out []Minute
	keys := make([]int64, 0, len(a.minutes))
	for k := range a.minutes {
		if k < cur {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, k := range keys {
		m := a.minutes[k]
		delete(a.minutes, k)
		if len(m.apps) == 0 {
			continue // sem janela em foco (bloqueado, fora do expediente): não conta como logado
		}
		apps := make([]App, 0, len(m.order))
		total := 0
		for _, key := range m.order {
			apps = append(apps, *m.apps[key])
			total += m.apps[key].Seconds
		}
		// Amostras de 5 s podem passar de 60 s num minuto: reescala para fechar em 60.
		if total > 60 {
			sum := 0
			for i := range apps {
				apps[i].Seconds = max(1, apps[i].Seconds*60/total)
				sum += apps[i].Seconds
			}
			for sum > 60 {
				for i := range apps {
					if sum > 60 && apps[i].Seconds > 1 {
						apps[i].Seconds--
						sum--
					}
				}
			}
		}
		out = append(out, Minute{WinUser: a.WinUser, TS: time.Unix(k, 0).UTC(), Input: min(60, len(m.inputSecs)), Switches: m.switches, Media: m.media, Apps: apps})
	}
	return out
}

// Discard joga fora o minuto em andamento (ex.: começou uma pausa ou acabou o expediente).
func (a *Aggregator) Discard(now time.Time) {
	delete(a.minutes, now.Truncate(time.Minute).Unix())
	a.lastKey = ""
}

var mediaApps = map[string]bool{"zoom.exe": true, "ms-teams.exe": true, "teams.exe": true, "webex.exe": true, "ciscocollabhost.exe": true, "vlc.exe": true}
var mediaDomains = map[string]bool{"meet.google.com": true, "teams.microsoft.com": true, "zoom.us": true, "youtube.com": true, "whereby.com": true, "meet.jit.si": true}

func isMediaApp(app, domain string) bool { return mediaApps[app] || mediaDomains[domain] }

// DomainOf extrai só o domínio do que está na barra de endereço ("https://www.site.com/x?y" → "site.com").
func DomainOf(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" || strings.ContainsAny(addr, " \t") {
		return "" // texto de busca digitado, não é endereço
	}
	if !strings.Contains(addr, "://") {
		addr = "https://" + addr
	}
	u, err := url.Parse(addr)
	if err != nil || u.Hostname() == "" || !strings.Contains(u.Hostname(), ".") && u.Hostname() != "localhost" {
		return ""
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
