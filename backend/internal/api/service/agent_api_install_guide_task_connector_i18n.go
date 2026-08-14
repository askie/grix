package service

// Translations of connectorTaskZh/En (see agent_api_install_guide_service.go)
// into the app's other nine languages. Four %s slots: cli phrase, install
// command, agents.json entry, cli phrase. Command lines, paths and the {{...}}
// placeholders must stay verbatim in every language.

const connectorTaskJa = `この Grix Agent をこのマシンの grix-connector に接続してください。手順どおりに実行し、完了したら結果を報告してください。

前提：このマシンに Node.js 18+ と %s がインストール済みで、ログインも完了していること。どちらかが欠けている場合は、自分でインストールせず、まず私に知らせてください。

1) コネクタをインストール（インストール済みなら最新版へアップグレード）
%s

2) 下のエントリを ~/.grix/config/agents.json にマージ
- ファイルが存在しない → {"agents": [下のエントリ]} という内容で作成する。
- ファイルが存在する → JSON として読み込み、agents 配列から agent_id が {{agent_id}} のエントリを探す：見つかれば丸ごと置き換え、なければ追記する。
  ⛔ 他のエントリは一切変更しないこと。ファイル全体の上書きや、他の Agent の削除・改変は禁止。

%s

3) 設定を反映
まず grix-connector status を実行して判断：
- daemon が未起動 → grix-connector start
- daemon が起動中 → grix-connector reload（ホットリロード。他の Agent のセッションを中断しない）
⛔ Agent の追加に restart を使わないこと。全 Agent が再接続され、進行中の会話が中断される。

4) 検証（必須）
grix-connector status は daemon の状態しか報告せず、Agent の一覧は出ない。この Agent が本当に接続されたかは、ローカルの admin API で確認する（daemon 起動後、数秒待つことがある）：
curl -s http://127.0.0.1:19580/api/agents
出力に "name":"{{agent_name}}" と "alive":true が含まれていること。（19580 はデフォルトポート。変更している場合、実際のポートは ~/.grix/data/admin-port に書かれている。）

接続できない場合は ~/.grix/log/ の最新ログを確認。原因は実質次の三つのいずれか：%s が PATH にない、CLI が未ログイン、api_key のコピーが不完全。

詳細は grix-connector の README（インストール後は $(npm root -g)/grix-connector/README.md）の "Adding an agent to an existing setup" 節を参照。

⚠️ api_key は一度きりの資格情報。~/.grix/config/agents.json 以外には書かず、ログへの出力や git へのコミットは禁止。`

const connectorTaskKo = `이 Grix Agent를 이 컴퓨터의 grix-connector에 연결하세요. 순서대로 실행하고 완료되면 결과를 보고하세요.

전제 조건: 이 컴퓨터에 Node.js 18+ 및 %s가 설치되어 있고 로그인도 완료된 상태여야 합니다. 둘 중 하나라도 없으면 직접 설치하지 말고 먼저 알려주세요.

1) 커넥터 설치（이미 설치되어 있으면 최신 버전으로 업그레이드）
%s

2) 아래 항목을 ~/.grix/config/agents.json에 병합
- 파일이 없으면 → {"agents": [아래 항목]} 내용으로 새로 만든다.
- 파일이 있으면 → JSON으로 읽어 agents 배열에서 agent_id가 {{agent_id}}인 항목을 찾는다: 있으면 통째로 교체하고, 없으면 추가한다.
  ⛔ 다른 항목은 그대로 두어야 한다. 파일 전체 덮어쓰기, 다른 Agent 삭제·수정 금지.

%s

3) 설정 적용
먼저 grix-connector status를 실행해 판단:
- daemon 미실행 → grix-connector start
- daemon 실행 중 → grix-connector reload（핫 리로드, 다른 Agent 세션을 끊지 않음）
⛔ Agent 추가에 restart를 쓰지 말 것. 모든 Agent가 재연결되어 진행 중인 대화가 끊긴다.

4) 검증（필수）
grix-connector status는 데몬 상태만 보고하며 Agent 목록은 보여주지 않는다. 이 Agent가 실제로 연결됐는지는 로컬 admin API로 확인한다（daemon 기동 후 몇 초 걸릴 수 있음）:
curl -s http://127.0.0.1:19580/api/agents
출력에 "name":"{{agent_name}}"과 "alive":true가 있어야 한다.（19580은 기본 포트. 바꿨다면 실제 포트는 ~/.grix/data/admin-port에 있다.）

연결되지 않으면 ~/.grix/log/의 최신 로그를 확인한다. 원인은 사실상 셋 중 하나다: %s가 PATH에 없음, CLI 미로그인, api_key 복사 누락.

자세한 내용은 grix-connector README（설치 후 $(npm root -g)/grix-connector/README.md）의 "Adding an agent to an existing setup" 절 참고.

⚠️ api_key는 일회성 자격 증명이다. ~/.grix/config/agents.json 외에는 쓰지 말고, 로그 출력이나 git 커밋도 금지.`

