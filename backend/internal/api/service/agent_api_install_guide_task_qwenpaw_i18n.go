package service

// qwenpawConnectorTaskJa / Ko / De / Fr / Es / Pt / Ru / Ar / Hi mirror
// qwenpawConnectorTaskZh / En (agent_api_install_guide_service.go) —
// round5 fix for qwenpaw falling back to English in
// ja/ko/de/fr/es/pt/ru/ar/hi. Same 2 %s slots in the same order:
// connectorInstallCommand, entry.

const qwenpawConnectorTaskJa = `この Grix Agent をこのマシンの grix-connector に接続してください。手順どおりに実行し、完了したら結果を報告してください。

前提条件：このマシンに Node.js 18 以上と uv（QwenPaw CLI に必要）がインストールされていること。どちらかがなければ先に知らせてください。自分でインストールしないでください。

注：今回の統合テストは macOS でのみ実機確認済みです。以下の uv tool install は Linux でも動くはずですが、実機での検証はまだ行っていません。失敗したら報告してください。

0) QwenPaw CLI をインストール（インストール済みならスキップ、必要なら最新版に更新）
uv tool install qwenpaw
モデル/プロバイダは QwenPaw 側で設定します（qwenpaw models config-key または対話的に選ぶ qwenpaw models set-llm）。これはログインではないので、案内に従うだけで構いません。どのプロバイダを選ぶか分からない場合は先に知らせてください。

1) コネクタをインストール（インストール済みなら最新版に更新）
%s

2) 以下のエントリを ~/.grix/config/agents.json にマージ
- ファイルが存在しない -> {"agents": [以下のエントリ]} として新規作成
- ファイルが既に存在する -> JSON として読み込み、agents 配列内で agent_id が {{agent_id}} のエントリを探す：見つかれば丸ごと置き換え、なければ追加する。
  ⛔ それ以外のエントリはそのまま残すこと。ファイル全体を上書きしたり、他の Agent を削除したりしないこと。

%s

3) 変更を適用
まず grix-connector status を実行して判断：
- daemon が起動していない -> grix-connector start
- daemon が既に起動している -> grix-connector reload（ホットリロード、他の Agent のセッションは中断されない）
⛔ Agent を追加するために restart を使わないこと——すべてが再接続され、進行中の会話が中断される。

4) 検証（必須）
grix-connector status は daemon の状態のみ報告し、Agent の一覧は表示しない。この Agent が実際に接続されたか確認するには、ローカルの admin API に問い合わせる（daemon 起動後、数秒かかる場合がある）：
curl -s http://127.0.0.1:19580/api/agents
出力に "name":"{{agent_name}}" と "alive":true が含まれているはず。（19580 はデフォルトポート。変更されている場合、実際のポートは ~/.grix/data/admin-port に記載。）

接続できない場合は、~/.grix/log/ 配下の最新のログを確認してください。実際にはほぼ次の3つのいずれか：qwenpaw が PATH に無い、CLI が起動しない、api_key のコピーが不完全。

詳細は grix-connector の README（インストール後は $(npm root -g)/grix-connector/README.md にある）の "Adding an agent to an existing setup" セクションを参照。

⚠️ api_key は使い捨ての秘密情報です。~/.grix/config/agents.json 以外には書き込まないこと。ログに出力したり git にコミットしたりしないこと。`

