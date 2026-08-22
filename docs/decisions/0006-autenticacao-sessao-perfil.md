# ADR 0006 — Autenticação, sessão web e bootstrap de perfil

- **Status:** Aceita
- **Data:** 2026-08-22

## Contexto

O navegador autentica usuários com Firebase Authentication, enquanto páginas protegidas são renderizadas pelo backend Go. O backend precisa derivar a identidade exclusivamente de credenciais Firebase validadas, oferecer sessões adequadas a navegação tradicional e recuperar de forma segura contas cuja identidade exista, mas cujo perfil Firestore esteja ausente ou incompleto.

## Decisão

O Firebase Auth Web SDK realizará cadastro, login, recuperação e alteração de senha. Após cadastro ou login, o navegador enviará o ID token ao endpoint de sessão. Esse endpoint validará o ID token e exigirá autenticação recente com base no claim `auth_time`: somente tokens autenticados nos últimos 5 minutos, incluindo exatamente o limite de 5 minutos, poderão originar uma sessão. Depois dessa validação, o endpoint criará somente uma sessão Firebase com duração de 5 dias; ele não criará nem atualizará o perfil.

O cookie de sessão terá `HttpOnly`, `SameSite=Lax` e `Path=/`. `Secure=true` será obrigatório em produção e configurável em desenvolvimento local por HTTP. Criação e remoção de sessão e todas as demais operações mutáveis autenticadas por cookie terão proteção CSRF explícita.

O middleware validará a sessão Firebase e colocará no contexto o UID e o e-mail obtidos exclusivamente da credencial validada. UID e e-mail enviados pelo cliente nunca serão usados como identidade ou autorização.

Depois da criação da sessão, uma operação separada `EnsureProfile` usará exclusivamente a identidade autenticada do contexto. Se `users/{uid}` não existir, ela criará idempotentemente um perfil mínimo de recuperação com UID e e-mail validados, timezone IANA válido quando informado, `rankingOptIn=false` e estado incompleto. Perfil sem apelido ou idade válidos continuará incompleto até o usuário fornecer esses dados. Nesta fase, idade é válida quando for um inteiro positivo, sem faixa mínima ou máxima adicional.

Apelido, idade, timezone IANA e `rankingOptIn` poderão ser editados. O ranking não será criado nesta fase.

`FIREBASE_WEB_API_KEY`, `FIREBASE_AUTH_DOMAIN` e `FIREBASE_APP_ID` são configurações públicas fornecidas ao Firebase Web SDK. Credenciais administrativas permanecem exclusivamente no backend. Em produção no Cloud Run, o SDK administrativo usará Application Default Credentials da identidade do serviço. Nenhum arquivo de service account será armazenado no repositório.

O Firebase Authentication Emulator e o Firestore Emulator serão suportados por variáveis de ambiente no desenvolvimento local.

## Consequências

- Criar uma sessão com sucesso não implica que o perfil esteja completo.
- ID token com `auth_time` ausente, futuro ou anterior à janela aceita não origina cookie de sessão e exige nova autenticação.
- Handlers de perfil não aceitam UID ou e-mail do cliente como fonte de identidade.
- Repetir `EnsureProfile` é seguro e não sobrescreve um perfil existente.
- Testes de autorização podem substituir validação Firebase e Firestore por fakes.
- A interface deve executar `EnsureProfile` após estabelecer a sessão e direcionar perfis incompletos para conclusão.
- Logout local expira o cookie de sessão; não revoga automaticamente sessões em outros dispositivos.
