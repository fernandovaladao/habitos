# ERS — Especificação de Requisitos de Software
## Projeto HÁBITOS — MVP
**Versão:** 1.2
**Idioma:** Português do Brasil  
**Produto:** PWA responsiva  
**Público-alvo:** adolescentes

## 1. Visão do produto
O HÁBITOS ajuda adolescentes a criar, organizar e manter hábitos positivos. Seu diferencial é transformar os 4 Rs da formação de hábitos em funcionalidades do aplicativo:
1. **Relembrar** — estímulo que sinaliza o momento de iniciar o hábito.
2. **Rotina** — ação física, mental ou emocional realizada em resposta ao estímulo.
3. **Recompensa** — resultado positivo que reforça a continuidade.
4. **Repetição** — continuidade do ciclo Relembrar + Rotina + Recompensa, contribuindo para tornar o comportamento mais automático.

## 2. Escopo do MVP
O usuário deve poder criar conta, autenticar-se, compreender os 4 Rs, criar/editar hábitos, configurar metas e lembretes, receber sugestões com IA, registrar execuções, acompanhar progresso, acumular pontos, obter bônus de sequência e conquistas, optar por participar do ranking geral, editar perfil e excluir conta/dados.

### Fora do escopo
Administrador, ligas, divisões, ranking semanal/mensal, punição ou perda de pontos por falha, marketplace, app nativo, chat, feed social, grupos, comentários, login social e criação/edição/registro offline.

## 3. Arquitetura aprovada
- Backend: Go.
- Frontend: HTML/CSS/JavaScript leve; HTMX permitido.
- PWA.
- Cloud Run.
- Firestore.
- Firebase Authentication.
- Cloud Storage/Firebase Storage.
- IA acessada somente pelo backend.
- Git/GitHub.
- Otimizar para cotas gratuitas no MVP, sem garantia de custo zero.
- Arquitetura de monólito modular em Go, com separação entre interface HTTP, casos de uso/regras de negócio e adaptadores de infraestrutura.

## 4. Identidade visual
Azul, laranja, branco e cinzas neutros. Interface limpa, cards arredondados, boa legibilidade e linguagem adequada a adolescentes. O logotipo Chromos deve ser usado quando o arquivo oficial estiver disponível. Os protótipos reconstruídos em `docs/prototypes/` funcionam como referência visual.

## 5. Navegação
Início | Criar Hábito | Meus Hábitos | Progresso | Recompensas | Aprenda os 4 Rs | Perfil

## 6. Telas

### 6.1 Início
Explicar resumidamente o HÁBITOS, os 4 Rs e como começar. Ter CTA “Criar meu primeiro hábito”. Usar exemplo genérico como “Ler 20 páginas de um livro”.

### 6.2 Criar Hábito
Obrigatórios: título, descrição, idade, dias, horário, tipo de meta, forma de lembrete.
Opcionais: peso, altura e gênero.
Idade, peso, altura e gênero são dados do perfil, não do hábito. Na criação/edição do hábito devem aparecer pré-preenchidos; alterações realizadas nesse contexto atualizam o perfil.
Permitir novo hábito ou carregar hábito existente para edição.
Botão `✨ Sugerir hábito com IA`; IA usa título e descrição como entrada principal e nunca salva automaticamente. Usuário pode aceitar, editar ou ignorar.
Lembrete: Notificação, E-mail ou Ambos.
Após salvar: persistir e redirecionar para Meus Hábitos.

### 6.3 Meus Hábitos
Lista, filtros Todos/Hoje/Concluídos ou equivalentes, progresso resumido, status, dias/horário, acesso a detalhes, criação de novo hábito e marcação de execução.

### 6.4 Detalhes do Hábito
Exibir título, descrição, status, calendário/horários, progresso, sequência, relação com os 4 Rs e notas/reflexões.
Ações: registrar resultado, nota/reflexão, editar.
Menu secundário: editar, calendário, estatísticas, lembretes, ajustar meta, duplicar, arquivar e excluir (com confirmação).