const connectorTaskDe = `Verbinde diesen Grix Agent mit dem grix-connector auf dieser Maschine. Führe die Schritte der Reihe nach aus und melde dich mit dem Ergebnis zurück.

Voraussetzungen: Node.js 18+ und %s sind auf dieser Maschine installiert und eingeloggt. Fehlt eines davon, sag mir zuerst Bescheid — installiere es nicht selbst.

1) Connector installieren (aktualisiert auf die neueste Version, falls schon installiert)
%s

2) Den Eintrag unten in ~/.grix/config/agents.json mergen
- Datei existiert nicht -> als {"agents": [der Eintrag unten]} anlegen.
- Datei existiert -> als JSON einlesen und im agents-Array den Eintrag mit agent_id {{agent_id}} suchen: gefunden -> komplett ersetzen, sonst anhängen.
  Alle anderen Einträge bleiben unangetastet. Niemals die ganze Datei überschreiben, niemals einen anderen Agent entfernen.

%s

3) Änderung anwenden
Zuerst grix-connector status ausführen:
- Daemon läuft nicht -> grix-connector start
- Daemon läuft bereits -> grix-connector reload (lädt den neuen Agent im laufenden Betrieb, andere Agents bleiben unberührt)
Zum Hinzufügen eines Agents niemals restart verwenden — das verbindet alle Agents neu und unterbricht laufende Gespräche.

4) Verifizieren (Pflicht)
grix-connector status meldet nur den Daemon, keine Agent-Liste. Ob dieser Agent wirklich verbunden ist, zeigt die lokale Admin-API (dem Daemon nach dem Start ein paar Sekunden geben):
curl -s http://127.0.0.1:19580/api/agents
Die Ausgabe muss "name":"{{agent_name}}" mit "alive":true enthalten. (19580 ist der Standardport; wurde er geändert, steht der echte in ~/.grix/data/admin-port.)

Verbindet er sich nie, lies das neueste Log unter ~/.grix/log/. In der Praxis ist es eines von drei Dingen: %s ist nicht im PATH, das CLI ist nicht eingeloggt, oder der api_key wurde beim Kopieren abgeschnitten.

Details stehen im Abschnitt "Adding an agent to an existing setup" der grix-connector-README, die mit dem Paket unter $(npm root -g)/grix-connector/README.md ausgeliefert wird.

Der api_key ist ein Einmal-Geheimnis: nur in ~/.grix/config/agents.json schreiben und nirgendwo sonst. Nicht in Logs ausgeben, nicht in git committen.`

const connectorTaskFr = `Connecte cet Agent Grix au grix-connector de cette machine. Exécute les étapes dans l'ordre et rends compte du résultat une fois terminé.

Prérequis : Node.js 18+ et %s, installés et connectés sur cette machine. S'il manque l'un des deux, préviens-moi d'abord — ne l'installe pas toi-même.

1) Installer le connecteur (met à niveau vers la dernière version s'il est déjà installé)
%s

2) Fusionner l'entrée ci-dessous dans ~/.grix/config/agents.json
- le fichier n'existe pas -> le créer avec {"agents": [l'entrée ci-dessous]}
- le fichier existe -> le lire en JSON, chercher dans le tableau agents l'entrée dont l'agent_id est {{agent_id}} : la remplacer entièrement si trouvée, sinon l'ajouter.
  Toute autre entrée doit rester intacte. Ne jamais écraser le fichier entier, ne jamais supprimer un autre Agent.

%s

3) Appliquer le changement
Exécuter d'abord grix-connector status :
- daemon arrêté -> grix-connector start
- daemon déjà lancé -> grix-connector reload (recharge à chaud le nouvel Agent sans toucher aux autres)
Ne jamais utiliser restart pour ajouter un Agent — cela reconnecte tout et interrompt les conversations en cours.

4) Vérifier (obligatoire)
grix-connector status ne rapporte que le daemon, il ne liste pas les agents. Pour confirmer que cet Agent est bien connecté, interroger l'API admin locale (laisser quelques secondes au daemon après son démarrage) :
curl -s http://127.0.0.1:19580/api/agents
La sortie doit contenir "name":"{{agent_name}}" avec "alive":true. (19580 est le port par défaut ; s'il a été changé, le vrai port est dans ~/.grix/data/admin-port.)

S'il ne se connecte jamais, lire le log le plus récent sous ~/.grix/log/. En pratique c'est l'une de ces trois causes : %s absent du PATH, CLI non connecté, ou api_key tronqué à la copie.

Pour les détails, voir la section "Adding an agent to an existing setup" du README de grix-connector, livré avec le paquet dans $(npm root -g)/grix-connector/README.md.

L'api_key est un secret à usage unique : ne l'écrire que dans ~/.grix/config/agents.json et nulle part ailleurs. Ne pas l'afficher dans les logs, ne pas le committer dans git.`

