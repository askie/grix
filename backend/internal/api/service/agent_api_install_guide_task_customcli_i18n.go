package service

// customCliConnectorTaskTemplates holds the ja/ko/de/fr/es/pt/ru/ar/hi versions
// of customCliConnectorTaskZhTemplate / customCliConnectorTaskEnTemplate
// (agent_api_install_guide_service.go). Same 7 %s slots in the same order:
// nodeVersion, displayName, installCmd, loginInstruction,
// connectorInstallCommand, entry, binName. Used by customCliInstallGuide for
// qodercli/qoderclicn/mcode/dim/omp — round5 fix for those 5 client types
// falling back to English in every other app language (ja ko de fr es pt ru
// ar hi — keep in sync with frontend/assets/i18n).
var customCliConnectorTaskTemplates = map[string]string{
	"ja": customCliConnectorTaskJa,
	"ko": customCliConnectorTaskKo,
	"de": customCliConnectorTaskDe,
	"fr": customCliConnectorTaskFr,
	"es": customCliConnectorTaskEs,
	"pt": customCliConnectorTaskPt,
	"ru": customCliConnectorTaskRu,
	"ar": customCliConnectorTaskAr,
	"hi": customCliConnectorTaskHi,
}

const customCliConnectorTaskJa = `この Grix Agent をこのマシンの grix-connector に接続してください。手順どおりに実行し、完了したら結果を報告してください。

前提条件：このマシンに Node.js %s 以上がインストールされていること。なければ先に知らせてください。自分でインストールしないでください。

0) %s CLI をインストール（インストール済みならスキップ、必要なら最新版に更新）
%s
%s
まだインストールされていない、またはインストール済みでも起動できない場合は先に知らせてください。自分でインストールや認証を行わないでください——認証は人が完了する必要があります。

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

接続できない場合は、~/.grix/log/ 配下の最新のログを確認してください。実際にはほぼ次の3つのいずれか：%s が PATH に無い、CLI が起動しない、api_key のコピーが不完全。

詳細は grix-connector の README（インストール後は $(npm root -g)/grix-connector/README.md にある）の "Adding an agent to an existing setup" セクションを参照。

⚠️ api_key は使い捨ての秘密情報です。~/.grix/config/agents.json 以外には書き込まないこと。ログに出力したり git にコミットしたりしないこと。`

const customCliConnectorTaskKo = `이 Grix Agent를 이 컴퓨터의 grix-connector에 연결하세요. 순서대로 실행하고 완료되면 결과를 보고하세요.

전제 조건: 이 컴퓨터에 Node.js %s 이상이 설치되어 있어야 합니다. 설치되어 있지 않다면 먼저 알려주세요 — 직접 설치하지 마세요.

0) %s CLI 설치 (이미 설치되어 있으면 건너뛰거나 필요시 업그레이드)
%s
%s
설치되어 있지 않거나 설치되어 있어도 아직 실행되지 않는다면 먼저 알려주세요 — 직접 설치하거나 인증하지 마세요. 인증은 사람이 직접 완료해야 합니다.

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

연결되지 않으면 ~/.grix/log/ 아래의 최신 로그를 확인하세요. 실제로는 다음 세 가지 중 하나입니다: %s가 PATH에 없음, CLI가 시작되지 않음, api_key가 복사 중 잘림.

자세한 내용은 grix-connector README (설치 후 $(npm root -g)/grix-connector/README.md에 위치)의 "Adding an agent to an existing setup" 섹션을 참고하세요.

⚠️ api_key는 일회용 비밀 값입니다. ~/.grix/config/agents.json 외에는 쓰지 마세요. 로그에 출력하거나 git에 커밋하지 마세요.`

const customCliConnectorTaskDe = `Verbinde diesen Grix Agent mit dem grix-connector auf dieser Maschine. Führe die Schritte der Reihe nach aus und melde dich mit dem Ergebnis zurück.

Voraussetzung: Node.js %s+ ist auf dieser Maschine installiert. Falls nicht, sag mir zuerst Bescheid — installiere es nicht selbst.

0) %s CLI installieren (überspringen, falls bereits installiert, oder bei Bedarf aktualisieren)
%s
%s
Falls es nicht installiert ist, oder installiert, aber noch nicht lauffähig ist, sag mir zuerst Bescheid — installiere oder authentifiziere es nicht selbst, die Authentifizierung muss von einem Menschen abgeschlossen werden.

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

Falls keine Verbindung zustande kommt, das neueste Log unter ~/.grix/log/ ansehen. In der Praxis ist es meist eines von drei Dingen: %s ist nicht im PATH, die CLI startet nicht, oder der api_key wurde beim Kopieren abgeschnitten.

Details siehe Abschnitt "Adding an agent to an existing setup" im README von grix-connector, das nach der Installation unter $(npm root -g)/grix-connector/README.md liegt.

Der api_key ist ein einmaliges Geheimnis: nur in ~/.grix/config/agents.json schreiben und sonst nirgends. Nicht in Logs ausgeben und nicht in git committen.`

