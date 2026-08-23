# HÁBITOS

MVP de uma aplicação web responsiva para adolescentes, focada na formação e manutenção de hábitos positivos usando os **4 Rs da formação de hábitos**: **Relembrar, Rotina, Recompensa e Repetição**.

## Objetivo
Permitir criar hábitos, definir metas e lembretes, registrar execuções, acompanhar progresso, receber pontos/conquistas e visualizar um ranking geral com privacidade.

## Stack aprovada
- Backend: Go (Golang)
- Frontend: HTML + CSS + JavaScript leve; HTMX quando simplificar
- Aplicação web responsiva. O manifest, o service worker e o cache estático existentes são suporte técnico, sem promessa de funcionamento offline.
- Hospedagem: Google Cloud Run
- Banco: Google Cloud Firestore
- Autenticação: Firebase Authentication
- Fotos privadas de perfil: Cloud Storage/Firebase Storage, acessado somente pelo backend
- IA: API acessada somente pelo backend Go
- Versionamento: Git + GitHub

## Documentação
- `docs/ERS_HABITOS_MVP.md`: especificação formal
- `AGENTS.md`: instruções operacionais para agentes de código
- `docs/prototypes/`: referências visuais regeneradas e aprovadas conceitualmente
- `docs/decisions/`: decisões arquiteturais aceitas

## Regra de precedência
A ERS é fonte de verdade para comportamento e regras de negócio. Os protótipos são referência visual. Em conflito funcional, prevalece a ERS.

## Fundação técnica atual

O projeto contém a fundação do monólito modular, autenticação e perfil, gestão de hábitos e execuções com histórico e notas privadas, gamificação, Progresso, Ranking Geral e lembretes reais por Web Push/E-mail. Ocorrências são materializadas e fechadas sob demanda, com snapshots temporais e idempotência no Firestore. A criação de hábito oferece sugestão opcional por IA sem salvamento automático.

## Requisitos locais

- Go 1.25 ou superior.
- Docker, opcional, para validar a imagem de produção.
- Um projeto Firebase com autenticação por e-mail/senha e Firestore, ou Firebase Emulator Suite.
- Node.js e Firebase CLI para executar o Emulator Suite.
- Java 21 ou superior para o Firestore Emulator.

Verifique a versão do Java antes de iniciar os emuladores:

```bash
java -version
```

A saída deve indicar a versão 21 ou posterior.

## Configuração Firebase

Copie `.env.example` para um arquivo local não versionado e exporte as variáveis no shell. O aplicativo não carrega `.env` automaticamente.

```bash
set -a
. ./.env
set +a
```

Variáveis obrigatórias:

- `FIREBASE_PROJECT_ID`
- `FIREBASE_WEB_API_KEY`
- `FIREBASE_AUTH_DOMAIN`
- `FIREBASE_APP_ID`
- `FIREBASE_STORAGE_BUCKET`
- `OPENAI_API_KEY`

Configuração da sugestão de hábito com IA:

- `OPENAI_MODEL`, inicialmente `gpt-5.6-luna`;
- `AI_REQUEST_TIMEOUT`, inicialmente `10s`.

Configuração dos lembretes reais:

- `APP_BASE_URL`, URL pública usada nos links dos e-mails;
- `VAPID_PUBLIC_KEY`, `VAPID_PRIVATE_KEY` e `VAPID_SUBSCRIBER` para Web Push;
- `RESEND_API_KEY`, `EMAIL_FROM` e `EMAIL_REQUEST_TIMEOUT` para E-mail;
- `REMINDER_PROCESSOR_ENABLED=true` somente na implantação privada invocada pelo Cloud Scheduler ou no desenvolvimento local.

`EMAIL_FROM` precisa pertencer a um domínio verificado no Resend antes do smoke test de produção. A implantação privada do mesmo binário deve exigir autenticação IAM do Cloud Run. A service account dedicada do Cloud Scheduler recebe somente `roles/run.invoker`; a implantação pública mantém `REMINDER_PROCESSOR_ENABLED=false`, portanto não registra a rota interna. O Scheduler chama a implantação privada a cada minuto com OIDC. Não há validação JWT artesanal na aplicação porque a autenticação é realizada pelo Cloud Run.

No ambiente local com projeto `demo-habitos-local` e Emulators, a rota pode ser habilitada para processamento manual. Fakes automatizados não enviam Push ou E-mail reais.

A integração usa a OpenAI Responses API com Structured Outputs. Somente título e descrição são enviados pelo backend; a aplicação não persiste nem registra prompts, respostas ou sugestões. Nunca exponha `OPENAI_API_KEY` no frontend ou no repositório. Os testes usam fakes e não chamam a API real.

`FIREBASE_WEB_API_KEY`, `FIREBASE_AUTH_DOMAIN` e `FIREBASE_APP_ID` são configuração pública do Firebase Web SDK, não credenciais administrativas. A configuração administrativa permanece somente no backend.

No Cloud Run, use Application Default Credentials concedendo à identidade do serviço apenas as permissões necessárias. Não envie nem monte arquivos de service account no repositório ou no frontend.

Para desenvolvimento com emuladores, defina:

```bash
export FIREBASE_PROJECT_ID=demo-habitos-local
export GCLOUD_PROJECT=demo-habitos-local
export FIREBASE_AUTH_EMULATOR_HOST=127.0.0.1:9099
export FIRESTORE_EMULATOR_HOST=127.0.0.1:8081
export FIREBASE_STORAGE_EMULATOR_HOST=127.0.0.1:9199
export FIREBASE_STORAGE_BUCKET=demo-habitos-local.appspot.com
```