const connectorTaskEs = `Conecta este Agent de Grix al grix-connector de esta máquina. Ejecuta los pasos en orden e informa del resultado al terminar.

Requisitos previos: Node.js 18+ y %s, instalados y con sesión iniciada en esta máquina. Si falta alguno, avísame primero — no lo instales por tu cuenta.

1) Instalar el conector (si ya está instalado, se actualiza a la última versión)
%s

2) Fusionar la entrada de abajo en ~/.grix/config/agents.json
- el archivo no existe -> créalo como {"agents": [la entrada de abajo]}
- el archivo ya existe -> léelo como JSON y busca en el array agents la entrada cuyo agent_id sea {{agent_id}}: reemplázala entera si existe, añádela si no.
  El resto de entradas debe quedar intacto. Nunca sobrescribas el archivo completo ni elimines otro Agent.

%s

3) Aplicar el cambio
Ejecuta primero grix-connector status:
- daemon parado -> grix-connector start
- daemon en marcha -> grix-connector reload (carga en caliente el nuevo Agent sin tocar los demás)
No uses restart para añadir un Agent — reconecta todo e interrumpe las conversaciones en curso.

4) Verificar (obligatorio)
grix-connector status solo informa del daemon, no lista los agents. Para confirmar que este Agent está conectado de verdad, consulta la API admin local (dale unos segundos al daemon tras arrancar):
curl -s http://127.0.0.1:19580/api/agents
La salida debe contener "name":"{{agent_name}}" con "alive":true. (19580 es el puerto por defecto; si se cambió, el real está en ~/.grix/data/admin-port.)

Si nunca conecta, lee el log más reciente en ~/.grix/log/. En la práctica es una de estas tres causas: %s no está en el PATH, el CLI no tiene sesión iniciada, o el api_key se truncó al copiarlo.

Para más detalle, consulta la sección "Adding an agent to an existing setup" del README de grix-connector, incluido en el paquete en $(npm root -g)/grix-connector/README.md.

El api_key es un secreto de un solo uso: escríbelo solo en ~/.grix/config/agents.json y en ningún otro sitio. No lo imprimas en logs ni lo subas a git.`

const connectorTaskPt = `Conecte este Agent do Grix ao grix-connector desta máquina. Execute os passos em ordem e reporte o resultado ao terminar.

Pré-requisitos: Node.js 18+ e %s, instalados e com login feito nesta máquina. Se faltar algum, me avise primeiro — não instale por conta própria.

1) Instalar o conector (se já estiver instalado, atualiza para a versão mais recente)
%s

2) Mesclar a entrada abaixo em ~/.grix/config/agents.json
- o arquivo não existe -> crie-o como {"agents": [a entrada abaixo]}
- o arquivo já existe -> leia-o como JSON e procure no array agents a entrada cujo agent_id seja {{agent_id}}: substitua-a inteira se existir, acrescente se não.
  Todas as outras entradas devem ficar intactas. Nunca sobrescreva o arquivo inteiro nem remova outro Agent.

%s

3) Aplicar a mudança
Execute primeiro grix-connector status:
- daemon parado -> grix-connector start
- daemon já rodando -> grix-connector reload (carrega o novo Agent a quente, sem tocar nos demais)
Não use restart para adicionar um Agent — ele reconecta tudo e interrompe conversas em andamento.

4) Verificar (obrigatório)
grix-connector status só informa o daemon, não lista os agents. Para confirmar que este Agent conectou de verdade, consulte a API admin local (dê alguns segundos ao daemon após iniciar):
curl -s http://127.0.0.1:19580/api/agents
A saída deve conter "name":"{{agent_name}}" com "alive":true. (19580 é a porta padrão; se foi alterada, a real está em ~/.grix/data/admin-port.)

Se nunca conectar, leia o log mais recente em ~/.grix/log/. Na prática é uma destas três causas: %s fora do PATH, CLI sem login, ou api_key truncado na cópia.

Para detalhes, veja a seção "Adding an agent to an existing setup" do README do grix-connector, que acompanha o pacote em $(npm root -g)/grix-connector/README.md.

O api_key é um segredo de uso único: escreva-o apenas em ~/.grix/config/agents.json e em nenhum outro lugar. Não o imprima em logs nem faça commit dele no git.`

