//go:build linux

// vantor-agent: agente do Vantor Pulse para Linux (desktop X11).
//
//	sudo vantor-agent enroll --server https://... --token CODIGO   matricula o PC (uma vez, como root)
//	vantor-agent collect    coletor da sessão do usuário (o systemd --user abre no login)
//	vantor-agent test       mostra no terminal, a cada minuto, exatamente o que seria enviado
//	vantor-agent version
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/debug"

	"github.com/Magnataflowless/vantor-pulse-agent/internal/agent"
)

func main() {
	// Meta: até 60 MB de RAM e 1% de CPU.
	runtime.GOMAXPROCS(2)
	debug.SetMemoryLimit(24 << 20)
	debug.SetGCPercent(25)
	cmd := "collect"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "enroll":
		fs := flag.NewFlagSet("enroll", flag.ExitOnError)
		server := fs.String("server", "", "endereço do painel (ex.: https://seu-nome.duckdns.org)")
		token := fs.String("token", "", "código da empresa (painel > Computadores)")
		_ = fs.Parse(os.Args[2:])
		if os.Geteuid() != 0 {
			fmt.Fprintln(os.Stderr, "rode como administrador: sudo vantor-agent enroll --server ... --token ...")
			os.Exit(1)
		}
		check(agent.Enroll(*server, *token))
		fmt.Println("Computador matriculado. O registro começa no próximo login de cada usuário (ou agora: systemctl --user start vantor-agent).")
	case "collect":
		if err := (&agent.Collector{}).Run(); err != nil {
			log.Fatal(err)
		}
	case "test":
		fmt.Println("Modo de teste: nada é enviado. A cada minuto aparece o lote que iria para o servidor.")
		if err := (&agent.Collector{Dry: true}).Run(); err != nil {
			log.Fatal(err)
		}
	case "version":
		fmt.Println(agent.Version)
	default:
		fmt.Println("uso: vantor-agent enroll --server URL --token CODIGO | collect | test | version")
		os.Exit(2)
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