const customCliConnectorTaskFr = `Connecte cet Agent Grix au grix-connector de cette machine. Exécute les étapes dans l'ordre et rends compte du résultat une fois terminé.

Prérequis : Node.js %s+ est installé sur cette machine. Si ce n'est pas le cas, dis-le-moi d'abord — ne l'installe pas toi-même.

0) Installer le CLI %s (passe cette étape si déjà installé, ou mets-le à jour si besoin)
%s
%s
S'il n'est pas installé, ou installé mais pas encore capable de démarrer, dis-le-moi d'abord — ne l'installe pas et ne l'authentifie pas toi-même, l'authentification doit être terminée par un humain.

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

S'il ne se connecte jamais, lire le dernier log sous ~/.grix/log/. En pratique c'est l'une de ces trois choses : %s n'est pas dans le PATH, le CLI ne démarre pas, ou l'api_key a été tronquée lors de la copie.

Pour les détails, voir la section "Adding an agent to an existing setup" du README de grix-connector, livré avec le paquet dans $(npm root -g)/grix-connector/README.md.

L'api_key est un secret à usage unique : ne l'écris que dans ~/.grix/config/agents.json et nulle part ailleurs. Ne l'affiche pas dans les logs et ne la commite pas dans git.`

const customCliConnectorTaskEs = `Conecta este Agent de Grix al grix-connector de esta máquina. Ejecuta los pasos en orden e informa del resultado al terminar.

Requisito previo: Node.js %s+ está instalado en esta máquina. Si no lo está, dímelo primero — no lo instales tú mismo.

0) Instalar el CLI de %s (omite este paso si ya está instalado, o actualízalo si hace falta)
%s
%s
Si no está instalado, o está instalado pero aún no puede ejecutarse, dímelo primero — no lo instales ni lo autentiques tú mismo, la autenticación la debe completar una persona.

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

Si nunca conecta, revisa el log más reciente en ~/.grix/log/. En la práctica suele ser una de tres cosas: %s no está en el PATH, el CLI no arranca, o el api_key se truncó al copiarlo.

Para más detalles, consulta la sección "Adding an agent to an existing setup" del README de grix-connector, que se incluye con el paquete en $(npm root -g)/grix-connector/README.md.

El api_key es un secreto de un solo uso: escríbelo únicamente en ~/.grix/config/agents.json. No lo imprimas en logs ni lo subas a git.`

const customCliConnectorTaskPt = `Conecte este Agent do Grix ao grix-connector desta máquina. Execute os passos em ordem e reporte o resultado ao terminar.

Pré-requisito: Node.js %s+ está instalado nesta máquina. Se não estiver, avise-me primeiro — não instale você mesmo.

0) Instalar o CLI do %s (pule esta etapa se já estiver instalado, ou atualize se necessário)
%s
%s
Se não estiver instalado, ou estiver instalado mas ainda não conseguir rodar, avise-me primeiro — não instale nem autentique você mesmo, a autenticação precisa ser concluída por um humano.

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

Se nunca conectar, veja o log mais recente em ~/.grix/log/. Na prática costuma ser uma destas três coisas: %s não está no PATH, o CLI não inicia, ou o api_key foi truncado ao copiar.

Para mais detalhes, veja a seção "Adding an agent to an existing setup" do README do grix-connector, que acompanha o pacote em $(npm root -g)/grix-connector/README.md.

O api_key é um segredo de uso único: escreva-o apenas em ~/.grix/config/agents.json. Não o imprima em logs nem o envie para o git.`

const customCliConnectorTaskRu = `Подключи этого Grix Agent к grix-connector на этой машине. Выполняй шаги по порядку и сообщи о результате по завершении.

Предварительное условие: на этой машине установлен Node.js %s+. Если нет, сначала сообщи мне — не устанавливай его сам.

0) Установить CLI %s (пропусти, если уже установлен, или обнови при необходимости)
%s
%s
Если он не установлен, либо установлен, но ещё не запускается, сначала сообщи мне — не устанавливай и не авторизуй его сам, авторизацию должен завершить человек.

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

Если подключения так и нет, посмотри последний лог в ~/.grix/log/. На практике это обычно одно из трёх: %s нет в PATH, CLI не запускается, либо api_key был обрезан при копировании.

Подробности — в разделе "Adding an agent to an existing setup" README grix-connector, который поставляется вместе с пакетом в $(npm root -g)/grix-connector/README.md.

api_key — это одноразовый секрет: записывай его только в ~/.grix/config/agents.json и больше никуда. Не выводи его в логи и не коммить в git.`

