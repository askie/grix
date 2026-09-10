package service

// traecliConnectorTaskJa / Ko / De / Fr / Es / Pt / Ru / Ar / Hi mirror
// traecliConnectorTaskZh / En (agent_api_install_guide_service.go) — round5
// fix for traecli falling back to English in ja/ko/de/fr/es/pt/ru/ar/hi.
// Same 2 %s slots in the same order: connectorInstallCommand, entry.

const traecliConnectorTaskJa = `この Grix Agent をこのマシンの grix-connector に接続してください。手順どおりに実行し、完了したら結果を報告してください。

前提条件：このマシンに Node.js 22.19 以上がインストールされていること（コネクタに必要）。なければ先に知らせてください。自分でインストールしないでください。

0) 公式の TraeCode CLI をインストール（インストール済みならスキップ、必要なら最新版に更新）
curl -fsSL https://trae.cn/trae-cli/install.sh | bash
注：これは TRAE IDE（Trae.app / Trae CN.app）ではありません。独立した公式コマンドラインツールです。インストールすると ~/.local/bin に traecli コマンドが生成され、IDE は不要です。
起動時に企業アカウントの SSO ログイン画面が表示される、または /login を促された場合は先に知らせてください。自分でログインしないでください——認証は人が完了する必要があります。中継の資格情報が設定済みなら、traecli は別途ログインせずに自動でモデル設定を読み取ります。

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

接続できない場合は、~/.grix/log/ 配下の最新のログを確認してください。実際にはほぼ次の3つのいずれか：traecli が PATH に無い、CLI が起動しない、api_key のコピーが不完全。

詳細は grix-connector の README（インストール後は $(npm root -g)/grix-connector/README.md にある）の "Adding an agent to an existing setup" セクションを参照。

⚠️ api_key は使い捨ての秘密情報です。~/.grix/config/agents.json 以外には書き込まないこと。ログに出力したり git にコミットしたりしないこと。`

const traecliConnectorTaskKo = `이 Grix Agent를 이 컴퓨터의 grix-connector에 연결하세요. 순서대로 실행하고 완료되면 결과를 보고하세요.

전제 조건: 이 컴퓨터에 Node.js 22.19 이상이 설치되어 있어야 합니다 (커넥터에 필요). 설치되어 있지 않다면 먼저 알려주세요 — 직접 설치하지 마세요.

0) 공식 TraeCode CLI 설치 (이미 설치되어 있으면 건너뛰거나 필요시 업그레이드)
curl -fsSL https://trae.cn/trae-cli/install.sh | bash
참고: 이것은 TRAE IDE(Trae.app / Trae CN.app)가 아닙니다 — 독립적인 공식 명령줄 도구입니다. 설치하면 ~/.local/bin에 traecli 명령이 생성되며 IDE는 필요하지 않습니다.
시작 시 기업 계정 SSO 로그인 페이지가 나타나거나 /login이 표시되면 먼저 알려주세요 — 직접 로그인하지 마세요. 인증은 사람이 직접 완료해야 합니다. 릴레이 자격 증명이 구성되면 traecli는 별도의 로그인 없이 모델 설정을 자동으로 읽어옵니다.

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

연결되지 않으면 ~/.grix/log/ 아래의 최신 로그를 확인하세요. 실제로는 다음 세 가지 중 하나입니다: traecli가 PATH에 없음, CLI가 시작되지 않음, api_key가 복사 중 잘림.

자세한 내용은 grix-connector README (설치 후 $(npm root -g)/grix-connector/README.md에 위치)의 "Adding an agent to an existing setup" 섹션을 참고하세요.

⚠️ api_key는 일회용 비밀 값입니다. ~/.grix/config/agents.json 외에는 쓰지 마세요. 로그에 출력하거나 git에 커밋하지 마세요.`

