# ADR 0003 — Cálculo de pontuação, sequência e bônus

- **Status:** Aceita
- **Data:** 2026-08-22

## Contexto

Pontuação, sequência e bônus são elementos centrais do produto e precisam ser determinísticos, auditáveis e resistentes a reenvio ou correção de registros.

## Decisão

Execução simples concluída vale 10 pontos e não realizada vale 0. Para meta quantitativa, os pontos são `10 × realizado/meta`, limitados a 10 e arredondados ao inteiro mais próximo; valores exatamente em `x,5` são arredondados para cima. Meta zero e valores negativos são inválidos. Meta e resultado aceitam até duas casas decimais. `realizado >= meta` significa concluído.

Parcial quantitativo recebe pontos proporcionais, mas não mantém sequência. Apenas execução concluída incrementa a sequência; parcial e não realizado a quebram. Dias sem ocorrência programada não interferem.

Os bônus são 3=+10, 7=+25, 15=+50 e 30=+100. Cada marco é concedido uma única vez por hábito durante toda a vida do hábito. Sequências acima de 30 continuam sendo contadas, sem novos bônus no MVP. Conquistas são desbloqueadas uma única vez por usuário quando qualquer hábito atinge o marco correspondente.

“Não retirar pontos” significa não punir falhas. Uma correção válida recalcula os pontos da execução e pode ajustar o total para cima ou para baixo. O total atual deve registrar também o timestamp em que foi atingido, para desempate do ranking.

## Consequências

- Registro e correção devem recalcular deltas, não simplesmente somar novos pontos.
- Execução, total, sequência, bônus e conquista exigem atualização transacional e idempotente.
- Snapshots preservam a meta e unidade vigentes na ocorrência.
- Marcos concedidos devem permanecer registrados após quebra, arquivamento ou reativação.

