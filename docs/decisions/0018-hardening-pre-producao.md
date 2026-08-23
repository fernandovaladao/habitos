# ADR 0018 — Hardening pré-produção e papéis do Cloud Run

## Contexto

Os 31 requisitos funcionais da ERS estão implementados. Implementação, homologação local/Emulators e homologação em produção são estados distintos: testes automatizados não substituem a validação dos provedores e da infraestrutura reais.

O mesmo binário atende a aplicação web e ao processador de lembretes. A preparação para produção precisa falhar de forma segura, sem reintroduzir funcionamento offline como requisito.

## Decisão

- Corpos JSON usam um decodificador central com limite de 1 MiB, rejeição de campos desconhecidos, JSON vazio ou malformado e qualquer conteúdo posterior ao primeiro objeto.
- A CSP permanece sem `unsafe-inline`. Conteúdo dinâmico do gráfico de Progresso usa atributos SVG, não estilo inline.
- Produção emite HSTS por um ano, inicialmente sem `preload` e sem `includeSubDomains`, além de `Permissions-Policy`. Respostas privadas usam `Cache-Control: no-store`.
- Produção rejeita qualquer configuração de Firebase Emulator e exige `APP_BASE_URL` HTTPS.
- Rate limiting é uma defesa adicional em memória e por instância para bordas sensíveis. Ele não constitui quota global distribuída nem substitui limites dos provedores.
  - após autenticação, a chave é o UID validado;
  - antes da autenticação, usa-se preferencialmente um hash truncado do cookie CSRF aleatório emitido pela aplicação;
  - sem cookie CSRF, o fallback é o host de `RemoteAddr` observado pelo servidor;
  - `X-Forwarded-For` não participa da chave, pois é um header que pode conter valores fornecidos pelo cliente. No Cloud Run, o fallback `RemoteAddr` pode representar o proxy de ingresso e agrupar clientes; por isso ele é apenas defesa adicional, não identificação confiável nem quota global.
  - sessão: 10 tentativas/minuto por cookie CSRF/origem;
  - sugestão de IA: 10/minuto por UID;
  - foto: 6/minuto por UID;
  - subscription Push: 20/minuto por UID;
  - início da exclusão: 5/minuto por UID;
  - continuação idempotente da exclusão: 60/minuto por UID, para não impedir a conclusão em volumes maiores.
- Logs HTTP são estruturados e contêm request ID de 128 bits gerado internamente pelo servidor, método, caminho, status e duração. Valores de `X-Request-ID` recebidos do cliente são ignorados. Os logs não contêm query string, corpo, e-mail, token, endpoint/chaves Push, nota, prompt ou resposta externa.
- A mesma imagem é implantável em dois papéis:
  - web público: `REMINDER_PROCESSOR_ENABLED=false`, timeout de escrita padrão de 30 segundos;
  - processador privado: `REMINDER_PROCESSOR_ENABLED=true`, sem acesso anônimo, timeout de escrita padrão de 10 minutos.
- A autenticação do processador em produção permanece responsabilidade do IAM do Cloud Run e do OIDC do Cloud Scheduler, sem validação JWT artesanal na aplicação.
- `firestore.indexes.json` inclui apenas os índices compostos correspondentes às consultas atuais. As consultas de Progresso que combinam proprietário e intervalo civil exigem:
  - `executions`: `ownerUid ASC, scheduledDate ASC`;
  - `habitBonusAwards`: `ownerUid ASC, triggerScheduledDate ASC`.
- O contexto Docker exclui configurações locais, segredos, logs, caches e artefatos.

## Consequências

- A homologação de produção ainda exige OpenAI, Resend, Web Push, Firebase/GCP e domínio reais.
- Limites por instância podem ser multiplicados horizontalmente; uma quota global futura exigirá infraestrutura distribuída e nova decisão.
- HSTS só é emitido quando `APP_ENV=production` e deve ser ativado apenas depois de confirmar HTTPS funcional.
- Manifest, service worker e cache estático existentes continuam sem promessa de funcionamento offline, conforme ADR 0017.