const connectorTaskRu = `Подключи этого Grix Agent к grix-connector на этой машине. Выполняй шаги по порядку и сообщи о результате по завершении.

Предварительные условия: на этой машине установлены Node.js 18+ и %s, вход в CLI выполнен. Если чего-то не хватает — сначала сообщи мне, не устанавливай самостоятельно.

1) Установи коннектор (если уже установлен — обновится до последней версии)
%s

2) Добавь запись ниже в ~/.grix/config/agents.json
- файла нет -> создай его с содержимым {"agents": [запись ниже]}
- файл есть -> прочитай его как JSON и найди в массиве agents запись с agent_id {{agent_id}}: нашлась — замени целиком, нет — добавь.
  Остальные записи не трогать. Никогда не перезаписывай весь файл и не удаляй других Agent.

%s

3) Примени изменение
Сначала выполни grix-connector status:
- daemon не запущен -> grix-connector start
- daemon уже работает -> grix-connector reload (горячая загрузка нового Agent, остальные не затрагиваются)
Не используй restart для добавления Agent — он переподключает всех и обрывает идущие разговоры.

4) Проверка (обязательно)
grix-connector status сообщает только о демоне и не показывает список Agent. Чтобы убедиться, что этот Agent действительно подключился, обратись к локальному admin API (после старта демона подожди несколько секунд):
curl -s http://127.0.0.1:19580/api/agents
В выводе должно быть "name":"{{agent_name}}" и "alive":true. (19580 — порт по умолчанию; если его меняли, настоящий порт лежит в ~/.grix/data/admin-port.)

Если подключения так и нет, посмотри свежий лог в ~/.grix/log/. На практике причина одна из трёх: %s нет в PATH, CLI без входа, или api_key скопирован не полностью.

Подробности — в разделе "Adding an agent to an existing setup" README grix-connector, который идёт в пакете: $(npm root -g)/grix-connector/README.md.

api_key — одноразовый секрет: записывай его только в ~/.grix/config/agents.json и никуда больше. Не выводи в логи и не коммить в git.`