const qwenpawConnectorTaskKo = `이 Grix Agent를 이 컴퓨터의 grix-connector에 연결하세요. 순서대로 실행하고 완료되면 결과를 보고하세요.

전제 조건: 이 컴퓨터에 Node.js 18 이상과 uv(QwenPaw CLI에 필요)가 설치되어 있어야 합니다. 둘 중 하나라도 없으면 먼저 알려주세요 — 직접 설치하지 마세요.

참고: 이번 통합 테스트는 macOS에서만 실기 검증되었습니다. 아래 uv tool install은 Linux에서도 작동해야 하지만 아직 실기에서 검증되지 않았습니다 — 실패하면 보고해주세요.

0) QwenPaw CLI 설치 (이미 설치되어 있으면 건너뛰거나 필요시 업그레이드)
uv tool install qwenpaw
모델/프로바이더는 QwenPaw 쪽에서 설정합니다 (qwenpaw models config-key 또는 대화형으로 선택하는 qwenpaw models set-llm). 이것은 로그인이 아니므로 안내를 따르기만 하면 됩니다. 어떤 프로바이더를 선택할지 모르겠으면 먼저 알려주세요.

1) 커넥터 설치 (이미 설치되어 있으면 최신 버전으로 업그레이드)
%s

2) 아래 항목을 ~/.grix/config/agents.json에 병합
- 파일이 없으면 -> {"agents": [아래 항목]}으로 새로 생성
- 파일이 이미 있으면 -> JSON으로 읽어서 agents 배열에서 agent_id가 {{agent_id}}인 항목을 찾습니다: 있으면 통째로 교체하고, 없으면 추가합니다.
  ⛔ 다른 항목은 그대로 유지해야 합니다. 파일 전체를 덮어쓰거나 다른 Agent를 삭제하지 마세요.

%s

3) 변경 사항 적용
먼저 grix-connector status를 실행해서 판단하세요:
- daemon이 실행 중이 아니면 -> grix-connector start
- daemon이 이미 실행 중이면 -> grix-connector reload (핫 리로드, 다른 Agent의 세션은 중단되지 않음)
⛔ Agent를 추가하기 위해 restart를 사용하지 마세요 — 모든 것이 재연결되고 진행 중인 대화가 중단됩니다.

4) 검증 (필수)
grix-connector status는 daemon 상태만 보고하며 Agent 목록은 표시하지 않습니다. 이 Agent가 실제로 연결되었는지 확인하려면 로컬 admin API를 조회하세요 (daemon이 시작된 후 몇 초가 걸릴 수 있음):
curl -s http://127.0.0.1:19580/api/agents
출력에 "name":"{{agent_name}}"와 "alive":true가 포함되어 있어야 합니다. (19580은 기본 포트입니다. 변경되었다면 실제 포트는 ~/.grix/data/admin-port에 있습니다.)

연결되지 않으면 ~/.grix/log/ 아래의 최신 로그를 확인하세요. 실제로는 다음 세 가지 중 하나입니다: qwenpaw가 PATH에 없음, CLI가 시작되지 않음, api_key가 복사 중 잘림.

자세한 내용은 grix-connector README (설치 후 $(npm root -g)/grix-connector/README.md에 위치)의 "Adding an agent to an existing setup" 섹션을 참고하세요.

⚠️ api_key는 일회용 비밀 값입니다. ~/.grix/config/agents.json 외에는 쓰지 마세요. 로그에 출력하거나 git에 커밋하지 마세요.`

const qwenpawConnectorTaskDe = `Verbinde diesen Grix Agent mit dem grix-connector auf dieser Maschine. Führe die Schritte der Reihe nach aus und melde dich mit dem Ergebnis zurück.

Voraussetzung: Node.js 18+ und uv (das QwenPaw CLI braucht es) sind auf dieser Maschine installiert. Falls eines von beiden fehlt, sag mir zuerst Bescheid — installiere es nicht selbst.

Hinweis: Diese Integrationstestrunde wurde nur auf macOS verifiziert. Der folgende uv-tool-install-Befehl sollte auch unter Linux funktionieren, wurde aber noch nicht auf einer echten Linux-Maschine getestet — melde dich, falls es dort fehlschlägt.

0) Das QwenPaw CLI installieren (überspringen, falls bereits installiert, oder bei Bedarf aktualisieren)
uv tool install qwenpaw
Das Modell/der Provider wird auf QwenPaw-Seite konfiguriert (qwenpaw models config-key, oder interaktiv über qwenpaw models set-llm) — das ist keine Anmeldung, folge einfach den Anweisungen; sag mir zuerst Bescheid, falls unklar ist, welcher Provider gewählt werden soll.

1) Den Connector installieren (aktualisiert auf die neueste Version, falls bereits installiert)
%s

2) Den folgenden Eintrag in ~/.grix/config/agents.json einfügen
- Datei existiert nicht -> als {"agents": [der folgende Eintrag]} anlegen
- Datei existiert bereits -> als JSON einlesen, im agents-Array nach dem Eintrag mit agent_id {{agent_id}} suchen: falls gefunden ersetzen, falls nicht anhängen.
  ⛔ Alle anderen Einträge müssen unverändert bleiben. Nie die ganze Datei überschreiben, nie einen anderen Agent entfernen.

%s

3) Die Änderung übernehmen
Zuerst grix-connector status ausführen:
- Daemon läuft nicht -> grix-connector start
- Daemon läuft bereits -> grix-connector reload (Hot-Reload, laufende Agent-Sitzungen bleiben unberührt)
⛔ Verwende nicht restart, um einen Agent hinzuzufügen — das verbindet alles neu und unterbricht laufende Gespräche.

4) Überprüfen (erforderlich)
grix-connector status meldet nur den Daemon-Status, listet aber keine Agents auf. Um zu bestätigen, dass dieser Agent tatsächlich verbunden ist, die lokale Admin-API abfragen (der Daemon braucht nach dem Start eventuell ein paar Sekunden):
curl -s http://127.0.0.1:19580/api/agents
Die Ausgabe muss "name":"{{agent_name}}" mit "alive":true enthalten. (19580 ist der Standardport; falls geändert, steht der echte Port in ~/.grix/data/admin-port.)

Falls keine Verbindung zustande kommt, das neueste Log unter ~/.grix/log/ ansehen. In der Praxis ist es meist eines von drei Dingen: qwenpaw ist nicht im PATH, die CLI startet nicht, oder der api_key wurde beim Kopieren abgeschnitten.

Details siehe Abschnitt "Adding an agent to an existing setup" im README von grix-connector, das nach der Installation unter $(npm root -g)/grix-connector/README.md liegt.

Der api_key ist ein einmaliges Geheimnis: nur in ~/.grix/config/agents.json schreiben und sonst nirgends. Nicht in Logs ausgeben und nicht in git committen.`