### 6.5 Progresso
Exibir taxa de conclusão, execuções concluídas, sequência, pontos, conquistas, evolução, concluídos/parciais/não realizados e progresso por hábito. A sequência geral exibida é a maior sequência atual entre os hábitos ativos. A semana vai de segunda-feira a domingo no timezone do usuário.

### 6.6 Recompensas e Ranking
Para usuários que optaram por participar: pontuação total, posição geral, distância para próxima posição, regras de pontuação, conquistas, bônus de sequência, Top 10 e posição do usuário sempre visível.
Privacidade pública: somente apelido, avatar e pontuação.

### 6.7 Aprenda os 4 Rs
Tela educativa fiel ao conceito do projeto original:
- Relembrar: estímulo/sinal.
- Rotina: ação realizada em resposta.
- Recompensa: resultado positivo que reforça o ciclo.
- Repetição: repetição contínua do ciclo.
Também orientar análise da rotina, escolha de mudanças realistas, redução de barreiras e foco em consistência.

### 6.8 Perfil e Configurações
Avatar pronto ou foto, apelido editável, idade, peso/altura/gênero opcionais, timezone IANA, pontuação e opção de participação no ranking.
O apelido deve ter entre 3 e 24 caracteres e aceitar somente letras, números, espaços, `_` e `-`. Apelidos não precisam ser únicos.
O timezone deve ser detectado inicialmente pelo navegador e pode ser alterado pelo usuário.
Conta: e-mail e alterar senha.
Preferências: Notificação e E-mail.
Excluir conta e todos os dados, com confirmação explícita.

## 7. Metas
### Meta simples
Concluído = 10 pontos; não realizado = 0.

### Meta quantitativa
Quantidade + unidade + frequência. Exemplos: páginas, minutos, km.
Estados: concluído, parcial, não realizado.
Ao registrar, usuário informa o resultado real.
Meta e resultado quantitativos aceitam valores positivos com até 2 casas decimais. Resultado igual a zero é permitido para representar não realizado; valores negativos são inválidos.
A unidade deve ser escolhida de um catálogo inicial ou pela opção `Outra`, com texto personalizado.
Quando `realizado >= meta`, a execução é concluída.
A meta semanal é independente dos dias selecionados, mas não pode ser maior que o número de dias programados na semana.
Cada hábito pode ter no máximo uma ocorrência programada por dia.

## 8. Pontuação
Execução completa = 10 pontos.
Parcial quantitativa:
`pontos = 10 × (realizado / meta)`
Máximo 10; arredondar para inteiro mais próximo; não aceitar valores negativos; meta zero é inválida.
Quando o valor calculado terminar em `x,5`, arredondar para cima.
Exemplo: 12 de 20 páginas = 6 pontos.
Não há punição nem perda de pontos por falha. Dentro da janela permitida, a correção de uma execução recalcula seus pontos e pode ajustar o total acumulado para cima ou para baixo, a fim de preservar a consistência.

## 9. Sequência
Usar **execuções programadas consecutivas**, não dias corridos. Dias sem execução programada não quebram sequência.
Apenas uma execução concluída mantém e incrementa a sequência. Execução parcial ou não realizada quebra a sequência, embora a parcial conceda pontos proporcionais.
Sequências são mantidas por hábito. No Progresso, a sequência geral é a maior sequência atual entre os hábitos ativos.
Uma sequência pode continuar acima de 30 e deve ser contada e exibida, sem novos bônus no MVP.

## 10. Bônus de sequência
- 3 execuções: +10
- 7 execuções: +25
- 15 execuções: +50
- 30 execuções: +100
Cada marco é concedido uma única vez durante toda a vida de cada hábito, mesmo que a sequência seja quebrada e reconstruída.

