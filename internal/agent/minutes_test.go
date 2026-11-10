package agent

import (
	"testing"
	"time"
)

func TestAggregatorMinute(t *testing.T) {
	base := time.Date(2026, 11, 10, 11, 0, 0, 0, time.UTC)
	a := NewAggregator("ana")
	for s := 0; s < 60; s++ {
		if s < 20 {
			a.Input(base.Add(time.Duration(s) * time.Second))
		}
	}
	for s := 0; s < 60; s += 5 {
		app := Sample{At: base.Add(time.Duration(s) * time.Second), App: "excel.exe", Title: "Planilha"}
		if s >= 40 {
			app = Sample{At: app.At, App: "chrome.exe", Domain: "youtube.com", Title: "YouTube"}
		}
		a.Focus(app, 5)
	}
	got := a.Flush(base.Add(61 * time.Second))
	if len(got) != 1 {
		t.Fatalf("minutos = %d, quer 1", len(got))
	}
	m := got[0]
	if m.Input != 20 || m.Switches != 1 || len(m.Apps) != 2 || m.Apps[0].Seconds != 40 || m.Apps[1].Seconds != 20 || m.Apps[1].Domain != "youtube.com" {
		t.Fatalf("minuto inesperado: %+v", m)
	}
	if len(a.Flush(base.Add(3*time.Minute))) != 0 {
		t.Fatal("flush deveria esvaziar")
	}
}

func TestAggregatorCapsAt60AndSkipsEmpty(t *testing.T) {
	base := time.Date(2026, 11, 10, 11, 0, 0, 0, time.UTC)
	a := NewAggregator("ana")
	for s := 0; s < 70; s += 5 { // 14 amostras de 5 s = 70 s no mesmo minuto (atraso do timer)
		a.Focus(Sample{At: base.Add(time.Duration(s%60) * time.Second), App: "word.exe"}, 5)
	}
	a.Input(base.Add(2 * time.Minute)) // minuto só com teclado, sem janela: descartado
	got := a.Flush(base.Add(5 * time.Minute))
	if len(got) != 1 || got[0].Apps[0].Seconds != 60 {
		t.Fatalf("esperado 1 minuto com 60 s: %+v", got)
	}
}

func TestMediaOnlyForMeetingOrVideo(t *testing.T) {
	base := time.Date(2026, 11, 10, 11, 0, 0, 0, time.UTC)
	a := NewAggregator("ana")
	a.Focus(Sample{At: base, App: "excel.exe", Fullscreen: true}, 5)
	a.Focus(Sample{At: base.Add(time.Minute), App: "chrome.exe", Domain: "meet.google.com", Fullscreen: true}, 5)
	got := a.Flush(base.Add(2 * time.Minute))
	if got[0].Media || !got[1].Media {
		t.Fatalf("media errado: %+v", got)
	}
}

func TestDomainOf(t *testing.T) {
	cases := map[string]string{
		"https://www.Instagram.com/p/abc?x=1": "instagram.com",
		"web.whatsapp.com":                    "web.whatsapp.com",
		"mail.google.com/mail/u/0/#inbox":     "mail.google.com",
		"como fazer bolo":                     "",
		"chrome://settings":                   "",
		"":                                    "",
		"localhost:3000/mural":                "localhost",
	}
	for in, want := range cases {
		if got := DomainOf(in); got != want {
			t.Errorf("DomainOf(%q) = %q, quer %q", in, got, want)
		}
	}
}