const qwenpawConnectorTaskFr = `Connecte cet Agent Grix au grix-connector de cette machine. Exécute les étapes dans l'ordre et rends compte du résultat une fois terminé.

Prérequis : Node.js 18+ et uv (nécessaire au CLI QwenPaw) sont installés sur cette machine. Si l'un des deux manque, dis-le-moi d'abord — ne l'installe pas toi-même.

Remarque : cette phase de tests d'intégration n'a été vérifiée que sur macOS. La commande uv tool install ci-dessous devrait aussi fonctionner sur Linux, mais n'a pas encore été testée sur une machine réelle — signale-le si elle échoue.

0) Installer le CLI QwenPaw (passe cette étape si déjà installé, ou mets-le à jour si besoin)
uv tool install qwenpaw
Le modèle/fournisseur se configure côté QwenPaw (qwenpaw models config-key, ou le sélecteur interactif qwenpaw models set-llm) — ce n'est pas une connexion, suis simplement les instructions ; dis-le-moi d'abord si tu ne sais pas quel fournisseur choisir.

1) Installer le connecteur (met à jour vers la dernière version si déjà installé)
%s

2) Fusionner l'entrée ci-dessous dans ~/.grix/config/agents.json
- le fichier n'existe pas -> le créer comme {"agents": [l'entrée ci-dessous]}
- le fichier existe déjà -> le lire comme JSON, chercher dans le tableau agents l'entrée dont l'agent_id est {{agent_id}} : la remplacer entièrement si trouvée, l'ajouter sinon.
  ⛔ Toutes les autres entrées doivent rester intactes. Ne jamais écraser tout le fichier, ne jamais supprimer un autre Agent.

%s

3) Appliquer le changement
Exécuter d'abord grix-connector status :
- daemon non lancé -> grix-connector start
- daemon déjà lancé -> grix-connector reload (rechargement à chaud, les Agents en cours ne sont pas interrompus)
⛔ Ne pas utiliser restart pour ajouter un Agent — cela reconnecte tout et interrompt les conversations en cours.

4) Vérifier (obligatoire)
grix-connector status ne rapporte que l'état du daemon, il ne liste pas les agents. Pour confirmer que cet Agent est bien connecté, interroger l'API admin locale (le daemon peut mettre quelques secondes après son démarrage) :
curl -s http://127.0.0.1:19580/api/agents
La sortie doit contenir "name":"{{agent_name}}" avec "alive":true. (19580 est le port par défaut ; s'il a été changé, le vrai port est dans ~/.grix/data/admin-port.)

S'il ne se connecte jamais, lire le dernier log sous ~/.grix/log/. En pratique c'est l'une de ces trois choses : qwenpaw n'est pas dans le PATH, le CLI ne démarre pas, ou l'api_key a été tronquée lors de la copie.

Pour les détails, voir la section "Adding an agent to an existing setup" du README de grix-connector, livré avec le paquet dans $(npm root -g)/grix-connector/README.md.

L'api_key est un secret à usage unique : ne l'écris que dans ~/.grix/config/agents.json et nulle part ailleurs. Ne l'affiche pas dans les logs et ne la commite pas dans git.`

