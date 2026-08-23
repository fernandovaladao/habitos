# ADR 0015 — Exclusão integral da conta e dos dados

- **Status:** Aceita
- **Data:** 2026-08-23

## Contexto

A conta possui dados no Firebase Authentication, Firestore e Storage. Não há transação distribuída entre esses serviços. A exclusão precisa retirar imediatamente a exposição no ranking, impedir novas gravações concorrentes e poder continuar com segurança depois de falhas ou respostas perdidas.

## Decisão

O usuário inicia a operação irreversível digitando literalmente `EXCLUIR MINHA CONTA` e apresentando um ID token cujo `auth_time` tenha no máximo cinco minutos, conforme o ADR 0006. Antes da criação do marcador, o diálogo pode ser cancelado. Depois dela não existe cancelamento.

O backend cria idempotentemente `accountDeletions/{sha256(uid)}` e remove `publicRanking/{uid}` na mesma transação. O UID e o e-mail vêm exclusivamente da sessão e do ID token verificados e devem coincidir. Continuações exigem apenas uma sessão válida da mesma identidade e seu marcador enquanto a conta Auth existir; não repetem a exigência de autenticação recente.

O marcador bloqueia rotas funcionais e também é lido dentro de cada transação persistente capaz de criar ou alterar dados do usuário. Assim, uma transação iniciada antes da exclusão é repetida ou abortada quando o marcador concorrente é criado. Isso abrange bootstrap/edição de perfil e projeção pública, hábitos e agenda, execuções, cursores, gamificação, notas, ativação de foto, subscriptions Push, projeções e entregas de lembretes. Um upload cujo objeto já tenha sido gravado, mas cuja ativação seja barrada, tenta remover o novo objeto como compensação.

Cada chamada de continuação adquire transacionalmente no próprio marcador um lease exclusivo de um minuto (`leaseId` e `leaseUntil`). Uma segunda chamada durante o lease não executa nem avança o estágio; apenas informa o estágio observado. O lease expirável permite retomada depois de queda do processo. A liberação usa o identificador do titular e não remove lease alheio. Com isso, `DeleteBatch` e `SetStage` são executados por um único continuador, evitando regressão ou avanço concorrente do estado; a idempotência dos lotes continua protegendo respostas perdidas.

O processo avança em lotes idempotentes de no máximo 200 documentos, nesta ordem:

1. notas;
2. chaves de unicidade de execução;
3. execuções;
4. cursores de materialização;
5. streaks/sequências;
6. bônus históricos;
7. desbloqueios de conquistas;
8. todas as subcoleções `scheduleVersions` do usuário;
9. hábitos ativos, arquivados e logicamente excluídos;
10. `avatarMedia` e `avatarCleanup`;
11. `pushSubscriptions`, `reminderSchedules` e `reminderDeliveries`;
12. perfil `users/{uid}`, incluindo preferências;
13. todos os objetos sob `avatars/{uid}/` no Storage;
14. varredura final estável de todas as fontes anteriores e de `publicRanking/{uid}`;
15. revogação dos tokens, exclusão do usuário no Firebase Authentication, remoção do marcador e limpeza dos cookies de sessão e CSRF.

A varredura volta à primeira etapa se encontrar qualquer dado funcional remanescente e volta ao Storage se encontrar objetos. A existência do próprio marcador não impede a conclusão. Depois da revogação não resta trabalho funcional dependente de outra chamada autenticada. `DeleteUser` já realizado é tratado como sucesso; se a resposta tiver sido perdida, a repetição recebe “usuário ausente” e conclui. A falha ao remover o marcador depois da exclusão do Auth não transforma a conta em existente novamente.

O marcador não recebe expiração enquanto a exclusão funcional está em andamento. Somente depois de `DeleteUser` confirmar que a identidade deixou de existir, o backend tenta armar `expiresAt` para sete dias no futuro e, independentemente do resultado desse armamento, tenta remover o marcador imediatamente. A política TTL é, portanto, apenas fallback para o caso raro em que a remoção normal falhou; uma falha ao configurar o fallback nunca impede a remoção possível. O fluxo normal não depende do TTL. A configuração está versionada em `firestore.indexes.json` e deve ser validada em produção com `gcloud firestore fields ttls list --collection-group=accountDeletions`; o Emulator não executa nem comprova o TTL.

## Inventário obrigatório

Persistências atuais pertencentes ao usuário: `users`, `habits` e suas `scheduleVersions`, `executions`, `executionUniqueness`, `habitOccurrenceCursors`, `notes`, `habitStreaks`, `habitBonusAwards`, `userAchievements`, `publicRanking`, `avatarMedia`, `avatarCleanup`, `pushSubscriptions`, `reminderSchedules`, `reminderDeliveries`, objetos `avatars/{uid}/...` e a conta Firebase Authentication. `accountDeletions` é estado técnico temporário do próprio fluxo.

Qualquer fase futura que crie persistência por usuário deve obrigatoriamente atualizar este inventário, o processo de exclusão, a varredura final e seus testes antes de ser considerada concluída.

## Segurança e observabilidade

Consultas e caminhos são derivados exclusivamente do UID autenticado. O processo nunca aceita `userId` do cliente. Logs registram somente estágio, resultado/código seguro e identificador técnico adequado; não incluem e-mail, conteúdo, token, corpo de erro externo nem caminho de Storage.

## Consequências

- Duas continuações concorrentes e reenvios após respostas perdidas são seguros.
- Lotes limitam o uso das operações do Firestore e suportam volumes superiores a 200 documentos.
- A exclusão entre serviços é recuperável, embora não seja uma única transação distribuída.
- O índice de grupo de coleção para `scheduleVersions.ownerUid` e a política TTL fazem parte da configuração de produção.