const customCliConnectorTaskAr = `اربط وكيل Grix هذا بـ grix-connector على هذا الجهاز. نفّذ الخطوات بالترتيب وأبلغني بالنتيجة عند الانتهاء.

المتطلب الأساسي: يجب أن يكون Node.js %s+ مثبتًا على هذا الجهاز. إذا لم يكن كذلك، أخبرني أولاً — لا تثبّته بنفسك.

0) تثبيت CLI الخاص بـ %s (تخطَّ هذه الخطوة إذا كان مثبتًا بالفعل، أو حدّثه عند الحاجة)
%s
%s
إذا لم يكن مثبتًا، أو كان مثبتًا لكنه لا يعمل بعد، أخبرني أولاً — لا تثبّته أو توثّقه بنفسك، فالتوثيق يجب أن يُنجزه إنسان.

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

إذا لم يتصل أبدًا، اقرأ أحدث سجل ضمن ~/.grix/log/. عمليًا يكون السبب أحد ثلاثة: %s غير موجود في PATH، أو CLI لا يبدأ التشغيل، أو تم اقتطاع api_key أثناء النسخ.

للتفاصيل، راجع قسم "Adding an agent to an existing setup" في ملف README الخاص بـ grix-connector، والذي يأتي مع الحزمة في $(npm root -g)/grix-connector/README.md.

api_key هو سرّ لمرة واحدة: اكتبه فقط في ~/.grix/config/agents.json ولا تكتبه في أي مكان آخر. لا تطبعه في السجلات ولا ترفعه إلى git.`

const customCliConnectorTaskHi = `इस Grix Agent को इस मशीन के grix-connector से कनेक्ट करें। चरण क्रम से चलाएँ और पूरा होने पर परिणाम बताएँ।

पूर्वशर्त: इस मशीन पर Node.js %s+ इंस्टॉल होना चाहिए। अगर नहीं है, तो पहले मुझे बताएँ — इसे खुद इंस्टॉल न करें।

0) %s CLI इंस्टॉल करें (पहले से इंस्टॉल है तो छोड़ें, या ज़रूरत पड़ने पर अपडेट करें)
%s
%s
अगर यह इंस्टॉल नहीं है, या इंस्टॉल है पर अभी चल नहीं पा रहा, तो पहले मुझे बताएँ — इसे खुद इंस्टॉल या प्रमाणित (authenticate) न करें, प्रमाणीकरण किसी इंसान को ही पूरा करना होगा।

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

अगर कभी कनेक्ट न हो, तो ~/.grix/log/ के अंतर्गत सबसे नया लॉग देखें। व्यावहारिक रूप से यह तीन में से एक होता है: %s PATH में नहीं है, CLI शुरू नहीं होता, या कॉपी करते समय api_key कट गया।

विस्तृत जानकारी के लिए grix-connector के README (इंस्टॉल के बाद $(npm root -g)/grix-connector/README.md पर) के "Adding an agent to an existing setup" सेक्शन को देखें।

api_key एक बार इस्तेमाल होने वाला गुप्त मान है: इसे केवल ~/.grix/config/agents.json में लिखें, कहीं और नहीं। इसे लॉग में न छापें और git में कमिट न करें।`

// Per-CLI login-instruction sentence, one per app language, fed as the
// loginInstruction %s slot in customCliConnectorTaskTemplates (and the zh/en
// templates). zh/en also live here (not just inline at the call site) so all
// 11 languages for one CLI are visible together.

var qodercliLogin = localizedGuideText{
	"zh": "安装后执行 qodercli login，浏览器完成登录后再继续（首次使用必须登录才能用，不要跳过；该 CLI 使用你自己 Qoder 账号的模型额度计费，不经 Grix 中转）。",
	"en": "After installing, run qodercli login and finish the browser sign-in before continuing (login is required before first use — do not skip it; this CLI bills against your own Qoder account's model quota, not routed through the Grix relay).",
	"ja": "インストール後、qodercli login を実行しブラウザでのサインインを完了してから続行してください（初回利用前にログインが必須です——省略しないこと。この CLI はあなた自身の Qoder アカウントのモデル枠で課金され、Grix 中継を経由しません）。",
	"ko": "설치 후 qodercli login을 실행하고 브라우저 로그인을 완료한 다음 계속하세요 (처음 사용하기 전에 로그인이 필요합니다 — 건너뛰지 마세요. 이 CLI는 본인의 Qoder 계정 모델 할당량으로 과금되며 Grix 릴레이를 거치지 않습니다).",
	"de": "Führe nach der Installation qodercli login aus und schließe die Anmeldung im Browser ab, bevor du fortfährst (die Anmeldung ist vor der ersten Nutzung erforderlich — überspringe sie nicht; dieses CLI wird über das Modellkontingent deines eigenen Qoder-Kontos abgerechnet, nicht über das Grix-Relay).",
	"fr": "Après l'installation, exécute qodercli login et termine la connexion dans le navigateur avant de continuer (la connexion est obligatoire avant la première utilisation — ne la saute pas ; ce CLI est facturé sur le quota de modèle de ton propre compte Qoder, sans passer par le relais Grix).",
	"es": "Después de instalar, ejecuta qodercli login y completa el inicio de sesión en el navegador antes de continuar (el inicio de sesión es obligatorio antes del primer uso — no lo omitas; este CLI se factura contra la cuota de modelo de tu propia cuenta de Qoder, sin pasar por el relay de Grix).",
	"pt": "Depois de instalar, execute qodercli login e conclua o login no navegador antes de continuar (o login é obrigatório antes do primeiro uso — não pule esta etapa; este CLI é cobrado na cota de modelo da sua própria conta Qoder, sem passar pelo relay do Grix).",
	"ru": "После установки выполни qodercli login и заверши вход через браузер, прежде чем продолжать (вход обязателен перед первым использованием — не пропускай его; этот CLI тарифицируется по квоте модели твоего собственного аккаунта Qoder, минуя ретранслятор Grix).",
	"ar": "بعد التثبيت، نفّذ qodercli login وأكمل تسجيل الدخول عبر المتصفح قبل المتابعة (تسجيل الدخول إلزامي قبل أول استخدام — لا تتخطاه؛ يُحاسَب هذا الـ CLI على حصة النموذج الخاصة بحساب Qoder الخاص بك، دون المرور عبر ترحيل Grix).",
	"hi": "इंस्टॉल करने के बाद qodercli login चलाएँ और आगे बढ़ने से पहले ब्राउज़र में लॉगिन पूरा करें (पहली बार उपयोग से पहले लॉगिन ज़रूरी है — इसे न छोड़ें; यह CLI आपके अपने Qoder खाते के मॉडल कोटा पर बिल होता है, Grix रिले से नहीं गुज़रता)।",
}