## 11. Ranking
Somente geral. Sem ligas/divisões. A participação é opt-in e o usuário pode escolher se deseja aparecer publicamente.
Ordenação por pontos em ordem decrescente; depois pelo timestamp em que o total atual foi atingido, em ordem crescente; por fim pelo UID, em ordem crescente, como desempate técnico determinístico.
Exibir Top 10 e a posição do usuário participante, mesmo quando estiver fora do Top 10.
O ranking público retorna somente apelido, avatar e pontuação. A posição pode ser exibida, mas nenhum outro dado do perfil ou dos hábitos pode ser exposto.

## 12. Conquistas
Conquistas são por usuário e desbloqueadas uma única vez. Quando qualquer hábito alcança um marco aplicável, a conquista correspondente é desbloqueada para o usuário. Devem conter id, nome, descrição, marco, data de desbloqueio e bônus quando aplicável.

## 13. Política temporal de execuções
Cada ocorrência usa a data local do usuário conforme o timezone IANA armazenado no perfil.
Uma execução programada pode ser registrada ou corrigida até o fim do dia seguinte à sua data programada, no timezone do usuário.
Depois do encerramento dessa janela, uma ocorrência programada sem registro passa a `Não realizado` e não pode mais ser registrada ou corrigida no MVP.
Alterações de agenda entram em vigor a partir do dia seguinte à alteração. Histórico, ocorrências anteriores e seus snapshots nunca são modificados por uma mudança de agenda.
Arquivar um hábito interrompe novas ocorrências e lembretes e preserva todo o histórico. Ao reativá-lo, sua sequência atual reinicia em zero; melhor sequência e marcos já concedidos permanecem preservados.

## 14. Lembretes
Notificação, E-mail ou Ambos. “Notificação” é o termo visível.
Web Push/PWA pode ser usado internamente.

## 15. Autenticação
Cadastro: apelido, e-mail, senha, idade.
Login: e-mail/senha.
Recuperação e alteração de senha via Firebase Authentication.
Se a identidade já existir no Firebase Authentication, mas o perfil não existir no Firestore, o backend deve tentar criar o perfil novamente de forma idempotente.

## 16. IA — Sugerir Hábito
Entrada principal: título e descrição. Dados opcionais podem personalizar quando fornecidos.
Saída: sugestão de título/descrição/meta/unidade/frequência e, quando pertinente, horário.
Regras: nunca salvar automaticamente, permitir edição/ignorar, evitar recomendações perigosas/extremas e não substituir profissionais de saúde.
Segredo da API nunca vai ao cliente.

## 17. Modelo de dados inicial
### users
id, email, nickname, avatarType, avatarUrl, age, weight?, height?, gender?, timezone, rankingOptIn, totalPoints, totalPointsReachedAt, createdAt, updatedAt.

### habits
id, userId, title, description, status, goalType, targetValue?, targetUnit?, customTargetUnit?, weeklyTargetExecutions, scheduledDays, scheduledTime, reminderNotification, reminderEmail, scheduleEffectiveAt, createdAt, updatedAt, archivedAt?.

### executions
id, userId, habitId, scheduledDate, timezoneSnapshot, registrationDeadline, performedAt?, achievedValue?, targetValueSnapshot?, unitSnapshot?, status, pointsAwarded, createdAt, updatedAt.

### streaks
userId, habitId, currentStreak, bestStreak, lastScheduledExecutionDate, milestonesAwarded.

### achievements / userAchievements
Catálogo de conquistas e desbloqueios únicos por usuário.

### notes
id, userId, habitId, executionId?, content, createdAt, updatedAt. Notas são privadas, têm no máximo 1.000 caracteres, pertencem a um hábito, podem estar associadas a uma execução e podem ser editadas ou excluídas pelo usuário.