O host do Auth Emulator não deve conter `http://`. O backend e a configuração pública entregue ao browser são ajustados automaticamente. Quando um emulador está configurado, o backend rejeita qualquer project ID diferente de `demo-habitos-local`. Fora dos emuladores, o SDK usa Application Default Credentials; localmente, `GOOGLE_APPLICATION_CREDENTIALS` pode apontar para um arquivo mantido fora do repositório.

### Teste local ponta a ponta com emuladores

O arquivo `.firebaserc` usa exclusivamente o projeto fictício `demo-habitos-local`. Esse identificador com prefixo `demo-` é local e não representa um projeto Firebase real.

As portas configuradas em `firebase.json` são:

- Authentication Emulator: `127.0.0.1:9099`
- Firestore Emulator: `127.0.0.1:8081`
- Storage Emulator: `127.0.0.1:9199`
- Emulator Suite UI: `127.0.0.1:4000`
- Backend HÁBITOS: `127.0.0.1:8080`

No primeiro terminal, inicie Authentication, Firestore e Storage:

```bash
npx firebase-tools@15.25.1 emulators:start \
  --project demo-habitos-local \
  --only auth,firestore,storage
```

Se o Firebase CLI já estiver instalado globalmente:

```bash
firebase emulators:start \
  --project demo-habitos-local \
  --only auth,firestore,storage
```

No segundo terminal, copie e carregue a configuração local e inicie o backend:

```bash
cp .env.example .env
set -a
. ./.env
set +a
go run ./cmd/web
```

Não defina `GOOGLE_APPLICATION_CREDENTIALS` nesse fluxo. O Admin SDK recebe explicitamente `demo-habitos-local`, o Auth Emulator recebe o mesmo valor por `GCLOUD_PROJECT`, o Firestore usa `FIRESTORE_EMULATOR_HOST` e o Storage usa `FIREBASE_STORAGE_EMULATOR_HOST`. As fotos permanecem privadas e são servidas somente pelo backend após autorização; `storage.rules` nega acesso direto de clientes.

O frontend consulta `/api/firebase-config`. Quando `FIREBASE_AUTH_EMULATOR_HOST` está definido, a resposta inclui `authEmulatorUrl=http://127.0.0.1:9099`, e o Firebase Web SDK chama `connectAuthEmulator` antes de autenticar.

O frontend não carrega o SDK do Firestore. `firestore.rules` nega todas as leituras e escritas diretas de clientes; somente o backend administrativo acessa o Firestore Emulator.

Abra [http://localhost:8080/cadastro](http://localhost:8080/cadastro) para testar cadastro, sessão e perfil. A interface dos emuladores fica em [http://localhost:4000](http://localhost:4000).

### Teste de integração opt-in

O teste comum não depende de emuladores:

```bash
go test ./...
```

Com os emuladores ativos e as variáveis de `.env` carregadas, execute o teste ponta a ponta explicitamente:

```bash
RUN_FIREBASE_EMULATOR_TESTS=1 go test -v ./tests/integration
```

Também é possível deixar o Firebase CLI iniciar e encerrar os emuladores ao redor do teste:

```bash
npx firebase-tools@15.25.1 emulators:exec \
  --project demo-habitos-local \
  --only auth,firestore,storage \
  "RUN_FIREBASE_EMULATOR_TESTS=1 go test -v ./tests/integration"
```

O teste falha antes de acessar a rede se project ID e hosts não corresponderem exatamente à configuração local. Ele cria uma conta temporária no Auth Emulator, troca o ID token por sessão, valida a identidade e testa perfil, foto privada, regras do Storage, hábitos, versionamento de agenda, materialização e registro concorrentes de execuções e CRUD autorizado de notas.

O Firestore Emulator não executa nem comprova políticas TTL. Em produção, `firestore.indexes.json` habilita `expiresAt` como TTL de `accountDeletions`; depois de publicar a configuração, confira o estado com:

```bash
gcloud firestore fields ttls list --collection-group=accountDeletions
```

Esse TTL de sete dias é apenas uma salvaguarda para marcador residual depois que o Firebase Auth já tiver sido excluído. O fluxo normal remove o marcador explicitamente e nunca depende do TTL.

`APP_ENV=production` torna o cookie de sessão obrigatoriamente seguro. Em desenvolvimento HTTP, use `SESSION_COOKIE_SECURE=false`. Sessões duram 5 dias.

## Executar localmente

```bash
go run ./cmd/web
```

O servidor usa a porta informada pela variável de ambiente `PORT`; na ausência dela, usa `8080`.

```bash
PORT=3000 go run ./cmd/web
```

Acesse [http://localhost:8080](http://localhost:8080) e verifique a saúde em [http://localhost:8080/health](http://localhost:8080/health).

Rotas da fase de autenticação:

- `/cadastro`
- `/entrar`
- `/recuperar-senha`
- `/perfil` — protegida
- `/alterar-senha` — protegida

Rotas da gestão de hábitos:

- `/criar-habito` — criação protegida
- `/meus-habitos` — listagem e filtros protegidos
- `/habitos/{id}` — detalhes protegidos
- `/habitos/{id}/editar` — edição protegida

## Testes

```bash
go test ./...
```

## Docker

```bash
docker build -t habitos .
docker run --rm -p 8080:8080 -e PORT=8080 habitos
```

O container é compatível com o contrato do Cloud Run: escuta em `0.0.0.0` na porta definida por `PORT` e encerra de forma graciosa ao receber `SIGTERM`.