var qoderclicnLogin = localizedGuideText{
	"zh": "安装后执行 qoderclicn login，浏览器完成登录后再继续（首次使用必须登录才能用，不要跳过；该 CLI 使用你自己 Qoder 账号的模型额度计费，不经 Grix 中转）。",
	"en": "After installing, run qoderclicn login and finish the browser sign-in before continuing (login is required before first use — do not skip it; this CLI bills against your own Qoder account's model quota, not routed through the Grix relay).",
	"ja": "インストール後、qoderclicn login を実行しブラウザでのサインインを完了してから続行してください（初回利用前にログインが必須です——省略しないこと。この CLI はあなた自身の Qoder アカウントのモデル枠で課金され、Grix 中継を経由しません）。",
	"ko": "설치 후 qoderclicn login을 실행하고 브라우저 로그인을 완료한 다음 계속하세요 (처음 사용하기 전에 로그인이 필요합니다 — 건너뛰지 마세요. 이 CLI는 본인의 Qoder 계정 모델 할당량으로 과금되며 Grix 릴레이를 거치지 않습니다).",
	"de": "Führe nach der Installation qoderclicn login aus und schließe die Anmeldung im Browser ab, bevor du fortfährst (die Anmeldung ist vor der ersten Nutzung erforderlich — überspringe sie nicht; dieses CLI wird über das Modellkontingent deines eigenen Qoder-Kontos abgerechnet, nicht über das Grix-Relay).",
	"fr": "Après l'installation, exécute qoderclicn login et termine la connexion dans le navigateur avant de continuer (la connexion est obligatoire avant la première utilisation — ne la saute pas ; ce CLI est facturé sur le quota de modèle de ton propre compte Qoder, sans passer par le relais Grix).",
	"es": "Después de instalar, ejecuta qoderclicn login y completa el inicio de sesión en el navegador antes de continuar (el inicio de sesión es obligatorio antes del primer uso — no lo omitas; este CLI se factura contra la cuota de modelo de tu propia cuenta de Qoder, sin pasar por el relay de Grix).",
	"pt": "Depois de instalar, execute qoderclicn login e conclua o login no navegador antes de continuar (o login é obrigatório antes do primeiro uso — não pule esta etapa; este CLI é cobrado na cota de modelo da sua própria conta Qoder, sem passar pelo relay do Grix).",
	"ru": "После установки выполни qoderclicn login и заверши вход через браузер, прежде чем продолжать (вход обязателен перед первым использованием — не пропускай его; этот CLI тарифицируется по квоте модели твоего собственного аккаунта Qoder, минуя ретранслятор Grix).",
	"ar": "بعد التثبيت، نفّذ qoderclicn login وأكمل تسجيل الدخول عبر المتصفح قبل المتابعة (تسجيل الدخول إلزامي قبل أول استخدام — لا تتخطاه؛ يُحاسَب هذا الـ CLI على حصة النموذج الخاصة بحساب Qoder الخاص بك، دون المرور عبر ترحيل Grix).",
	"hi": "इंस्टॉल करने के बाद qoderclicn login चलाएँ और आगे बढ़ने से पहले ब्राउज़र में लॉगिन पूरा करें (पहली बार उपयोग से पहले लॉगिन ज़रूरी है — इसे न छोड़ें; यह CLI आपके अपने Qoder खाते के मॉडल कोटा पर बिल होता है, Grix रिले से नहीं गुज़रता)।",
}