## 18. Segurança e privacidade
HTTPS em produção.
Autorização por usuário para dados privados.
Ranking é opt-in e nunca retorna e-mail, idade, peso, altura, gênero, timezone, hábitos, execuções ou notas.
Segredos só no servidor.
No MVP, a exclusão de conta deve excluir, e não anonimizar, todos os dados pertencentes ao usuário, incluindo no mínimo perfil, hábitos, execuções, streaks/sequências, notas, desbloqueios de conquistas, preferências, projeção de ranking, registros de lembretes associados e fotos/objetos de Storage. O usuário deve ser removido do ranking. A operação exige confirmação explícita e deve ser implementada de forma idempotente e recuperável em caso de falha parcial entre serviços.

## 19. Requisitos funcionais resumidos
RF-001 Cadastro; RF-002 Login; RF-003 Recuperar senha; RF-004 Alterar senha; RF-005 Criar hábito; RF-006 Editar; RF-007 Arquivar; RF-008 Excluir; RF-009 Meta simples; RF-010 Meta quantitativa; RF-011 Dias; RF-012 Horário; RF-013 Notificação; RF-014 E-mail; RF-015 Ambos; RF-016 IA; RF-017 Registro simples; RF-018 Registro quantitativo; RF-019 Parcial; RF-020 Pontuação proporcional; RF-021 Sequência; RF-022 Bônus; RF-023 Ranking; RF-024 Privacidade ranking; RF-025 Progresso; RF-026 Histórico; RF-027 Notas; RF-028 Perfil; RF-029 Avatar/foto; RF-030 Exclusão conta; RF-031 Aprenda os 4 Rs; RF-032 PWA.

## 20. Requisitos não funcionais
Responsividade; HTTPS; Firebase Auth; privacidade; monólito modular em Go; logs sem segredos; foco em free tiers; acessibilidade; PT-BR; navegadores modernos; segurança de IA.
A PWA deve manter em cache a interface e o conteúdo estático. Criação, edição e registro offline não fazem parte do MVP.

## 21. Cálculo do progresso
A taxa de progresso de cada ocorrência usa: concluído = 100%; parcial quantitativo = `min(100, 100 × realizado/meta)`; não realizado = 0%.
Indicadores agregados devem usar essa contribuição proporcional para as ocorrências do período.
O termo de interface é `Execuções concluídas`, não `Hábitos concluídos`.
A semana começa na segunda-feira e termina no domingo, sempre no timezone do usuário.

## 22. Testes obrigatórios
Pontuação: 20/20=10, 12/20=6, 10/20=5, 25/20=10, 0=0, meta zero inválida.
Arredondamento: resultado proporcional terminado em 6,5 gera 7 pontos.
Sequência: hábito Seg/Qua/Sex cumprido nesses dias gera sequência 3, sem terça/quinta interferirem; parcial e não realizado quebram a sequência.
Bônus: concessão única em 3/7/15/30 por hábito durante toda a vida do hábito, inclusive após quebra e reconstrução da sequência.
Temporalidade: registro e correção até o fim do dia seguinte no timezone do usuário; fechamento posterior como não realizado; mudança de agenda somente a partir do dia seguinte.
Ranking: ordenação por pontos, timestamp e UID; usuário sem opt-in não aparece.
Privacidade: endpoint de ranking não retorna campos privados, inclusive em respostas serializadas ou campos ocultos.
Autorização: usuário A não acessa hábito de B.
Exclusão: remoção idempotente dos dados do usuário e do ranking.

## 23. Ordem recomendada
1. Fundação: Go, Cloud Run, Firebase, Firestore, PWA.
2. Hábitos.
3. Execuções/metas.
4. Pontuação/Sequências/Conquistas.
5. Ranking.
6. IA.
7. Progresso.
8. Perfil.
9. Conteúdo 4 Rs e refinamento.

## 24. Referências visuais
- `01-inicio.png`
- `02-criar-habito.png`
- `03-meus-habitos.png`
- `04-detalhes-habito.png`
- `05-progresso.png`
- `06-recompensas-ranking.png`
- `07-aprenda-4rs.png`
- `08-perfil-configuracoes.png`

A ERS prevalece sobre divergências funcionais nos protótipos.
