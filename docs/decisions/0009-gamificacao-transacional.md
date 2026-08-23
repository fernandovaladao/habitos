# ADR 0009 — Gamificação transacional e correções retroativas

- Status: Aceito
- Data: 2026-08-22

## Contexto

Pontuação, sequência, bônus e conquistas derivam das execuções, mas uma execução pode ser corrigida até o prazo definido no ADR 0002. Somar eventos sem recalcular produziria totais e sequências incorretos. Reenvios e requisições concorrentes também não podem duplicar recompensas.

## Decisão

Cada execução guarda seus `pointsAwarded` correntes e a projeção auditável `streakBefore`/`streakAfter`. Quantidades continuam em centésimos inteiros. A pontuação proporcional é calculada sem ponto flutuante, arredondando a metade para cima e limitando o resultado a 10.

Existe um documento `habitStreaks/{habitId}` por hábito, com sequência atual, melhor sequência, última ocorrência confirmada e marcos concedidos. Somente `completed` incrementa a sequência. `partial` e `not_done` quebram; datas sem ocorrência não participam. Uma ocorrência `pending` não quebra, mas bloqueia a confirmação de continuidade através da lacuna até ser resolvida.

O `bestStreak` é histórico e nunca diminui por correção. Reativar um hábito zera atomicamente apenas a sequência atual, preservando melhor sequência e marcos.

Bônus usam documentos determinísticos em `habitBonusAwards`, um por proprietário, hábito e marco. São 3=+10, 7=+25, 15=+50 e 30=+100. Uma concessão é histórica e imutável: correções posteriores não a removem do total e reconstruir a sequência não a concede novamente.

Conquistas usam documentos determinísticos por usuário e código. Elas não concedem pontos adicionais. O catálogo é:

- 3 — `Primeira sequência`: “Concluiu 3 execuções programadas consecutivas.”
- 7 — `Ritmo firme`: “Concluiu 7 execuções programadas consecutivas.”
- 15 — `Constância`: “Concluiu 15 execuções programadas consecutivas.”
- 30 — `Compromisso`: “Concluiu 30 execuções programadas consecutivas.”

Conforme o ADR 0001, a mutação de resultado, seus pontos, as execuções posteriores afetadas, o streak, os bônus, as conquistas e o total do perfil são reconciliados na mesma transação Firestore. Uma correção recalcula cronologicamente a partir da lacuna afetada. Duas correções concorrentes são serializadas pelas tentativas da transação; o último commit válido define o resultado corrente.

O total corrente obedece a:

`totalPoints = soma dos pointsAwarded correntes + soma dos bônus historicamente concedidos`.

Se o total aumenta ou diminui, `totalPointsReachedAt` recebe o instante normalizado da transação. Se o total não muda, o timestamp é preservado. Um usuário que nunca teve alteração permanece com zero e timestamp ausente; se uma correção posteriormente levar o total a zero, o instante da correção é gravado.

Um resultado exatamente igual ao persistido é um no-op completo: não altera timestamps, pontos, sequência, bônus ou conquistas.

Como não existe base legada de produção, a Fase 5 oferece apenas reconciliação idempotente do histórico completo de um hábito, adequada aos testes e volumes esperados do MVP. Não serão criados worker, lock operacional ou infraestrutura genérica de migração em lotes.

## Consequências

- Pontos de execução podem subir ou descer; bônus históricos nunca são debitados.
- IDs determinísticos e transações impedem concessões duplicadas sob concorrência.
- Fechamento, materialização vencida, registro e correção precisam usar a mesma fronteira de reconciliação.
- A reconciliação completa fica sujeita aos limites de documentos de uma transação Firestore; eventual migração em lotes exige decisão futura.
- O perfil mantém apenas total e timestamp necessários ao ranking futuro; nenhuma projeção pública ou regra de ranking é implementada nesta fase.
