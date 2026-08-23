# ADR 0010 — Métricas de progresso e períodos

- Status: Aceito
- Data: 2026-08-22

## Contexto

A tela Progresso agrega execuções, pontos, bônus, sequências e conquistas produzidos pelas fases anteriores. O relatório precisa respeitar datas civis do usuário, correções retroativas e a privacidade de hábitos excluídos sem criar uma projeção persistida suscetível a divergências.

## Decisão

A taxa de conclusão é calculada sobre execuções resolvidas do período. Cada `completed` contribui 1, cada `not_done` contribui 0 e cada `partial` quantitativa contribui `min(1, achieved/target)`. Execuções `pending` ficam completamente fora da contribuição e do denominador. O núcleo mantém contribuição e denominador exatos, sem ponto flutuante; somente a apresentação converte a taxa em percentual inteiro, arredondando metade para cima.

Os contadores exibem execuções concluídas, parciais e não realizadas. `Pontos no período` soma os `pointsAwarded` correntes das execuções cuja `scheduledDate` pertence ao intervalo e os bônus históricos cuja `triggerScheduledDate` pertence ao mesmo intervalo. Conquistas mostram todos os desbloqueios do usuário, independentemente do filtro. A sequência geral é a maior `currentStreak` entre hábitos ativos, excluindo arquivados e logicamente excluídos.

Os períodos são inclusivos e resolvidos no timezone IANA atual do perfil:

- Semana: segunda-feira a domingo; datas futuras podem compor o eixo, mas não recebem valor nem zero.
- Mês: mês-calendário atual.
- Período personalizado: datas inicial e final informadas, no máximo 366 dias.

Somente datas até o dia civil atual são consultadas e agregadas. A evolução usa a mesma taxa proporcional: até 31 dias, um ponto por data civil; acima de 31 dias, um ponto por semana civil de segunda a domingo. A primeira e a última semana podem ser parciais e incluem somente as datas dentro do intervalo.

O detalhamento por hábito inclui apenas hábitos com ao menos uma execução resolvida no período e é ordenado por taxa decrescente, título crescente e ID. Hábitos arquivados preservam seus dados históricos. Execuções já existentes de hábitos logicamente excluídos continuam nos agregados, porém o detalhamento mostra apenas `Hábito excluído`, sem título original, link ou ação. Hábitos excluídos nunca são sincronizados ou materializados novamente; a sincronização anterior ao relatório alcança somente hábitos não excluídos retornados pela listagem normal.

O relatório é calculado sob demanda diretamente dos fatos e projeções transacionais existentes. Nenhuma projeção agregada de Progresso é persistida. Correções retroativas aparecem automaticamente porque substituem `pointsAwarded`, status e contribuição correntes; bônus históricos permanecem conforme o ADR 0009.

Todas as consultas usam exclusivamente o UID da sessão validada. O backend consulta execuções e bônus por proprietário e intervalo de data civil, sequências e conquistas por proprietário, e carrega em lote somente os hábitos referenciados para título, estado e máscara de exclusão.

## Consequências

- A tela não exibe dados simulados: ausência de execução resolvida resulta em ausência de taxa, não em zero artificial.
- O custo de leitura cresce com os fatos do período, limitado a 366 dias no filtro personalizado.
- Alterações funcionais em execuções são refletidas sem processo de reconstrução de uma projeção de relatório.
- Índices Firestore só serão versionados quando as consultas reais forem validadas contra o Emulator e exigirem configuração adicional.