const connectorTaskAr = `اربط وكيل Grix هذا بـ grix-connector على هذا الجهاز. نفّذ الخطوات بالترتيب وأبلغني بالنتيجة عند الانتهاء.

المتطلبات المسبقة: Node.js 18+ و %s مثبّتان على هذا الجهاز مع تسجيل الدخول. إن كان أحدهما مفقودًا فأخبرني أولًا — لا تثبّته بنفسك.

1) ثبّت الموصّل (يُحدَّث إلى أحدث إصدار إن كان مثبّتًا)
%s

2) ادمج الإدخال أدناه في ‎~/.grix/config/agents.json
- الملف غير موجود -> أنشئه بالمحتوى {"agents": [الإدخال أدناه]}
- الملف موجود -> اقرأه كـ JSON وابحث في مصفوفة agents عن الإدخال الذي agent_id فيه هو {{agent_id}}: استبدله كاملًا إن وُجد، وإلا أضِفه.
  ⛔ يجب ترك بقية الإدخالات كما هي. يُمنع استبدال الملف كاملًا أو حذف/تعديل وكيل آخر.

%s

3) طبّق التغيير
نفّذ أولًا grix-connector status:
- الخدمة (daemon) غير شغّالة -> grix-connector start
- الخدمة شغّالة -> grix-connector reload (تحميل ساخن للوكيل الجديد دون المساس بالبقية)
⛔ لا تستخدم restart لإضافة وكيل — فهو يعيد اتصال الجميع ويقطع المحادثات الجارية.

4) التحقق (إلزامي)
grix-connector status يعرض حالة الخدمة فقط ولا يسرد الوكلاء. للتأكد من أن هذا الوكيل متصل فعلًا، استعلم عن admin API المحلي (امنح الخدمة بضع ثوانٍ بعد الإقلاع):
curl -s http://127.0.0.1:19580/api/agents
يجب أن يظهر في الناتج "name":"{{agent_name}}" مع "alive":true. (المنفذ الافتراضي 19580؛ إن غُيّر فالمنفذ الفعلي في ‎~/.grix/data/admin-port.)

إن لم يتصل أبدًا فاقرأ أحدث سجل في ‎~/.grix/log/. عمليًا السبب واحد من ثلاثة: %s ليس في PATH، أو لم يُسجَّل الدخول في CLI، أو نُسخ api_key منقوصًا.

للتفاصيل راجع قسم "Adding an agent to an existing setup" في README الخاص بـ grix-connector المرفق مع الحزمة في ‎$(npm root -g)/grix-connector/README.md.

⚠️ api_key سرّ يُستخدم مرة واحدة: اكتبه في ‎~/.grix/config/agents.json فقط ولا مكان آخر. لا تطبعه في السجلات ولا ترفعه إلى git.`

const connectorTaskHi = `इस Grix Agent को इस मशीन के grix-connector से कनेक्ट करें। चरण क्रम से चलाएँ और पूरा होने पर परिणाम बताएँ।

पूर्व-शर्तें: इस मशीन पर Node.js 18+ और %s इंस्टॉल हों तथा लॉगिन हो चुका हो। इनमें से कुछ भी न हो तो पहले मुझे बताएँ — खुद इंस्टॉल न करें।

1) कनेक्टर इंस्टॉल करें (पहले से इंस्टॉल हो तो नवीनतम संस्करण में अपग्रेड)
%s

2) नीचे दी गई एंट्री को ~/.grix/config/agents.json में मर्ज करें
- फ़ाइल नहीं है -> उसे {"agents": [नीचे की एंट्री]} सामग्री से बनाएं
- फ़ाइल है -> उसे JSON के रूप में पढ़ें और agents ऐरे में वह एंट्री खोजें जिसका agent_id {{agent_id}} है: मिले तो पूरी बदलें, न मिले तो जोड़ें।
  ⛔ बाकी सभी एंट्री ज्यों की त्यों रहें। पूरी फ़ाइल कभी न overwrite करें, किसी दूसरे Agent को न हटाएँ।

%s

3) बदलाव लागू करें
पहले grix-connector status चलाकर देखें:
- daemon नहीं चल रहा -> grix-connector start
- daemon चल रहा है -> grix-connector reload (नया Agent हॉट-लोड होता है, बाकी Agent अछूते रहते हैं)
⛔ Agent जोड़ने के लिए restart का उपयोग न करें — यह सबको फिर से कनेक्ट करता है और चालू बातचीत तोड़ देता है।

4) सत्यापन (अनिवार्य)
grix-connector status केवल daemon की स्थिति बताता है, Agent की सूची नहीं। यह Agent सचमुच जुड़ा या नहीं, इसके लिए लोकल admin API देखें (daemon शुरू होने के बाद कुछ सेकंड दें):
curl -s http://127.0.0.1:19580/api/agents
आउटपुट में "name":"{{agent_name}}" और "alive":true दिखना चाहिए। (19580 डिफ़ॉल्ट पोर्ट है; बदला गया हो तो असली पोर्ट ~/.grix/data/admin-port में है।)

कनेक्ट न हो तो ~/.grix/log/ का सबसे नया लॉग पढ़ें। व्यवहार में कारण इन तीन में से एक होता है: %s PATH में नहीं, CLI लॉगिन नहीं, या api_key कॉपी में कट गया।

विवरण के लिए grix-connector की README का "Adding an agent to an existing setup" खंड देखें, जो पैकेज के साथ $(npm root -g)/grix-connector/README.md पर मिलती है।

⚠️ api_key एक बार का सीक्रेट है: इसे केवल ~/.grix/config/agents.json में लिखें, कहीं और नहीं। लॉग में न छापें, git में कमिट न करें।`
