# ADR 0014 — Upload privado de foto/avatar

- Status: Aceito
- Data: 2026-08-22

## Contexto

A ERS permite foto no Perfil, mas o público-alvo inclui adolescentes e ainda não existe política de moderação e consentimento para publicar conteúdo enviado por usuários. O avatar interno da Fase 9A já atende à representação pública no Ranking.

## Decisão

A foto enviada nesta fase é privada e visível somente ao próprio usuário autenticado no Perfil. O Ranking continua usando exclusivamente `avatarType`, limitado a `default`, `blue`, `orange`, `green` e `purple`; caminho de Storage, identificador da foto e imagem enviada não integram `publicRanking`.

Serão aceitos JPEG, PNG e WebP estático com até 5 MiB, 20 megapixels e lado máximo de 8192 pixels. O backend identifica e decodifica o conteúdo, aplica orientação EXIF de JPEG, corte quadrado central, composição de transparência sobre fundo neutro e redimensionamento para 512×512. A saída é JPEG qualidade 85, sem metadados.

Objetos usam `avatars/{uid}/{mediaId}.jpg`, com UID exclusivamente da sessão e `mediaId` aleatório do servidor. O bucket é privado e `storage.rules` nega todo acesso direto de clientes. O backend serve `GET /media/avatars/{mediaId}` somente depois de confirmar em `avatarMedia` que o UID autenticado é proprietário. A resposta usa cache privado com revalidação; fotos não entram no service worker.

Upload, remoção de foto e seleção de avatar interno são operações separadas do `PUT /api/profile`. Alterar outros dados nunca remove a foto. Selecionar explicitamente avatar interno limpa a referência privada e inicia a remoção física.

O novo objeto é gravado antes da transação Firestore. A transação atualiza o perfil, reconcilia a projeção pública sem incluir a foto, troca o mapeamento privado e registra a limpeza anterior. Se a transação falhar, o novo objeto é removido por compensação. Depois do commit, falha ao excluir o objeto antigo não restaura sua autorização; `avatarCleanup` registra a limpeza oportunista e nunca é fonte de autorização.

## Consequências

- Não há publicação de UGC no Ranking nesta fase.
- Storage e Firestore não formam uma transação distribuída; compensação e limpeza oportunista tratam falhas parciais.
- A futura exclusão integral deve remover objetos pelo prefixo do UID, mapeamentos e registros de limpeza.
- Publicação futura de foto exige nova decisão sobre moderação e consentimento.