const qwenpawConnectorTaskEs = `Conecta este Agent de Grix al grix-connector de esta máquina. Ejecuta los pasos en orden e informa del resultado al terminar.

Requisito previo: Node.js 18+ y uv (lo necesita el CLI de QwenPaw) están instalados en esta máquina. Si falta alguno, dímelo primero — no lo instales tú mismo.

Nota: esta ronda de pruebas de integración solo se verificó en macOS. El comando uv tool install de abajo debería funcionar también en Linux, pero aún no se ha probado en una máquina real — informa si falla ahí.

0) Instalar el CLI de QwenPaw (omite este paso si ya está instalado, o actualízalo si hace falta)
uv tool install qwenpaw
El modelo/proveedor se configura en el lado de QwenPaw (qwenpaw models config-key, o el selector interactivo qwenpaw models set-llm) — eso no es un inicio de sesión, solo sigue las indicaciones; dímelo primero si no sabes qué proveedor elegir.

1) Instalar el conector (lo actualiza a la última versión si ya está instalado)
%s

2) Combinar la entrada de abajo en ~/.grix/config/agents.json
- el archivo no existe -> créalo como {"agents": [la entrada de abajo]}
- el archivo ya existe -> léelo como JSON, busca en el array agents la entrada cuyo agent_id sea {{agent_id}}: reemplázala si se encuentra, añádela si no.
  ⛔ El resto de entradas deben quedar intactas. Nunca sobrescribas el archivo completo, nunca elimines otro Agent.

%s

3) Aplicar el cambio
Ejecuta primero grix-connector status:
- daemon no está corriendo -> grix-connector start
- daemon ya está corriendo -> grix-connector reload (recarga en caliente, no interrumpe las sesiones de otros Agents)
⛔ No uses restart para añadir un Agent — eso reconecta todo e interrumpe las conversaciones en curso.

4) Verificar (obligatorio)
grix-connector status solo informa del estado del daemon, no lista los agents. Para confirmar que este Agent está realmente conectado, consulta la API admin local (el daemon puede tardar unos segundos tras arrancar):
curl -s http://127.0.0.1:19580/api/agents
La salida debe contener "name":"{{agent_name}}" con "alive":true. (19580 es el puerto por defecto; si se cambió, el puerto real está en ~/.grix/data/admin-port.)

Si nunca conecta, revisa el log más reciente en ~/.grix/log/. En la práctica suele ser una de tres cosas: qwenpaw no está en el PATH, el CLI no arranca, o el api_key se truncó al copiarlo.

Para más detalles, consulta la sección "Adding an agent to an existing setup" del README de grix-connector, que se incluye con el paquete en $(npm root -g)/grix-connector/README.md.

El api_key es un secreto de un solo uso: escríbelo únicamente en ~/.grix/config/agents.json. No lo imprimas en logs ni lo subas a git.`