var mcodeLogin = localizedGuideText{
	"zh": "安装后执行 mcode login，浏览器完成登录后再继续（首次使用必须登录才能用，不要跳过；如需切换账号区域可加 --region cn 或 --region global；即使配置了 Grix 中转，session/new 仍要求先完成这一步登录，中转不能替代它）。",
	"en": "After installing, run mcode login and finish the browser sign-in before continuing (login is required before first use — do not skip it; add --region cn or --region global to switch account regions; session/new requires this login step even after Grix relay is configured — the relay does not replace it).",
	"ja": "インストール後、mcode login を実行しブラウザでのサインインを完了してから続行してください（初回利用前にログインが必須です——省略しないこと。アカウントのリージョンを切り替えるには --region cn または --region global を付ける。Grix 中継を設定していても session/new はこのログイン手順を要求します——中継はこれを代替しません）。",
	"ko": "설치 후 mcode login을 실행하고 브라우저 로그인을 완료한 다음 계속하세요 (처음 사용하기 전에 로그인이 필요합니다 — 건너뛰지 마세요. 계정 지역을 전환하려면 --region cn 또는 --region global을 추가하세요. Grix 릴레이가 구성되어 있어도 session/new는 이 로그인 단계를 요구합니다 — 릴레이가 이를 대체하지 않습니다).",
	"de": "Führe nach der Installation mcode login aus und schließe die Anmeldung im Browser ab, bevor du fortfährst (die Anmeldung ist vor der ersten Nutzung erforderlich — überspringe sie nicht; füge --region cn oder --region global hinzu, um die Kontoregion zu wechseln; session/new verlangt diesen Anmeldeschritt auch dann, wenn das Grix-Relay bereits konfiguriert ist — das Relay ersetzt ihn nicht).",
	"fr": "Après l'installation, exécute mcode login et termine la connexion dans le navigateur avant de continuer (la connexion est obligatoire avant la première utilisation — ne la saute pas ; ajoute --region cn ou --region global pour changer de région de compte ; session/new exige cette étape de connexion même après la configuration du relais Grix — le relais ne la remplace pas).",
	"es": "Después de instalar, ejecuta mcode login y completa el inicio de sesión en el navegador antes de continuar (el inicio de sesión es obligatorio antes del primer uso — no lo omitas; añade --region cn o --region global para cambiar la región de la cuenta; session/new exige este paso de inicio de sesión incluso con el relay de Grix ya configurado — el relay no lo sustituye).",
	"pt": "Depois de instalar, execute mcode login e conclua o login no navegador antes de continuar (o login é obrigatório antes do primeiro uso — não pule esta etapa; adicione --region cn ou --region global para trocar a região da conta; session/new exige esta etapa de login mesmo com o relay do Grix já configurado — o relay não a substitui).",
	"ru": "После установки выполни mcode login и заверши вход через браузер, прежде чем продолжать (вход обязателен перед первым использованием — не пропускай его; добавь --region cn или --region global, чтобы переключить регион аккаунта; session/new требует этот шаг входа даже после настройки ретранслятора Grix — ретранслятор его не заменяет).",
	"ar": "بعد التثبيت، نفّذ mcode login وأكمل تسجيل الدخول عبر المتصفح قبل المتابعة (تسجيل الدخول إلزامي قبل أول استخدام — لا تتخطاه؛ أضِف --region cn أو --region global لتبديل منطقة الحساب؛ يتطلب session/new خطوة تسجيل الدخول هذه حتى بعد إعداد ترحيل Grix — الترحيل لا يغني عنها).",
	"hi": "इंस्टॉल करने के बाद mcode login चलाएँ और आगे बढ़ने से पहले ब्राउज़र में लॉगिन पूरा करें (पहली बार उपयोग से पहले लॉगिन ज़रूरी है — इसे न छोड़ें; खाते का क्षेत्र बदलने के लिए --region cn या --region global जोड़ें; Grix रिले कॉन्फ़िगर होने के बाद भी session/new को यह लॉगिन चरण चाहिए — रिले इसकी जगह नहीं लेता)।",
}

