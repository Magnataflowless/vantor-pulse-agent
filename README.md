# Vantor Pulse: agente para Windows

Programa leve, de código aberto (MIT), que roda nos computadores da empresa e alimenta o painel do Vantor Pulse.
O código é público para que qualquer colaborador, empresa ou auditor possa conferir exatamente o que ele faz.

## O que ele registra

Só **no horário de trabalho** configurado pela empresa, só **depois que o colaborador aceita o Termo de Ciência** e nunca durante uma pausa:

| Registra | Como |
| --- | --- |
| Programa em uso e título da janela | a cada 5 s |
| Site em uso | **só o domínio** (ex.: `web.whatsapp.com`), lido da barra de endereço pela UI Automation do Windows |
| Atividade | **quantos segundos por minuto** houve teclado ou mouse. Não há gancho de teclado: o agente só lê o instante do último toque (`GetLastInputInfo`) |
| Sessão | ligar, bloquear, desbloquear, entrar |
| Computador | Windows e versão, processador, memória, disco livre, IP, nome do PC e versão do agente |

**Nunca registra:** o que é digitado, senhas, mensagens, conteúdo de páginas, áudio, microfone, webcam, arquivos pessoais ou localização.

Para ver com os próprios olhos o que seria enviado, rode no Prompt de Comando:

```bat
"C:\Program Files\VantorPulse\vantor-agent.exe" test
```

A cada minuto aparece o lote completo, e nada é enviado.

## Transparência para o colaborador

- Ícone na bandeja do Windows: verde (registrando), azul (pausa), cinza (parado ou fora do expediente).
- Menu do ícone: **Pausar** (almoço, café, pessoal, com limite diário definido pela empresa), **Ver meus números**, **Ler o Termo de Ciência** e **O que é registrado?**
- No primeiro uso aparece o Termo de Ciência. Sem o aceite, nada é registrado. Cada nova versão do termo pede novo aceite.

## Instalação

### Um computador

1. No painel, em **Computadores**, clique em **Baixar instalador**. O arquivo vem com o nome `VantorPulse_<código-da-empresa>.msi`. **Não renomeie**: o código da empresa vai no nome.
2. Dê dois cliques e avance. O computador aparece sozinho no painel em até 1 minuto.
3. No painel, vincule o usuário do Windows ao colaborador.

### Vários computadores (TI, GPO, RMM)

```bat
msiexec /i VantorPulse.msi TOKEN=<código-da-empresa> SERVER=https://seu-servidor /qn
```

### Aviso do Windows (SmartScreen) e antivírus

Enquanto o instalador não tiver assinatura digital (pedida à [SignPath Foundation](https://signpath.org), gratuita para código aberto), o Windows pode mostrar "O Windows protegeu o computador":

1. Clique em **Mais informações**.
2. Clique em **Executar assim mesmo**.

Se o antivírus bloquear, adicione uma exceção para `C:\Program Files\VantorPulse\vantor-agent.exe`. Confira o SHA-256 do arquivo com o `SHA256SUMS.txt` publicado em cada versão.

### Desinstalar

**Configurações → Aplicativos → Vantor Pulse → Desinstalar**, ou `msiexec /x VantorPulse.msi /qn`. O painel avisa o gestor quando um agente para de responder no horário de trabalho.

## Como funciona

- **Serviço `VantorPulse`** (LocalSystem): matricula o PC com o código da empresa, abre o coletor em cada sessão de usuário aberta (vários usuários do Windows no mesmo PC são separados), envia o inventário a cada minuto e aplica atualizações pedidas pelo painel (baixa o MSI por HTTPS e confere o SHA-256 antes de instalar). Se for encerrado, o Windows reinicia o serviço em 5 s.
- **Coletor** (na sessão do usuário): ícone na bandeja, leitura da janela em foco, agregação por minuto e envio a cada 60 s, compactado (gzip) por HTTPS.
- **Sem internet:** os lotes ficam numa fila local SQLite, cada um criptografado com AES-256-GCM. A chave é protegida pelo DPAPI do Windows. Tudo é enviado, em ordem, quando a conexão volta.
- **Consumo medido** (Windows 10, Chrome aberto): cerca de 0,4% de CPU e 49 MB de memória própria.

Protocolo com a API: `POST /api/agent/enroll` e `POST /api/agent/sync` (veja `internal/agent/api.go`).

## Desenvolvimento

```bash
go test ./...
go build -ldflags "-H windowsgui -X github.com/Magnataflowless/vantor-pulse-agent/internal/agent.Version=0.1.0" -o dist/vantor-agent.exe ./cmd/vantor-agent
```

O instalador é gerado pelo GitHub Actions a cada tag `v*` (WiX Toolset v5, gratuito) e publicado em **Releases**.
Ícones da bandeja: `go run ./tools/mkicon`.

Prioridade atual: Windows 10 e 11. Linux vem na Fase 3. macOS fica para depois, porque a Apple cobra para assinar aplicativos.

## Licença

MIT. Veja [LICENSE](LICENSE).