const traecliConnectorTaskDe = `Verbinde diesen Grix Agent mit dem grix-connector auf dieser Maschine. Führe die Schritte der Reihe nach aus und melde dich mit dem Ergebnis zurück.

Voraussetzung: Node.js 22.19+ ist auf dieser Maschine installiert (der Connector braucht es). Falls nicht, sag mir zuerst Bescheid — installiere es nicht selbst.

0) Das offizielle TraeCode CLI installieren (überspringen, falls bereits installiert, oder bei Bedarf aktualisieren)
curl -fsSL https://trae.cn/trae-cli/install.sh | bash
Hinweis: Das ist nicht die TRAE IDE (Trae.app / Trae CN.app) — es ist ein eigenständiges offizielles Kommandozeilenwerkzeug. Die Installation erzeugt den Befehl traecli unter ~/.local/bin; die IDE wird nicht benötigt.
Falls beim Start eine Unternehmens-SSO-Anmeldeseite erscheint oder /login angezeigt wird, sag mir zuerst Bescheid — melde dich nicht selbst an, die Authentifizierung muss von einem Menschen abgeschlossen werden. Sobald die Relay-Zugangsdaten konfiguriert sind, übernimmt traecli die Modellkonfiguration automatisch, ohne einen separaten Anmeldeschritt.

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

Falls keine Verbindung zustande kommt, das neueste Log unter ~/.grix/log/ ansehen. In der Praxis ist es meist eines von drei Dingen: traecli ist nicht im PATH, die CLI startet nicht, oder der api_key wurde beim Kopieren abgeschnitten.

Details siehe Abschnitt "Adding an agent to an existing setup" im README von grix-connector, das nach der Installation unter $(npm root -g)/grix-connector/README.md liegt.

Der api_key ist ein einmaliges Geheimnis: nur in ~/.grix/config/agents.json schreiben und sonst nirgends. Nicht in Logs ausgeben und nicht in git committen.`

const traecliConnectorTaskFr = `Connecte cet Agent Grix au grix-connector de cette machine. Exécute les étapes dans l'ordre et rends compte du résultat une fois terminé.

Prérequis : Node.js 22.19+ est installé sur cette machine (le connecteur en a besoin). Si ce n'est pas le cas, dis-le-moi d'abord — ne l'installe pas toi-même.

0) Installer le CLI officiel TraeCode (passe cette étape si déjà installé, ou mets-le à jour si besoin)
curl -fsSL https://trae.cn/trae-cli/install.sh | bash
Remarque : ce n'est pas l'IDE TRAE (Trae.app / Trae CN.app) — c'est un outil en ligne de commande officiel autonome. L'installation crée la commande traecli sous ~/.local/bin ; l'IDE n'est pas nécessaire.
Si une page de connexion SSO d'entreprise s'affiche au démarrage, ou si /login est demandé, dis-le-moi d'abord — ne te connecte pas toi-même, l'authentification doit être terminée par un humain. Une fois les identifiants du relais configurés, traecli récupère automatiquement la configuration du modèle, sans étape de connexion séparée.

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

S'il ne se connecte jamais, lire le dernier log sous ~/.grix/log/. En pratique c'est l'une de ces trois choses : traecli n'est pas dans le PATH, le CLI ne démarre pas, ou l'api_key a été tronquée lors de la copie.

Pour les détails, voir la section "Adding an agent to an existing setup" du README de grix-connector, livré avec le paquet dans $(npm root -g)/grix-connector/README.md.

L'api_key est un secret à usage unique : ne l'écris que dans ~/.grix/config/agents.json et nulle part ailleurs. Ne l'affiche pas dans les logs et ne la commite pas dans git.`

const traecliConnectorTaskEs = `Conecta este Agent de Grix al grix-connector de esta máquina. Ejecuta los pasos en orden e informa del resultado al terminar.

Requisito previo: Node.js 22.19+ está instalado en esta máquina (el conector lo necesita). Si no lo está, dímelo primero — no lo instales tú mismo.

0) Instalar el CLI oficial de TraeCode (omite este paso si ya está instalado, o actualízalo si hace falta)
curl -fsSL https://trae.cn/trae-cli/install.sh | bash
Nota: esto no es el IDE TRAE (Trae.app / Trae CN.app) — es una herramienta de línea de comandos oficial independiente. Al instalarla se genera el comando traecli en ~/.local/bin; no se necesita el IDE.
Si al iniciar aparece una página de inicio de sesión SSO empresarial, o pide /login, dímelo primero — no inicies sesión tú mismo, la autenticación la debe completar una persona. Una vez configuradas las credenciales del relay, traecli obtiene la configuración del modelo automáticamente, sin un paso de inicio de sesión aparte.

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

Si nunca conecta, revisa el log más reciente en ~/.grix/log/. En la práctica suele ser una de tres cosas: traecli no está en el PATH, el CLI no arranca, o el api_key se truncó al copiarlo.

Para más detalles, consulta la sección "Adding an agent to an existing setup" del README de grix-connector, que se incluye con el paquete en $(npm root -g)/grix-connector/README.md.

El api_key es un secreto de un solo uso: escríbelo únicamente en ~/.grix/config/agents.json. No lo imprimas en logs ni lo subas a git.`