var dimLogin = localizedGuideText{
	"zh": "安装后执行 dim auth login，浏览器完成登录后再继续（首次使用必须登录才能用，不要跳过；该 CLI 使用你自己 DimAgent 账号的模型额度计费，不经 Grix 中转）。",
	"en": "After installing, run dim auth login and finish the browser sign-in before continuing (login is required before first use — do not skip it; this CLI bills against your own DimAgent account's model quota, not routed through the Grix relay).",
	"ja": "インストール後、dim auth login を実行しブラウザでのサインインを完了してから続行してください（初回利用前にログインが必須です——省略しないこと。この CLI はあなた自身の DimAgent アカウントのモデル枠で課金され、Grix 中継を経由しません）。",
	"ko": "설치 후 dim auth login을 실행하고 브라우저 로그인을 완료한 다음 계속하세요 (처음 사용하기 전에 로그인이 필요합니다 — 건너뛰지 마세요. 이 CLI는 본인의 DimAgent 계정 모델 할당량으로 과금되며 Grix 릴레이를 거치지 않습니다).",
	"de": "Führe nach der Installation dim auth login aus und schließe die Anmeldung im Browser ab, bevor du fortfährst (die Anmeldung ist vor der ersten Nutzung erforderlich — überspringe sie nicht; dieses CLI wird über das Modellkontingent deines eigenen DimAgent-Kontos abgerechnet, nicht über das Grix-Relay).",
	"fr": "Après l'installation, exécute dim auth login et termine la connexion dans le navigateur avant de continuer (la connexion est obligatoire avant la première utilisation — ne la saute pas ; ce CLI est facturé sur le quota de modèle de ton propre compte DimAgent, sans passer par le relais Grix).",
	"es": "Después de instalar, ejecuta dim auth login y completa el inicio de sesión en el navegador antes de continuar (el inicio de sesión es obligatorio antes del primer uso — no lo omitas; este CLI se factura contra la cuota de modelo de tu propia cuenta de DimAgent, sin pasar por el relay de Grix).",
	"pt": "Depois de instalar, execute dim auth login e conclua o login no navegador antes de continuar (o login é obrigatório antes do primeiro uso — não pule esta etapa; este CLI é cobrado na cota de modelo da sua própria conta DimAgent, sem passar pelo relay do Grix).",
	"ru": "После установки выполни dim auth login и заверши вход через браузер, прежде чем продолжать (вход обязателен перед первым использованием — не пропускай его; этот CLI тарифицируется по квоте модели твоего собственного аккаунта DimAgent, минуя ретранслятор Grix).",
	"ar": "بعد التثبيت، نفّذ dim auth login وأكمل تسجيل الدخول عبر المتصفح قبل المتابعة (تسجيل الدخول إلزامي قبل أول استخدام — لا تتخطاه؛ يُحاسَب هذا الـ CLI على حصة النموذج الخاصة بحساب DimAgent الخاص بك، دون المرور عبر ترحيل Grix).",
	"hi": "इंस्टॉल करने के बाद dim auth login चलाएँ और आगे बढ़ने से पहले ब्राउज़र में लॉगिन पूरा करें (पहली बार उपयोग से पहले लॉगिन ज़रूरी है — इसे न छोड़ें; यह CLI आपके अपने DimAgent खाते के मॉडल कोटा पर बिल होता है, Grix रिले से नहीं गुज़रता)।",
}

