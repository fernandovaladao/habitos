# ADR 0008 — Materialização e fechamento de execuções

- Status: Aceito
- Data: 2026-08-22

## Contexto

Execuções precisam representar exclusivamente ocorrências programadas, preservar a configuração vigente na data civil e ser seguras contra reenvio e concorrência. O MVP não terá worker ou serviço agendador para criar e fechar ocorrências.

## Decisão

As ocorrências serão materializadas incrementalmente sob demanda ao consultar hábitos e histórico e antes de mutações que possam alterar sua interpretação: edição de meta ou agenda, arquivamento, exclusão lógica e mudança de timezone. Um cursor por hábito registra a última data civil examinada. O início nunca antecede a primeira `effectiveDate` do hábito e, após reativação, nunca antecede `occurrencesResumeDate`.

As `scheduleVersions` são carregadas uma única vez por sincronização. Para cada data candidata, seleciona-se a versão com maior `effectiveDate <= scheduledDate`. Somente dias presentes nessa versão geram execução. Meta, unidade, agenda, timezone e prazo são copiados para snapshots imutáveis da execução.

A execução possui ID público aleatório. A unicidade por proprietário, hábito e data programada é protegida em transação por um documento interno cuja chave é SHA-256 desses valores. Materializações repetidas ou concorrentes retornam a mesma execução. Registro e correção gravam o estado absoluto e são idempotentes.

O prazo é representado pelo instante exclusivo correspondente ao início do segundo dia seguinte no `timezoneSnapshot`. Antes de mudar o timezone, o sistema sincroniza todas as ocorrências devidas no timezone anterior. Ocorrências existentes nunca têm data, timezone ou prazo recalculados; o novo timezone vale apenas para datas ainda não materializadas.

O fechamento será sob demanda. Durante a sincronização, ocorrências vencidas sem registro tornam-se `not_done`; ocorrências já registradas mantêm o status, e todas recebem `closedAt`. Uma ocorrência antiga ainda não materializada já é criada fechada como `not_done`. Repetir o fechamento é seguro. Não haverá worker, microsserviço, Cloud Scheduler ou endpoint de manutenção nesta fase.

Antes de alterar uma meta, o sistema sincroniza até a data local corrente. A nova meta é incluída em uma versão com vigência no dia seguinte, preservando o snapshot antigo da ocorrência de hoje.

## Consequências

- O estado persistido pode permanecer pendente enquanto não houver acesso, mas é normalizado antes de ser observado ou alterado.
- Arquivar, excluir ou trocar timezone depende do sucesso da sincronização anterior; em caso de falha, a mutação é interrompida.
- Histórico é paginado em 30 execuções, por `scheduledDate` decrescente.
- Pontuação, sequência e conquistas não são calculadas nesta fase.