const traecliConnectorTaskPt = `Conecte este Agent do Grix ao grix-connector desta máquina. Execute os passos em ordem e reporte o resultado ao terminar.

Pré-requisito: Node.js 22.19+ está instalado nesta máquina (o conector precisa dele). Se não estiver, avise-me primeiro — não instale você mesmo.

0) Instalar o CLI oficial do TraeCode (pule esta etapa se já estiver instalado, ou atualize se necessário)
curl -fsSL https://trae.cn/trae-cli/install.sh | bash
Nota: isto não é a IDE TRAE (Trae.app / Trae CN.app) — é uma ferramenta de linha de comando oficial independente. Instalá-la gera o comando traecli em ~/.local/bin; a IDE não é necessária.
Se ao iniciar aparecer uma página de login SSO corporativo, ou pedir /login, avise-me primeiro — não faça login você mesmo, a autenticação precisa ser concluída por um humano. Assim que as credenciais do relay estiverem configuradas, o traecli obtém a configuração do modelo automaticamente, sem uma etapa de login separada.

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

Se nunca conectar, veja o log mais recente em ~/.grix/log/. Na prática costuma ser uma destas três coisas: traecli não está no PATH, o CLI não inicia, ou o api_key foi truncado ao copiar.

Para mais detalhes, veja a seção "Adding an agent to an existing setup" do README do grix-connector, que acompanha o pacote em $(npm root -g)/grix-connector/README.md.

O api_key é um segredo de uso único: escreva-o apenas em ~/.grix/config/agents.json. Não o imprima em logs nem o envie para o git.`

const traecliConnectorTaskRu = `Подключи этого Grix Agent к grix-connector на этой машине. Выполняй шаги по порядку и сообщи о результате по завершении.

Предварительное условие: на этой машине установлен Node.js 22.19+ (нужен коннектору). Если нет, сначала сообщи мне — не устанавливай его сам.

0) Установить официальный CLI TraeCode (пропусти, если уже установлен, или обнови при необходимости)
curl -fsSL https://trae.cn/trae-cli/install.sh | bash
Примечание: это не TRAE IDE (Trae.app / Trae CN.app) — это отдельный официальный инструмент командной строки. Установка создаёт команду traecli в ~/.local/bin; IDE не требуется.
Если при запуске появляется страница входа через корпоративный SSO или запрашивается /login, сначала сообщи мне — не выполняй вход сам, авторизацию должен завершить человек. После настройки учётных данных ретранслятора traecli автоматически подхватывает конфигурацию модели без отдельного шага входа.

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

Если подключения так и нет, посмотри последний лог в ~/.grix/log/. На практике это обычно одно из трёх: traecli нет в PATH, CLI не запускается, либо api_key был обрезан при копировании.

Подробности — в разделе "Adding an agent to an existing setup" README grix-connector, который поставляется вместе с пакетом в $(npm root -g)/grix-connector/README.md.

api_key — это одноразовый секрет: записывай его только в ~/.grix/config/agents.json и больше никуда. Не выводи его в логи и не коммить в git.`