var ompLogin = localizedGuideText{
	"zh": "omp 本身不需要单独登录；它按你配置的供应商工作（Grix 中转会自动写入虚拟 Key，或者你也可以自己配置厂商 API Key/OAuth，见 omp --help 的环境变量清单）。需要 bun ≥ 1.3.14（omp 包自己 package.json 的 engines 字段要求）。第一次运行前确认能执行 omp --version，如果报 env: bun: No such file or directory，说明上一步 bun 没装成功或不在 PATH 里。",
	"en": "omp does not need a separate login step; it works with whichever provider is configured (the Grix relay writes a virtual key automatically, or you can configure your own provider API key/OAuth — see the environment variable list in omp --help). Requires bun >= 1.3.14 (per the omp package's own engines field). Before first use, confirm omp --version runs; if it reports env: bun: No such file or directory, bun did not install correctly or is not on PATH.",
	"ja": "omp 自体には個別のログイン手順は不要です。設定されたプロバイダに従って動作します（Grix 中継が仮想キーを自動的に書き込むか、自分でプロバイダの API キー/OAuth を設定することもできます——omp --help の環境変数一覧を参照）。bun ≥ 1.3.14 が必要です（omp パッケージ自身の package.json の engines フィールドによる要件）。初回利用前に omp --version が実行できることを確認してください。env: bun: No such file or directory と表示される場合、bun のインストールが失敗しているか PATH に含まれていません。",
	"ko": "omp 자체는 별도의 로그인 단계가 필요하지 않습니다. 구성된 프로바이더에 따라 작동합니다 (Grix 릴레이가 가상 키를 자동으로 기록하거나, 직접 프로바이더의 API 키/OAuth를 구성할 수 있습니다 — omp --help의 환경 변수 목록 참고). bun >= 1.3.14가 필요합니다 (omp 패키지 자체 package.json의 engines 필드 기준). 처음 사용하기 전에 omp --version이 실행되는지 확인하세요. env: bun: No such file or directory가 표시되면 bun이 제대로 설치되지 않았거나 PATH에 없는 것입니다.",
	"de": "omp selbst benötigt keinen separaten Anmeldeschritt; es arbeitet mit dem jeweils konfigurierten Provider (das Grix-Relay schreibt automatisch einen virtuellen Key, oder du konfigurierst deinen eigenen Provider-API-Key/OAuth — siehe die Liste der Umgebungsvariablen in omp --help). Benötigt bun >= 1.3.14 (laut dem engines-Feld des omp-Pakets selbst). Bestätige vor der ersten Nutzung, dass omp --version läuft; meldet es env: bun: No such file or directory, wurde bun nicht korrekt installiert oder ist nicht im PATH.",
	"fr": "omp n'a pas besoin d'une étape de connexion séparée ; il fonctionne avec le fournisseur configuré (le relais Grix écrit automatiquement une clé virtuelle, ou tu peux configurer toi-même la clé API/OAuth de ton fournisseur — voir la liste des variables d'environnement dans omp --help). Nécessite bun >= 1.3.14 (selon le champ engines du paquet omp lui-même). Avant la première utilisation, vérifie que omp --version fonctionne ; s'il indique env: bun: No such file or directory, bun ne s'est pas installé correctement ou n'est pas dans le PATH.",
	"es": "omp no necesita un paso de inicio de sesión independiente; funciona con el proveedor que esté configurado (el relay de Grix escribe una clave virtual automáticamente, o puedes configurar tu propia clave API/OAuth de proveedor — consulta la lista de variables de entorno en omp --help). Requiere bun >= 1.3.14 (según el campo engines del propio paquete omp). Antes del primer uso, confirma que omp --version se ejecuta; si informa env: bun: No such file or directory, bun no se instaló correctamente o no está en el PATH.",
	"pt": "O omp em si não precisa de uma etapa de login separada; ele funciona com o provedor que estiver configurado (o relay do Grix grava uma chave virtual automaticamente, ou você pode configurar sua própria chave de API/OAuth do provedor — veja a lista de variáveis de ambiente em omp --help). Requer bun >= 1.3.14 (conforme o campo engines do próprio pacote omp). Antes do primeiro uso, confirme que omp --version funciona; se reportar env: bun: No such file or directory, o bun não foi instalado corretamente ou não está no PATH.",
	"ru": "Сам omp не требует отдельного шага входа; он работает с настроенным провайдером (ретранслятор Grix автоматически записывает виртуальный ключ, либо ты можешь настроить собственный API-ключ/OAuth провайдера — см. список переменных окружения в omp --help). Требуется bun >= 1.3.14 (согласно полю engines самого пакета omp). Перед первым использованием убедись, что omp --version запускается; если появляется env: bun: No such file or directory, значит bun установлен некорректно или отсутствует в PATH.",
	"ar": "لا يحتاج omp نفسه إلى خطوة تسجيل دخول منفصلة؛ فهو يعمل مع أي مزوّد تم إعداده (يكتب ترحيل Grix مفتاحًا افتراضيًا تلقائيًا، أو يمكنك إعداد مفتاح API/OAuth الخاص بمزوّدك بنفسك — راجع قائمة متغيرات البيئة في omp --help). يتطلب bun >= 1.3.14 (وفق حقل engines الخاص بحزمة omp نفسها). قبل أول استخدام، تأكد من أن omp --version يعمل؛ إذا ظهرت الرسالة env: bun: No such file or directory فهذا يعني أن bun لم يُثبَّت بشكل صحيح أو أنه غير موجود في PATH.",
	"hi": "omp को स्वयं अलग लॉगिन चरण की ज़रूरत नहीं है; यह जो भी प्रोवाइडर कॉन्फ़िगर किया गया है उसके साथ काम करता है (Grix रिले स्वचालित रूप से एक वर्चुअल की लिखता है, या आप स्वयं अपने प्रोवाइडर की API की/OAuth कॉन्फ़िगर कर सकते हैं — omp --help की एनवायरनमेंट वेरिएबल सूची देखें)। bun >= 1.3.14 आवश्यक है (omp पैकेज के अपने package.json के engines फ़ील्ड के अनुसार)। पहली बार उपयोग से पहले पुष्टि करें कि omp --version चलता है; अगर यह env: bun: No such file or directory रिपोर्ट करे, तो इसका मतलब है bun सही से इंस्टॉल नहीं हुआ या PATH में नहीं है।",
}