const qwenpawConnectorTaskPt = `Conecte este Agent do Grix ao grix-connector desta máquina. Execute os passos em ordem e reporte o resultado ao terminar.

Pré-requisito: Node.js 18+ e uv (necessário para o CLI QwenPaw) estão instalados nesta máquina. Se algum estiver faltando, avise-me primeiro — não instale você mesmo.

Nota: esta rodada de testes de integração só foi verificada no macOS. O comando uv tool install abaixo deve funcionar também no Linux, mas ainda não foi testado em uma máquina real — reporte se falhar lá.

0) Instalar o CLI QwenPaw (pule esta etapa se já estiver instalado, ou atualize se necessário)
uv tool install qwenpaw
O modelo/provedor é configurado no lado do QwenPaw (qwenpaw models config-key, ou o seletor interativo qwenpaw models set-llm) — isso não é login, apenas siga as instruções; avise-me primeiro se não souber qual provedor escolher.

1) Instalar o conector (atualiza para a versão mais recente se já estiver instalado)
%s

2) Mesclar a entrada abaixo em ~/.grix/config/agents.json
- o arquivo não existe -> crie-o como {"agents": [a entrada abaixo]}
- o arquivo já existe -> leia-o como JSON, procure no array agents a entrada cujo agent_id seja {{agent_id}}: substitua-a inteira se encontrada, adicione se não.
  ⛔ As demais entradas devem permanecer intactas. Nunca sobrescreva o arquivo inteiro, nunca remova outro Agent.

%s

3) Aplicar a mudança
Execute primeiro grix-connector status:
- daemon não está rodando -> grix-connector start
- daemon já está rodando -> grix-connector reload (recarga a quente, não interrompe sessões de outros Agents)
⛔ Não use restart para adicionar um Agent — isso reconecta tudo e interrompe conversas em andamento.

4) Verificar (obrigatório)
grix-connector status só reporta o estado do daemon, não lista os agents. Para confirmar que este Agent está realmente conectado, consulte a API admin local (o daemon pode levar alguns segundos após iniciar):
curl -s http://127.0.0.1:19580/api/agents
A saída deve conter "name":"{{agent_name}}" com "alive":true. (19580 é a porta padrão; se foi alterada, a porta real está em ~/.grix/data/admin-port.)

Se nunca conectar, veja o log mais recente em ~/.grix/log/. Na prática costuma ser uma destas três coisas: qwenpaw não está no PATH, o CLI não inicia, ou o api_key foi truncado ao copiar.

Para mais detalhes, veja a seção "Adding an agent to an existing setup" do README do grix-connector, que acompanha o pacote em $(npm root -g)/grix-connector/README.md.

O api_key é um segredo de uso único: escreva-o apenas em ~/.grix/config/agents.json. Não o imprima em logs nem o envie para o git.`

const qwenpawConnectorTaskRu = `Подключи этого Grix Agent к grix-connector на этой машине. Выполняй шаги по порядку и сообщи о результате по завершении.

Предварительное условие: на этой машине установлены Node.js 18+ и uv (нужен CLI QwenPaw). Если чего-то из этого нет, сначала сообщи мне — не устанавливай сам.

Примечание: этот раунд интеграционного тестирования проверен только на macOS. Команда uv tool install ниже должна работать и на Linux, но на реальной машине ещё не проверялась — сообщи, если там она не сработает.

0) Установить CLI QwenPaw (пропусти, если уже установлен, или обнови при необходимости)
uv tool install qwenpaw
Модель/провайдер настраивается на стороне QwenPaw (qwenpaw models config-key либо интерактивный выбор через qwenpaw models set-llm) — это не вход, просто следуй подсказкам; если не уверен, какого провайдера выбрать, сначала сообщи мне.

1) Установить коннектор (обновит до последней версии, если уже установлен)
%s

2) Слить запись ниже в ~/.grix/config/agents.json
- файла нет -> создать его как {"agents": [запись ниже]}
- файл уже есть -> прочитать как JSON, найти в массиве agents запись с agent_id {{agent_id}}: если найдена — заменить целиком, если нет — добавить.
  ⛔ Все остальные записи должны остаться нетронутыми. Никогда не перезаписывай весь файл, никогда не удаляй другого Agent.

%s

3) Применить изменение
Сначала выполнить grix-connector status:
- daemon не запущен -> grix-connector start
- daemon уже запущен -> grix-connector reload (горячая перезагрузка, не прерывает сессии других Agent)
⛔ Не используй restart для добавления Agent — это переподключит всё и прервёт текущие разговоры.

4) Проверить (обязательно)
grix-connector status сообщает только о состоянии daemon, но не выводит список agent. Чтобы убедиться, что этот Agent действительно подключён, обратись к локальному admin API (после запуска daemon может потребоваться несколько секунд):
curl -s http://127.0.0.1:19580/api/agents
В выводе должно быть "name":"{{agent_name}}" с "alive":true. (19580 — порт по умолчанию; если он был изменён, реальный порт указан в ~/.grix/data/admin-port.)

Если подключения так и нет, посмотри последний лог в ~/.grix/log/. На практике это обычно одно из трёх: qwenpaw нет в PATH, CLI не запускается, либо api_key был обрезан при копировании.

Подробности — в разделе "Adding an agent to an existing setup" README grix-connector, который поставляется вместе с пакетом в $(npm root -g)/grix-connector/README.md.

api_key — это одноразовый секрет: записывай его только в ~/.grix/config/agents.json и больше никуда. Не выводи его в логи и не коммить в git.`

