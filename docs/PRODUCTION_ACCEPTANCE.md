# Aceite final e preparação para produção

## Estados de entrega

- **Implementado:** comportamento presente no código e coberto por testes proporcionais ao risco.
- **Homologado localmente/Emulators:** suítes comuns, concorrência, HTTP, E2E local e Auth/Firestore/Storage Emulators aprovadas no mesmo commit.
- **Homologado em produção:** configuração real, deploy, smoke tests e provedores externos aprovados. Este estado só pode ser marcado depois do deploy.

Os 31 RFs vigentes estão implementados. A homologação local e a homologação em produção são registradas separadamente no relatório de release.

## Aceite automatizado local

O baseline mínimo é Go 1.26.6. Versões anteriores contêm vulnerabilidades alcançáveis na biblioteca padrão e não são aceitas para build de produção.

```bash
gofmt -w $(find . -name '*.go' -not -path './vendor/*')
go mod tidy
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
govulncheck ./...
git diff --check
bash scripts/check-secrets.sh
```

Validar também `firebase.json`, `.firebaserc`, `firestore.indexes.json`, manifests Node, build Docker, usuário não root, Playwright/axe e a suíte opt-in dos Emulators.

Com as dependências de teste instaladas, o Firebase CLI pode envolver as duas suítes:

```bash
npx firebase-tools@15.25.1 emulators:exec \
  --project demo-habitos-local \
  --only auth,firestore,storage \
  "bash scripts/test-emulators.sh"
```

## E2E

Os testes em `tests/e2e` cobrem páginas públicas, viewports e o fluxo principal cadastro → perfil → hábito quando o backend e os Emulators estão ativos. IA, Push e E-mail usam fakes nas suítes automatizadas.

```bash
cd tests/e2e
npm ci
npx playwright install chromium firefox
cd ../..
```

`tests/e2e/go.mod` é uma fronteira vazia de ferramentas: impede que comandos do módulo Go principal percorram `node_modules`; não contém código nem dependências Go.

Safari real, leitor de tela, foco/retorno de foco dos diálogos, contraste visual, zoom/reflow e entregas reais permanecem aceite manual. A suíte automatiza Chromium desktop/mobile e Firefox; Safari não é inferido a partir de outro navegador.

## Papéis da mesma imagem

| Configuração | Web público | Processador privado |
|---|---|---|
| `APP_ENV` | `production` | `production` |
| `REMINDER_PROCESSOR_ENABLED` | `false` | `true` |
| acesso Cloud Run | público | IAM, sem acesso anônimo |
| `HTTP_WRITE_TIMEOUT` | `30s` padrão | `10m` padrão |
| Scheduler | não chama | OIDC, SA dedicada com `roles/run.invoker` |
| segredos de envio | não necessários | VAPID privada e Resend |

Nenhum papel de produção aceita variáveis de Emulator. `APP_BASE_URL` deve ser HTTPS. O processador não aceita sessão de usuário e `REMINDER_PROCESSOR_ENABLED` não autoriza uma chamada: em produção, a autorização é feita pelo Cloud Run.

O rate limiter não confia em `X-Forwarded-For`: usa UID autenticado, hash do cookie CSRF no fluxo anônimo e, apenas como fallback, o host de `RemoteAddr`. No Cloud Run esse fallback pode ser o proxy de ingresso, reforçando que o limite é defesa local por instância, não quota ou identidade global.

## Índices mínimos de Progresso

`internal/progress/firestore.go` executa estas consultas:

1. `executions`: `ownerUid == uid`, `scheduledDate >= início`, `scheduledDate <= fim`, ordenada por `scheduledDate ASC`.
2. `habitBonusAwards`: `ownerUid == uid`, `triggerScheduledDate >= início`, `triggerScheduledDate <= fim`, ordenada por `triggerScheduledDate ASC`.

Por isso o arquivo versionado contém exatamente os índices `ownerUid + scheduledDate` e `ownerUid + triggerScheduledDate`. Índices do Ranking e dos lembretes permanecem justificados por suas consultas canônicas. O Emulator não comprova necessidade de índices compostos; o estado dos índices deve ser confirmado no Firestore real antes do tráfego.

## Homologação de produção pendente

- Firebase Auth e domínios autorizados;
- Rules, índices e TTL publicados e ativos;
- dois serviços Cloud Run e IAM mínimo;
- Cloud Scheduler com OIDC/audience exata;
- Secret Manager e ADC, sem service account em arquivo;
- uma chamada controlada à OpenAI;
- Push real em navegador/dispositivo suportado;
- e-mail real com domínio/remetente Resend verificado;
- upload privado no bucket real;
- exclusão integral de conta de teste e varredura residual;
- logs/alertas e rollback.

Funcionamento offline não integra o aceite do MVP.
