# ERS — Especificação de Requisitos de Software
## Projeto HÁBITOS — MVP
**Versão:** 1.1  
**Idioma:** Português do Brasil  
**Produto:** PWA responsiva  
**Público-alvo:** adolescentes

## 1. Visão do produto
O HÁBITOS ajuda adolescentes a criar, organizar e manter hábitos positivos. Seu diferencial é transformar os 4 Rs da formação de hábitos em funcionalidades do aplicativo:
1. **Relembrar** — estímulo que sinaliza o momento de iniciar o hábito.
2. **Rotina** — ação física, mental ou emocional realizada em resposta ao estímulo.
3. **Recompensa** — resultado positivo que reforça a continuidade.
4. **Repetição** — repetição do ciclo Relembrar + Rotina + Recompensa, contribuindo para tornar o comportamento mais automático.

## 2. Escopo do MVP
O usuário deve poder criar conta, autenticar-se, compreender os 4 Rs, criar/editar hábitos, configurar metas e lembretes, receber sugestões com IA, registrar execuções, acompanhar progresso, acumular pontos, obter bônus de sequência e conquistas, participar de ranking geral, editar perfil e excluir conta/dados.

### Fora do escopo
Administrador, ligas, divisões, ranking semanal/mensal, perda de pontos, marketplace, app nativo, chat, feed social, grupos, comentários e login social.

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
Exibir taxa de conclusão, hábitos concluídos, sequência, pontos, conquistas, evolução, concluídos/parciais/não realizados e progresso por hábito.

### 6.6 Recompensas e Ranking
Pontuação total, posição geral, distância para próxima posição, regras de pontuação, conquistas, bônus de sequência, Top 10 e posição do usuário sempre visível.
Privacidade pública: somente apelido, avatar e pontuação.

### 6.7 Aprenda os 4 Rs
Tela educativa fiel ao conceito do projeto original:
- Relembrar: estímulo/sinal.
- Rotina: ação realizada em resposta.
- Recompensa: resultado positivo que reforça o ciclo.
- Repetição: repetição contínua do ciclo.
Também orientar análise da rotina, escolha de mudanças realistas, redução de barreiras e foco em consistência.

### 6.8 Perfil e Configurações
Avatar pronto ou foto, apelido livremente editável, idade, peso/altura/gênero opcionais, pontuação e ranking.
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

## 8. Pontuação
Execução completa = 10 pontos.
Parcial quantitativa:
`pontos = 10 × (realizado / meta)`
Máximo 10; arredondar para inteiro mais próximo; não aceitar valores negativos; meta zero é inválida.
Exemplo: 12 de 20 páginas = 6 pontos.

## 9. Sequência
Usar **execuções programadas consecutivas**, não dias corridos. Dias sem execução programada não quebram sequência.

## 10. Bônus de sequência
- 3 execuções: +10
- 7 execuções: +25
- 15 execuções: +50
- 30 execuções: +100
Conceder uma vez por marco.

## 11. Ranking
Somente geral. Sem ligas/divisões. Pontos acumulativos, sem perda.
Ordenação decrescente por pontos. Top 10 + posição do usuário.
Desempate recomendado: quem atingiu a pontuação primeiro.

## 12. Conquistas
Conquistas associadas a marcos de sequência, contendo id, nome, descrição, marco, data de desbloqueio e bônus quando aplicável.

## 13. Lembretes
Notificação, E-mail ou Ambos. “Notificação” é o termo visível.
Web Push/PWA pode ser usado internamente.

## 14. Autenticação
Cadastro: apelido, e-mail, senha, idade.
Login: e-mail/senha.
Recuperação e alteração de senha via Firebase Authentication.

## 15. IA — Sugerir Hábito
Entrada principal: título e descrição. Dados opcionais podem personalizar quando fornecidos.
Saída: sugestão de título/descrição/meta/unidade/frequência e, quando pertinente, horário.
Regras: nunca salvar automaticamente, permitir edição/ignorar, evitar recomendações perigosas/extremas e não substituir profissionais de saúde.
Segredo da API nunca vai ao cliente.

## 16. Modelo de dados inicial
### users
id, email, nickname, avatarType, avatarUrl, age, weight?, height?, gender?, totalPoints, createdAt, updatedAt.

### habits
id, userId, title, description, status, goalType, targetValue?, targetUnit?, weeklyTargetExecutions, scheduledDays, scheduledTime, reminderNotification, reminderEmail, createdAt, updatedAt, archivedAt?.

### executions
id, userId, habitId, scheduledDate, performedAt?, achievedValue?, targetValueSnapshot?, unitSnapshot?, status, pointsAwarded, createdAt.

### streaks
userId, habitId, currentStreak, bestStreak, lastScheduledExecutionDate, milestonesAwarded.

### achievements / userAchievements / notes
Estruturas para catálogo de conquistas, desbloqueios e notas/reflexões.

## 17. Segurança e privacidade
HTTPS em produção.
Autorização por usuário para dados privados.
Ranking nunca retorna e-mail, idade, peso, altura, gênero ou hábitos.
Segredos só no servidor.
Exclusão de conta deve excluir/anonimizar dados pessoais, hábitos, execuções, notas, conquistas e foto.

## 18. Requisitos funcionais resumidos
RF-001 Cadastro; RF-002 Login; RF-003 Recuperar senha; RF-004 Alterar senha; RF-005 Criar hábito; RF-006 Editar; RF-007 Arquivar; RF-008 Excluir; RF-009 Meta simples; RF-010 Meta quantitativa; RF-011 Dias; RF-012 Horário; RF-013 Notificação; RF-014 E-mail; RF-015 Ambos; RF-016 IA; RF-017 Registro simples; RF-018 Registro quantitativo; RF-019 Parcial; RF-020 Pontuação proporcional; RF-021 Sequência; RF-022 Bônus; RF-023 Ranking; RF-024 Privacidade ranking; RF-025 Progresso; RF-026 Histórico; RF-027 Notas; RF-028 Perfil; RF-029 Avatar/foto; RF-030 Exclusão conta; RF-031 Aprenda os 4 Rs; RF-032 PWA.

## 19. Requisitos não funcionais
Responsividade; HTTPS; Firebase Auth; privacidade; backend Go modular; logs sem segredos; foco em free tiers; acessibilidade; PT-BR; navegadores modernos; segurança de IA.

## 20. Testes obrigatórios
Pontuação: 20/20=10, 12/20=6, 10/20=5, 25/20=10, 0=0, meta zero inválida.
Sequência: hábito Seg/Qua/Sex cumprido nesses dias gera sequência 3, sem terça/quinta interferirem.
Bônus: concessão única em 3/7/15/30.
Privacidade: endpoint de ranking não retorna campos privados.
Autorização: usuário A não acessa hábito de B.

## 21. Ordem recomendada
1. Fundação: Go, Cloud Run, Firebase, Firestore, PWA.
2. Hábitos.
3. Execuções/metas.
4. Pontuação/Sequências/Conquistas.
5. Ranking.
6. IA.
7. Progresso.
8. Perfil.
9. Conteúdo 4 Rs e refinamento.

## 22. Referências visuais
- `01-inicio.png`
- `02-criar-habito.png`
- `03-meus-habitos.png`
- `04-detalhes-habito.png`
- `05-progresso.png`
- `06-recompensas-ranking.png`
- `07-aprenda-4rs.png`
- `08-perfil-configuracoes.png`

A ERS prevalece sobre divergências funcionais nos protótipos.
