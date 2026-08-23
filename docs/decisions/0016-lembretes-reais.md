# ADR 0016 — Lembretes reais, projeção operacional e entregas idempotentes

- **Status:** Aceita
- **Data:** 2026-08-23

## Contexto

A agenda versionada, as datas civis, as preferências globais e os canais por hábito já existem, mas ainda não produzem entregas. Lembretes externos não participam de uma transação distribuída com o Firestore e Web Push pode possuir vários destinos físicos por usuário.

## Decisão

Uma entrega lógica é identificada exclusivamente por `ownerUid + habitId + scheduledDate + channel`, usando SHA-256. `scheduleVersionId`, timezone e horário são snapshots reconciliáveis e nunca permitem uma segunda entrega lógica. Para Notificação, a entrega lógica contém estados físicos separados por subscription; sucesso ou invalidez permanente de um dispositivo impede reenvio deliberado a ele, sem bloquear retries dos demais.

`reminderSchedules/{habitId}` é uma projeção operacional descartável. Ela é reconstruída idempotentemente após mutações de hábito e timezone e pode ser reparada oportunisticamente. Imediatamente antes de enviar, o backend relê perfil, preferências, hábito, agenda, execução e marcador de exclusão. `completed`, `partial` e `not_done` cancelam a entrega; `pending` e ausência de execução válida permitem enviar.

A projeção nunca autoriza sozinha uma entrega. Documento ausente é reconstruído; documento incorreto ou atrasado é recusado pela revalidação. Quando a ocorrência já foi materializada, seus snapshots de agenda, canal, horário, versão e timezone prevalecem e não são reinterpretados por alteração posterior. Sem ocorrência materializada, vale a versão autoritativa selecionada por data civil.

Mudança de timezone reconcilia entregas futuras ainda não enviadas. Se mudar a data civil, a entrega anterior é supersedida. Quando a entrega da ocorrência equivalente já foi enviada, a nova identidade é criada como `skipped`, ligada por `equivalentTo`, e nunca é enviada. Equivalência significa a mesma próxima ocorrência projetada do hábito durante a operação explícita de mudança de timezone, não duas ocorrências ordinárias consecutivas.

Horário civil inexistente por DST usa o primeiro instante válido posterior. Horário ambíguo usa a primeira ocorrência cronológica. O Scheduler executa a cada minuto; retries ocorrem em T+0, T+5 e T+15 e expiram em T+30.

Cloud Scheduler chama por OIDC uma implantação privada do mesmo binário no Cloud Run, com service account dedicada e `roles/run.invoker`. O Cloud Run valida IAM; a aplicação não duplica validação JWT. A rota interna não aceita sessão de usuário nem possui bypass por header, query ou segredo compartilhado. `REMINDER_PROCESSOR_ENABLED` controla somente o registro da rota e não concede autorização: em produção a opção pertence exclusivamente à implantação privada protegida por IAM. Desenvolvimento exige simultaneamente `APP_ENV=development`, projeto `demo-habitos-local` e os três hosts de Emulator aprovados.

E-mail usa Resend atrás de `EmailSender`, com `Idempotency-Key` igual à entrega lógica. O snapshot mínimo de destinatário, título e horário é persistido para que retries tenham payload idêntico. `EMAIL_FROM` é configuração e deve estar verificado antes do smoke test. E-mail menciona somente título, horário, CTA e preferências.

Web Push aceita no máximo dez subscriptions ativas. Payload visível é neutro e não contém título/descrição do hábito, resultado, pontos, sequência, notas ou dados pessoais. Subscriptions expiradas ou permanentemente inválidas são desativadas sem afetar outras e endpoints/chaves nunca entram em logs. Uma resposta perdida depois de o serviço Push aceitar o envio deixa aquele destino físico como não confirmado; ele pode receber uma duplicidade externa rara na retry. Destinos já confirmados nunca são reenviados deliberadamente.

Não existe garantia genérica de exactly-once entre Firestore e provedores externos. Resend recebe a mesma `Idempotency-Key` e o mesmo snapshot/payload em retries, inclusive após resposta perdida. Web Push não oferece idempotência equivalente, portanto leases impedem concorrência interna e os estados físicos limitam retries ao destino não confirmado, mas não eliminam a rara duplicidade externa.

## Consequências

- Entrega exatamente uma vez não é prometida entre Firestore e Web Push, mas leases e estado físico evitam duplicações deliberadas.
- A projeção pode ser apagada e reconstruída sem perda do histórico das entregas.
- `pushSubscriptions`, `reminderSchedules` e `reminderDeliveries` passam a integrar obrigatoriamente a exclusão integral e sua varredura final.