const qwenpawConnectorTaskAr = `اربط وكيل Grix هذا بـ grix-connector على هذا الجهاز. نفّذ الخطوات بالترتيب وأبلغني بالنتيجة عند الانتهاء.

المتطلب الأساسي: يجب أن يكون Node.js 18+ و uv (يحتاجه CLI الخاص بـ QwenPaw) مثبتَين على هذا الجهاز. إذا كان أحدهما ناقصًا، أخبرني أولاً — لا تثبّته بنفسك.

ملاحظة: هذه الجولة من اختبار التكامل تم التحقق منها على macOS فقط. من المفترض أن يعمل أمر uv tool install أدناه على Linux أيضًا، لكن لم يُختبر بعد على جهاز حقيقي — أبلغني إذا فشل هناك.

0) تثبيت CLI الخاص بـ QwenPaw (تخطَّ هذه الخطوة إذا كان مثبتًا بالفعل، أو حدّثه عند الحاجة)
uv tool install qwenpaw
يتم إعداد النموذج/المزوّد من جانب QwenPaw (qwenpaw models config-key، أو الاختيار التفاعلي عبر qwenpaw models set-llm) — هذا ليس تسجيل دخول، فقط اتبع التعليمات؛ أخبرني أولاً إذا لم تكن متأكدًا من المزوّد الذي يجب اختياره.

1) تثبيت الموصل (يحدّثه لأحدث إصدار إذا كان مثبتًا بالفعل)
%s

2) دمج المُدخَل أدناه في ~/.grix/config/agents.json
- إذا لم يكن الملف موجودًا -> أنشئه بالشكل {"agents": [المُدخَل أدناه]}
- إذا كان الملف موجودًا بالفعل -> اقرأه كـ JSON، وابحث في مصفوفة agents عن المُدخَل الذي يكون agent_id فيه {{agent_id}}: استبدله بالكامل إذا وُجد، وأضفه إذا لم يوجد.
  ⛔ يجب أن تبقى بقية المُدخَلات كما هي دون تغيير. لا تكتب فوق الملف بالكامل أبدًا، ولا تحذف وكيلًا آخر أبدًا.

%s

3) تطبيق التغيير
نفّذ أولاً grix-connector status:
- daemon غير يعمل -> grix-connector start
- daemon يعمل بالفعل -> grix-connector reload (إعادة تحميل فورية، لا تقطع جلسات الوكلاء الآخرين)
⛔ لا تستخدم restart لإضافة وكيل — فهذا يعيد ربط كل شيء ويقطع المحادثات الجارية.

4) التحقق (إلزامي)
يبلّغ grix-connector status فقط عن حالة daemon، ولا يسرد الوكلاء. للتأكد من أن هذا الوكيل متصل فعليًا، استعلم عن admin API المحلي (قد يحتاج daemon بضع ثوانٍ بعد بدء تشغيله):
curl -s http://127.0.0.1:19580/api/agents
يجب أن يحتوي الخرج على "name":"{{agent_name}}" مع "alive":true. (19580 هو المنفذ الافتراضي؛ إذا تم تغييره، فالمنفذ الفعلي موجود في ~/.grix/data/admin-port.)

إذا لم يتصل أبدًا، اقرأ أحدث سجل ضمن ~/.grix/log/. عمليًا يكون السبب أحد ثلاثة: qwenpaw غير موجود في PATH، أو CLI لا يبدأ التشغيل، أو تم اقتطاع api_key أثناء النسخ.

للتفاصيل، راجع قسم "Adding an agent to an existing setup" في ملف README الخاص بـ grix-connector، والذي يأتي مع الحزمة في $(npm root -g)/grix-connector/README.md.

api_key هو سرّ لمرة واحدة: اكتبه فقط في ~/.grix/config/agents.json ولا تكتبه في أي مكان آخر. لا تطبعه في السجلات ولا ترفعه إلى git.`

