# ADR 0012 — Sugestões de hábito com IA

- Status: Aceito
- Data: 2026-08-22

## Contexto

A ERS prevê sugestões assistidas na criação de hábitos, mas a saída da IA não pode contornar validações, salvar dados automaticamente nem expor informações pessoais ou segredos no frontend. O comportamento externo também precisa permanecer substituível e testável sem chamadas reais.

## Decisão

O backend usará a OpenAI Responses API com Structured Outputs por JSON Schema. O modelo inicial é `gpt-5.6-luna`, configurado por `OPENAI_MODEL` e não incorporado às regras de domínio. A autenticação usa `OPENAI_API_KEY` exclusivamente no backend e cada chamada tem timeout configurado por `AI_REQUEST_TIMEOUT`, inicialmente `10s`.

Somente título e descrição digitados serão enviados. UID, e-mail, idade, peso, altura, gênero, timezone e quaisquer outros dados de perfil ou domínio não serão incluídos. O pedido usa `store=false`, não envia metadata identificadora e não habilita ferramentas. Prompt, resposta e sugestão não serão persistidos em Firestore nem registrados em logs pela aplicação.

O adaptador OpenAI permanece atrás de uma interface interna. Uma requisição HTTP do usuário causa no máximo uma chamada à API, sem retry automático, com limite explícito de 64 KiB para o corpo da resposta. Falhas, recusas, timeout, resposta incompleta ou conteúdo malformado produzem mensagem genérica ao navegador, sem corpo do provedor, prompt, modelo, chave ou detalhes internos.

A saída estruturada contém título, descrição, meta, unidade, frequência e horário opcional. Ela passa por validação própria de `Suggestion`, reutilizando tipos e regras puras de hábitos quando aplicáveis, sem ser tratada diretamente como um `habit.Input`. A IA não escolhe canal de lembrete. Horário omitido preserva o valor já preenchido.

A funcionalidade aparece somente na criação. `Usar sugestão` apenas preenche os campos, que continuam editáveis; nunca salva o hábito. `Ignorar` preserva o formulário. O fluxo normal de criação continua sendo a validação final e a única operação persistente.

Como defesa adicional, título e descrição de entrada e de saída passam por uma verificação determinística estreita, voltada somente a instruções explicitamente perigosas, como autolesão, privação extrema, indução de vômito, alteração explícita de medicação ou esforço até desmaiar. A verificação é deliberadamente de melhor esforço: não é uma classificação médica geral, não usa uma lista ampla de termos de saúde e não oferece garantia de segurança semântica. Conteúdo recusado produz apenas erro genérico e não é registrado.

A interface informa permanentemente que sugestões de IA podem conter erros e não substituem orientação de profissionais de saúde. O prompt, a verificação estreita, a validação estrutural, a revisão humana e a validação final do hábito formam camadas complementares; nenhuma delas autoriza salvamento automático.

## Consequências

- Testes usam fakes e servidor HTTP local; nenhuma suíte automatizada chama a OpenAI API real.
- A aplicação depende de configuração OpenAI válida para inicializar a funcionalidade.
- Structured Outputs reduz respostas fora do formato, mas a validação determinística do backend continua obrigatória.
- A defesa semântica reduz casos perigosos explícitos, mas não elimina falsos negativos e não deve ser apresentada como garantia.
- Nenhuma coleção, índice ou transação Firestore é adicionada nesta fase.
- A interface deve comunicar que a sugestão precisa ser revisada antes de salvar e não substitui orientação profissional.
