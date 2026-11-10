# AGENTS.md — Vantor Pulse (vantor-pulse-agent)

Regras para qualquer agente (de qualquer modelo) e para pessoas que trabalham neste repositório.

## Regras do produto

- **Custo zero**: só software livre e planos gratuitos (lista na seção "Custo zero" do README). Nunca adicione serviço pago sem perguntar ao dono.
- **LGPD e transparência**: nada é capturado escondido. Sem keylogger, sem conteúdo digitado, mensagens, áudio, webcam ou GPS. Só equipamentos da empresa e só no horário de trabalho.
- **Multiempresa**: todo dado de cliente tem `tenant_id` e é isolado por Row Level Security no PostgreSQL.
- Tudo em português do Brasil, fuso America/Sao_Paulo, datas dd/mm/aaaa, valores em R$.

## Fluxo de trabalho: Issues e Pull Requests

- **Toda tarefa vira uma Issue no GitHub** antes de ser implementada, com exatamente um destes labels:
  - `Correção`: algo que está quebrado ou se comporta errado;
  - `Melhoria`: aprimorar algo que já existe;
  - `Nova função`: funcionalidade que ainda não existe.
- Crie os três labels no repositório se ainda não existirem.
- **Nada vai direto para a branch principal.** Cada entrega e cada deploy passam por um Pull Request, feito numa branch própria.
- **Toda descrição de Pull Request deve ter:**
  1. **Issue relacionada**: `Closes #<número>` (ou `Refs #<número>` se não encerra a Issue);
  2. **O que mudou**: resumo objetivo das alterações;
  3. **Como foi validado**: testes, verificações manuais e resultados;
  4. **Riscos, limitações e próximos passos**.
- Mantenha um modelo em `.github/pull_request_template.md` com essas quatro seções.

## Padrões de interface e movimento

- Toda interface deve ter:
  - lazy loading quando fizer sentido;
  - skeleton screens nos carregamentos;
  - animações suaves de entrada e saída;
  - estados de progresso nos elementos interativos (botões, formulários, uploads);
  - feedback visual para as ações do usuário;
  - transições consistentes entre telas, cards, modais e listas;
  - respeito a `prefers-reduced-motion`.
### Qual skill de design usar em cada etapa

Há várias skills de design instaladas que se sobrepõem. Para não gerar orientações conflitantes, siga esta ordem e use **só as skills da etapa atual**. Se duas skills discordarem, vale a da etapa atual.

1. **Direção visual** (definir ideia, composição, fontes, cores e imagens antes de codar): **design-taste-frontend**. Em projeto novo, rode antes `/impeccable init` para registrar o contexto de design do projeto.
2. **Construção**:
   - **frontend-design** para as decisões visuais durante a implementação;
   - componentes: primeiro os que já existem no projeto, depois **shadcn** (skill e MCP), depois **21st.dev** (`21st-cli-use` / `21st-ui-build`). Só crie do zero se nada servir;
   - React/Next.js: **vercel-react-best-practices** (performance) e **vercel-composition-patterns** (componentes reutilizáveis).
3. **Movimento**:
   - **animate** e **emil-design-eng** (Emil Kowalski) para criar animações;
   - **design-motion-principles** como referência complementar;
   - **vercel-react-view-transitions** para transições entre telas em React;
   - **animation-performance** se alguma animação travar.
4. **Revisão final**, como um designer de produto sênior, corrigindo tudo o que parecer brusco, travado, genérico ou amador:
   - **impeccable** (`/impeccable audit` e `/impeccable critique`): auditoria principal de design;
   - **ux-high-level-agent**: screenshots em desktop, tablet e celular, com crítica de UX, acessibilidade e estados de tela;
   - **web-design-guidelines** (Vercel): checagem do código contra as Web Interface Guidelines;
   - **review-animations**: revisão das animações;
   - **break-ui**: teste com dados extremos (textos longos, listas vazias, emojis).

**Use só quando fizer sentido ou quando o usuário pedir:**
- **build-awwwards-quality-sites**: só para sites de marketing premium e cinematográficos;
- **21st-ui-explore**: quando o usuário quiser comparar várias direções visuais;
- **no-ai-design-slop** e **audit-ai-design-slop**: segunda opinião anti-genérico, se a revisão do impeccable não bastar;
- **apple-design**: interfaces com gestos, arrastar e animações com efeito de mola.

## Esteira de qualidade (antes de qualquer merge na branch principal)

A branch principal é protegida: um PR só entra com a CI verde e as checagens abaixo aprovadas. Escolha as ferramentas **quando fizerem sentido para o projeto**: em geral, uma por necessidade, não todas. Justifique a escolha no PR que configurar cada uma.

### 1. Observabilidade
- Erros: **Sentry**.
- Métricas, traces e logs: **OpenTelemetry** como padrão de instrumentação, enviando para um backend (**Datadog** ou **New Relic**). Não adote os dois ao mesmo tempo sem motivo.

### 2. Qualidade e lint de código
- **Biome**: lint e formatação, rodando na CI e antes do commit.
- **Commitlint**: mensagens de commit no padrão Conventional Commits.
- **Knip**: detectar arquivos, exports e dependências sem uso.
- **arch-contract**: regras de dependência entre camadas e módulos (por exemplo, o frontend nunca importa código do backend).
- **Stryker**: testes de mutação no código crítico (regras de negócio), não no projeto inteiro.

### 3. Testes
- **Unitários** para regras de negócio e utilitários.
- **Integração** para API, banco de dados e serviços externos.
- **End-to-end** com **Playwright** para os fluxos principais do usuário. Use o **Endtest** só se houver testes mantidos por pessoas que não programam.
- **Codecov**: cobertura reportada em todo PR. A cobertura não pode cair em código novo.

### 4. Segurança e operação
- **Rate limit** em todas as rotas públicas e de autenticação.
- **Revisão de segurança** em todo PR que mexa em autenticação, permissões, pagamentos, upload ou dados pessoais.
- **Performance budget** definido (tamanho do bundle, LCP, INP) e checado na CI.
- **Backend e frontend separados**, com contrato de API explícito. Segredos nunca vão para o frontend.
- **Termos de uso e política de privacidade**: o agente pode redigir rascunhos, mas eles só vão para produção depois de **revisados e aprovados pelo jurídico** (uma pessoa). Registre essa pendência na Issue e no PR.

### 5. Arquitetura
- Evite overengineering: a solução mais simples que resolve o problema atual.
- Evite gargalos: nada de consultas N+1, chamadas em série que poderiam ser paralelas, ou um único ponto que trava tudo.
- Componentize desde o início, com componentes pequenos e reutilizáveis.
- Aplique DRY com critério: extraia uma abstração só quando houver de fato duas ou três repetições. Não crie abstrações prematuras.
- **Antes de criar um componente, procure se ele já existe** no projeto (no design system ou na pasta de componentes) e reutilize ou estenda o que já existe. Não reconstrua componentes.

## Idioma

- O usuário fala português (Brasil). Responda em português.