const qwenpawConnectorTaskHi = `इस Grix Agent को इस मशीन के grix-connector से कनेक्ट करें। चरण क्रम से चलाएँ और पूरा होने पर परिणाम बताएँ।

पूर्वशर्त: इस मशीन पर Node.js 18+ और uv (QwenPaw CLI को इसकी ज़रूरत है) इंस्टॉल होने चाहिए। अगर इनमें से कोई न हो, तो पहले मुझे बताएँ — इसे खुद इंस्टॉल न करें।

नोट: इंटीग्रेशन टेस्टिंग का यह राउंड केवल macOS पर वास्तविक मशीन पर जाँचा गया है। नीचे दिया गया uv tool install कमांड Linux पर भी चलना चाहिए, पर अभी तक वास्तविक Linux मशीन पर जाँचा नहीं गया है — अगर वहाँ फेल हो तो बताएँ।

0) QwenPaw CLI इंस्टॉल करें (पहले से इंस्टॉल है तो छोड़ें, या ज़रूरत पड़ने पर अपडेट करें)
uv tool install qwenpaw
मॉडल/प्रोवाइडर QwenPaw की तरफ़ से कॉन्फ़िगर होता है (qwenpaw models config-key, या इंटरैक्टिव qwenpaw models set-llm) — यह लॉगिन नहीं है, बस निर्देशों का पालन करें; अगर पता न हो कि कौन सा प्रोवाइडर चुनना है तो पहले मुझे बताएँ।

1) कनेक्टर इंस्टॉल करें (पहले से इंस्टॉल है तो नवीनतम संस्करण में अपडेट करें)
%s

2) नीचे दी गई प्रविष्टि को ~/.grix/config/agents.json में मर्ज करें
- फ़ाइल मौजूद नहीं है -> इसे {"agents": [नीचे दी गई प्रविष्टि]} के रूप में बनाएँ
- फ़ाइल पहले से मौजूद है -> इसे JSON के रूप में पढ़ें, agents ऐरे में वह प्रविष्टि खोजें जिसका agent_id {{agent_id}} है: मिले तो पूरी प्रविष्टि बदल दें, न मिले तो जोड़ दें।
  ⛔ बाकी सभी प्रविष्टियाँ ज्यों की त्यों रहनी चाहिए। पूरी फ़ाइल को कभी ओवरराइट न करें, किसी दूसरे Agent को कभी न हटाएँ।

%s

3) बदलाव लागू करें
पहले grix-connector status चलाकर देखें:
- daemon नहीं चल रहा -> grix-connector start
- daemon पहले से चल रहा है -> grix-connector reload (हॉट-रीलोड, बाकी Agent के सेशन बाधित नहीं होते)
⛔ Agent जोड़ने के लिए restart का उपयोग न करें — इससे सब कुछ फिर से कनेक्ट होगा और चल रही बातचीत बाधित होगी।

4) सत्यापन (अनिवार्य)
grix-connector status केवल daemon की स्थिति बताता है, Agent की सूची नहीं देता। यह पुष्टि करने के लिए कि यह Agent वाकई कनेक्ट हुआ है, स्थानीय admin API से पूछें (daemon शुरू होने के बाद कुछ सेकंड लग सकते हैं):
curl -s http://127.0.0.1:19580/api/agents
आउटपुट में "name":"{{agent_name}}" के साथ "alive":true होना चाहिए। (19580 डिफ़ॉल्ट पोर्ट है; अगर बदला गया है, तो असली पोर्ट ~/.grix/data/admin-port में है।)

अगर कभी कनेक्ट न हो, तो ~/.grix/log/ के अंतर्गत सबसे नया लॉग देखें। व्यावहारिक रूप से यह तीन में से एक होता है: qwenpaw PATH में नहीं है, CLI शुरू नहीं होता, या कॉपी करते समय api_key कट गया।

विस्तृत जानकारी के लिए grix-connector के README (इंस्टॉल के बाद $(npm root -g)/grix-connector/README.md पर) के "Adding an agent to an existing setup" सेक्शन को देखें।

api_key एक बार इस्तेमाल होने वाला गुप्त मान है: इसे केवल ~/.grix/config/agents.json में लिखें, कहीं और नहीं। इसे लॉग में न छापें और git में कमिट न करें।`

var qwenpawConnectorTasksI18n = map[string]string{
	"ja": qwenpawConnectorTaskJa,
	"ko": qwenpawConnectorTaskKo,
	"de": qwenpawConnectorTaskDe,
	"fr": qwenpawConnectorTaskFr,
	"es": qwenpawConnectorTaskEs,
	"pt": qwenpawConnectorTaskPt,
	"ru": qwenpawConnectorTaskRu,
	"ar": qwenpawConnectorTaskAr,
	"hi": qwenpawConnectorTaskHi,
}
