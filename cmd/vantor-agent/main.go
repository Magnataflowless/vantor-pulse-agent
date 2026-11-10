//go:build windows

// vantor-agent: agente do Vantor Pulse para Windows.
//
//	vantor-agent service    roda como serviço (o MSI registra assim)
//	vantor-agent collect    coletor da sessão do usuário (aberto pelo serviço)
//	vantor-agent test       mostra no console, a cada minuto, exatamente o que seria enviado
//	vantor-agent version
package main

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/debug"

	"golang.org/x/sys/windows/svc"

	"github.com/Magnataflowless/vantor-pulse-agent/internal/agent"
)

func main() {
	// Meta: até 60 MB de RAM e 1% de CPU. Poucas threads e coleta de lixo mais frequente.
	runtime.GOMAXPROCS(2)
	debug.SetMemoryLimit(24 << 20)
	debug.SetGCPercent(25)
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	if isSvc, _ := svc.IsWindowsService(); isSvc {
		cmd = "service"
	}
	switch cmd {
	case "service":
		if err := svc.Run(agent.ServiceName, agent.Service{}); err != nil {
			log.Fatal(err)
		}
	case "collect":
		if err := (&agent.Collector{}).Run(); err != nil {
			log.Fatal(err)
		}
	case "test":
		fmt.Println("Modo de teste: nada é enviado. A cada minuto aparece o lote que iria para o servidor.")
		if err := (&agent.Collector{Dry: true}).Run(); err != nil {
			log.Fatal(err)
		}
	case "install-service":
		exe, _ := os.Executable()
		check(agent.InstallService(exe))
	case "remove-service":
		check(agent.RemoveService())
	case "version":
		fmt.Println(agent.Version)
	default:
		fmt.Println("uso: vantor-agent service | collect | test | version")
		os.Exit(2)
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