const traecliConnectorTaskAr = `اربط وكيل Grix هذا بـ grix-connector على هذا الجهاز. نفّذ الخطوات بالترتيب وأبلغني بالنتيجة عند الانتهاء.

المتطلب الأساسي: يجب أن يكون Node.js 22.19+ مثبتًا على هذا الجهاز (يحتاجه الموصل). إذا لم يكن كذلك، أخبرني أولاً — لا تثبّته بنفسك.

0) تثبيت CLI الرسمي لـ TraeCode (تخطَّ هذه الخطوة إذا كان مثبتًا بالفعل، أو حدّثه عند الحاجة)
curl -fsSL https://trae.cn/trae-cli/install.sh | bash
ملاحظة: هذه ليست بيئة TRAE IDE (Trae.app / Trae CN.app) — إنها أداة سطر أوامر رسمية مستقلة. يؤدي تثبيتها إلى إنشاء أمر traecli ضمن ~/.local/bin؛ لا حاجة إلى الـ IDE.
إذا ظهرت صفحة تسجيل دخول SSO للمؤسسة عند بدء التشغيل، أو طُلب منك /login، أخبرني أولاً — لا تسجّل الدخول بنفسك، فالتوثيق يجب أن يُنجزه إنسان. بمجرد إعداد بيانات اعتماد الترحيل، يلتقط traecli إعدادات النموذج تلقائيًا دون خطوة تسجيل دخول منفصلة.

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

إذا لم يتصل أبدًا، اقرأ أحدث سجل ضمن ~/.grix/log/. عمليًا يكون السبب أحد ثلاثة: traecli غير موجود في PATH، أو CLI لا يبدأ التشغيل، أو تم اقتطاع api_key أثناء النسخ.

للتفاصيل، راجع قسم "Adding an agent to an existing setup" في ملف README الخاص بـ grix-connector، والذي يأتي مع الحزمة في $(npm root -g)/grix-connector/README.md.

api_key هو سرّ لمرة واحدة: اكتبه فقط في ~/.grix/config/agents.json ولا تكتبه في أي مكان آخر. لا تطبعه في السجلات ولا ترفعه إلى git.`

const traecliConnectorTaskHi = `इस Grix Agent को इस मशीन के grix-connector से कनेक्ट करें। चरण क्रम से चलाएँ और पूरा होने पर परिणाम बताएँ।

पूर्वशर्त: इस मशीन पर Node.js 22.19+ इंस्टॉल होना चाहिए (कनेक्टर को इसकी ज़रूरत है)। अगर नहीं है, तो पहले मुझे बताएँ — इसे खुद इंस्टॉल न करें।

0) आधिकारिक TraeCode CLI इंस्टॉल करें (पहले से इंस्टॉल है तो छोड़ें, या ज़रूरत पड़ने पर अपडेट करें)
curl -fsSL https://trae.cn/trae-cli/install.sh | bash
नोट: यह TRAE IDE (Trae.app / Trae CN.app) नहीं है — यह एक स्वतंत्र आधिकारिक कमांड-लाइन टूल है। इसे इंस्टॉल करने पर ~/.local/bin में traecli कमांड बनता है; IDE की ज़रूरत नहीं।
अगर शुरू होने पर एंटरप्राइज़ खाते का SSO लॉगिन पेज दिखे, या /login माँगा जाए, तो पहले मुझे बताएँ — खुद लॉगिन न करें, प्रमाणीकरण किसी इंसान को ही पूरा करना होगा। रिले क्रेडेंशियल कॉन्फ़िगर होने के बाद traecli बिना अलग लॉगिन चरण के मॉडल कॉन्फ़िगरेशन स्वतः ले लेता है।

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

अगर कभी कनेक्ट न हो, तो ~/.grix/log/ के अंतर्गत सबसे नया लॉग देखें। व्यावहारिक रूप से यह तीन में से एक होता है: traecli PATH में नहीं है, CLI शुरू नहीं होता, या कॉपी करते समय api_key कट गया।

विस्तृत जानकारी के लिए grix-connector के README (इंस्टॉल के बाद $(npm root -g)/grix-connector/README.md पर) के "Adding an agent to an existing setup" सेक्शन को देखें।

api_key एक बार इस्तेमाल होने वाला गुप्त मान है: इसे केवल ~/.grix/config/agents.json में लिखें, कहीं और नहीं। इसे लॉग में न छापें और git में कमिट न करें।`

var traecliConnectorTasksI18n = map[string]string{
	"ja": traecliConnectorTaskJa,
	"ko": traecliConnectorTaskKo,
	"de": traecliConnectorTaskDe,
	"fr": traecliConnectorTaskFr,
	"es": traecliConnectorTaskEs,
	"pt": traecliConnectorTaskPt,
	"ru": traecliConnectorTaskRu,
	"ar": traecliConnectorTaskAr,
	"hi": traecliConnectorTaskHi,
}
