# ADR 0002 — Política temporal de execuções e timezone

- **Status:** Aceita
- **Data:** 2026-08-22

## Contexto

Ocorrências, lembretes e sequências dependem da data civil percebida pelo usuário. Usar apenas UTC ou o timezone do servidor produziria resultados incorretos para registro atrasado, fechamento de ocorrências e semanas de progresso.

## Decisão

Cada perfil armazenará um timezone IANA, detectado inicialmente pelo navegador e editável pelo usuário. Datas programadas, prazo de registro e semanas serão interpretados nesse timezone.

Uma execução pode ser registrada ou corrigida até o fim do dia seguinte à data programada. Encerrada essa janela, uma ocorrência sem registro passa a `Não realizado` e fica fechada para registro ou correção no MVP.

Cada hábito terá no máximo uma ocorrência programada por dia. Alterações de agenda entram em vigor no dia seguinte e não modificam histórico nem snapshots. Arquivar interrompe novas ocorrências e lembretes; reativar reinicia a sequência atual em zero, preservando histórico, melhor sequência e marcos concedidos.

A semana de progresso vai de segunda-feira a domingo no timezone do usuário.

## Consequências

- Ocorrências devem guardar data programada, snapshot do timezone e prazo de registro suficientes para auditoria.
- Mudanças posteriores de timezone não podem reinterpretar retroativamente ocorrências já criadas.
- O fechamento de ocorrências precisa ser idempotente e tolerar atrasos do mecanismo agendador.
- Testes devem cobrir limites de dia, mudança de agenda, arquivamento e timezones distintos.