var codebuddyLogin = localizedGuideText{
	"zh": "安装后先手动登录一次：在终端运行 codebuddy 进入交互会话，输入 /login，四种方式（企业 iOA / Google 或 GitHub / 微信 / 企业域）任选一种完成登录后再继续（首次使用必须登录才能用，不要跳过；/login 是应用内的斜杠命令，不是可以直接在 shell 里跑的子命令；该 CLI 使用你自己 CodeBuddy 账号的模型额度计费，不经 Grix 中转）。",
	"en": "After installing, log in once by hand first: run codebuddy in a terminal to enter an interactive session, then type /login and complete sign-in through any one of the four methods (enterprise iOA / Google or GitHub / WeChat / enterprise domain) before continuing (login is required before first use — do not skip it; /login is an in-app slash command, not a shell subcommand you can run directly; this CLI bills against your own CodeBuddy account's model quota, not routed through the Grix relay).",
	"ja": "インストール後、まず手動で一度ログインしてください：ターミナルで codebuddy を実行して対話セッションに入り、/login と入力して4種類の方法（企業 iOA／Google または GitHub／WeChat／企業ドメイン）のいずれかでサインインを完了してから続行してください（初回利用前にログインが必須です——省略しないこと。/login はアプリ内のスラッシュコマンドであり、シェルで直接実行できるサブコマンドではありません。この CLI はあなた自身の CodeBuddy アカウントのモデル枠で課金され、Grix 中継を経由しません）。",
	"ko": "설치 후 먼저 수동으로 한 번 로그인하세요: 터미널에서 codebuddy를 실행해 대화형 세션에 들어간 다음 /login을 입력하고 네 가지 방법(기업 iOA / Google 또는 GitHub / WeChat / 기업 도메인) 중 하나로 로그인을 완료한 뒤 계속하세요 (처음 사용하기 전에 로그인이 필요합니다 — 건너뛰지 마세요. /login은 앱 내 슬래시 명령이며 셸에서 직접 실행할 수 있는 하위 명령이 아닙니다. 이 CLI는 본인의 CodeBuddy 계정 모델 할당량으로 과금되며 Grix 릴레이를 거치지 않습니다).",
	"de": "Melde dich nach der Installation zunächst einmal manuell an: Starte codebuddy im Terminal, um eine interaktive Sitzung zu öffnen, gib dann /login ein und schließe die Anmeldung über eine der vier Methoden ab (Enterprise iOA / Google oder GitHub / WeChat / Enterprise-Domain), bevor du fortfährst (die Anmeldung ist vor der ersten Nutzung erforderlich — überspringe sie nicht; /login ist ein In-App-Slash-Befehl, kein Shell-Unterbefehl, den du direkt ausführen kannst; dieses CLI wird über das Modellkontingent deines eigenen CodeBuddy-Kontos abgerechnet, nicht über das Grix-Relay).",
	"fr": "Après l'installation, connecte-toi d'abord une fois manuellement : lance codebuddy dans un terminal pour entrer en session interactive, puis tape /login et termine la connexion via l'une des quatre méthodes (iOA d'entreprise / Google ou GitHub / WeChat / domaine d'entreprise) avant de continuer (la connexion est obligatoire avant la première utilisation — ne la saute pas ; /login est une commande slash intégrée à l'application, pas une sous-commande shell exécutable directement ; ce CLI est facturé sur le quota de modèle de ton propre compte CodeBuddy, sans passer par le relais Grix).",
	"es": "Después de instalar, inicia sesión primero manualmente una vez: ejecuta codebuddy en una terminal para entrar en una sesión interactiva, luego escribe /login y completa el inicio de sesión mediante uno de los cuatro métodos (iOA empresarial / Google o GitHub / WeChat / dominio empresarial) antes de continuar (el inicio de sesión es obligatorio antes del primer uso — no lo omitas; /login es un comando slash dentro de la app, no un subcomando de shell que puedas ejecutar directamente; este CLI se factura contra la cuota de modelo de tu propia cuenta de CodeBuddy, sin pasar por el relay de Grix).",
	"pt": "Depois de instalar, faça login manualmente uma vez primeiro: execute codebuddy em um terminal para entrar em uma sessão interativa, depois digite /login e conclua o login por um dos quatro métodos (iOA corporativo / Google ou GitHub / WeChat / domínio corporativo) antes de continuar (o login é obrigatório antes do primeiro uso — não pule esta etapa; /login é um comando slash interno do app, não um subcomando de shell executável diretamente; este CLI é cobrado na cota de modelo da sua própria conta CodeBuddy, sem passar pelo relay do Grix).",
	"ru": "После установки сначала выполни вход вручную один раз: запусти codebuddy в терминале, чтобы войти в интерактивную сессию, затем введи /login и заверши вход одним из четырёх способов (корпоративный iOA / Google или GitHub / WeChat / корпоративный домен), прежде чем продолжать (вход обязателен перед первым использованием — не пропускай его; /login — это внутренняя слэш-команда приложения, а не подкоманда shell, которую можно запустить напрямую; этот CLI тарифицируется по квоте модели твоего собственного аккаунта CodeBuddy, минуя ретранслятор Grix).",
	"ar": "بعد التثبيت، سجّل الدخول يدويًا مرة واحدة أولاً: شغّل codebuddy في الطرفية للدخول إلى جلسة تفاعلية، ثم اكتب /login وأكمل تسجيل الدخول عبر إحدى الطرق الأربع (iOA المؤسسي / Google أو GitHub / WeChat / نطاق المؤسسة) قبل المتابعة (تسجيل الدخول إلزامي قبل أول استخدام — لا تتخطاه؛ /login هو أمر شرطة مائلة داخل التطبيق وليس أمرًا فرعيًا في الصدفة يمكن تشغيله مباشرة؛ يُحاسَب هذا الـ CLI على حصة النموذج الخاصة بحساب CodeBuddy الخاص بك، دون المرور عبر ترحيل Grix).",
	"hi": "इंस्टॉल करने के बाद पहले एक बार मैन्युअल रूप से लॉगिन करें: टर्मिनल में codebuddy चलाकर इंटरैक्टिव सेशन में जाएँ, फिर /login टाइप करें और चार तरीकों (एंटरप्राइज़ iOA / Google या GitHub / WeChat / एंटरप्राइज़ डोमेन) में से किसी एक से साइन-इन पूरा करके ही आगे बढ़ें (पहली बार उपयोग से पहले लॉगिन ज़रूरी है — इसे न छोड़ें; /login ऐप के भीतर की स्लैश कमांड है, शेल में सीधे चलाई जाने वाली सबकमांड नहीं; यह CLI आपके अपने CodeBuddy खाते के मॉडल कोटा पर बिल होता है, Grix रिले से नहीं गुज़रता)।",
}
