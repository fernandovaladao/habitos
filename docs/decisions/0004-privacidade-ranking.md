# ADR 0004 — Privacidade e participação no ranking

- **Status:** Aceita
- **Data:** 2026-08-22

## Contexto

O público-alvo inclui adolescentes, e o perfil contém dados pessoais e potencialmente sensíveis. O ranking deve oferecer competição opcional sem transformar o restante do perfil em informação pública.

## Decisão

A participação no ranking geral será opt-in. Usuários sem consentimento ativo não aparecem publicamente.

O ranking público expõe somente apelido, avatar e pontuação, além da posição derivada. E-mail, idade, peso, altura, gênero, timezone, hábitos, execuções e notas nunca serão retornados.

Apelidos não precisam ser únicos. Devem ter de 3 a 24 caracteres e aceitar letras, números, espaços, `_` e `-`.

A ordenação será: pontos em ordem decrescente; timestamp em que o total atual foi atingido em ordem crescente; UID em ordem crescente como desempate técnico. A tela exibe Top 10 e a posição do usuário participante mesmo fora desse grupo.

## Consequências

- O backend deve produzir uma projeção pública explícita, em vez de serializar documentos completos de usuário.
- O timestamp do total precisa ser atualizado sempre que uma correção alterar o total corrente.
- Ativar ou desativar a participação deve refletir-se no ranking sem divulgar dados além dos permitidos.
- Testes devem verificar tanto os campos presentes quanto a ausência de todos os campos privados.

