package service

// The Hermes plugin task in all app languages. zh/en are the reference texts
// (moved verbatim from the catalog definition when the language matrix was
// introduced). No %s slots — {{...}} placeholders are substituted client-side.

const hermesTaskZh = `把这个 Grix Agent 接入本机的 Hermes。请按顺序执行，完成后回报结果。

前提：本机已安装 hermes CLI。如果没有，先告诉我，不要自行安装。

1) 选定 profile
一个 profile 只能接入一个 Grix Agent。如果默认 profile 已经接了别的 Agent，先新建一个：
hermes profile create <profile>        （名称只能用小写字母、数字、- 和 _）
下面 <profile> 指你选定的 profile；用默认 profile 时，把命令里的 --profile <profile> 整段去掉。

2) 安装插件
hermes --profile <profile> plugins install askie/grix-hermes-python --enable

3) 写入凭据
默认 profile → ~/.hermes/.env
具名 profile → ~/.hermes/profiles/<profile>/.env
把下面三行写进该文件（已有同名变量就替换，其余内容原样保留，不要清空文件）：

GRIX_ENDPOINT={{api_endpoint}}
GRIX_AGENT_ID={{agent_id}}
GRIX_API_KEY={{api_key}}

⚠️ GRIX_ENDPOINT 要连同后面的 ?agent_id=... 一起原样照抄，不要截断。

4) 重启网关
hermes --profile <profile> gateway restart

5) 验证（必做）
hermes --profile <profile> gateway status   → 应显示 running
再看 profile 目录下的 logs/gateway.log，出现形如 "[Grix] Connected to wss://..." 的行即接入成功（grep 时不要区分大小写）；此时 Grix 里名为 {{agent_name}} 的 Agent 应显示已上线。
若日志出现 "no messaging platforms enabled" 或 "grix disabled"，说明插件没启用，检查该 profile 的 config.yaml 里 plugins.enabled 是否包含 grix-hermes。

⚠️ GRIX_API_KEY 是一次性凭据，只写入 .env，不要打印到日志、不要提交到 git。`

const hermesTaskEn = `Connect this Grix Agent to Hermes on this machine. Follow the steps in order and report back when done.

Prerequisite: the hermes CLI is installed on this machine. If it is not, tell me first — do not install it yourself.

1) Pick a profile
One profile can host only one Grix Agent. If the default profile already hosts another one, create a new profile first:
hermes profile create <profile>        (lowercase letters, digits, - and _ only)
<profile> below is the profile you picked; when using the default profile, drop the --profile <profile> part from every command.

2) Install the plugin
hermes --profile <profile> plugins install askie/grix-hermes-python --enable

3) Write the credentials
default profile -> ~/.hermes/.env
named profile   -> ~/.hermes/profiles/<profile>/.env
Put these three lines in that file (replace the keys if they already exist; leave the rest of the file intact):

GRIX_ENDPOINT={{api_endpoint}}
GRIX_AGENT_ID={{agent_id}}
GRIX_API_KEY={{api_key}}

Copy GRIX_ENDPOINT verbatim, including the trailing ?agent_id=... — do not truncate it.

4) Restart the gateway
hermes --profile <profile> gateway restart

5) Verify (required)
hermes --profile <profile> gateway status   -> should report running
Then read logs/gateway.log under the profile directory: a line like "[Grix] Connected to wss://..." means it is in (grep case-insensitively), and the Agent named {{agent_name}} shows up as online in Grix.
If the log says "no messaging platforms enabled" or "grix disabled", the plugin is not enabled — check that plugins.enabled in that profile's config.yaml contains grix-hermes.

The GRIX_API_KEY is a one-time secret: write it into the .env and nowhere else. Do not echo it into logs or commit it to git.`

const hermesTaskJa = `この Grix Agent をこのマシンの Hermes に接続してください。手順どおりに実行し、完了したら結果を報告してください。

前提：このマシンに hermes CLI がインストール済みであること。なければ、自分でインストールせず、まず私に知らせてください。

1) profile を選ぶ
1 つの profile に接続できる Grix Agent は 1 つだけ。デフォルト profile が既に別の Agent を抱えている場合は、先に新しい profile を作る：
hermes profile create <profile>        （名前に使えるのは小文字英字・数字・- と _ のみ）
以下の <profile> は選んだ profile を指す。デフォルト profile を使う場合は、各コマンドから --profile <profile> の部分を丸ごと外す。

2) プラグインをインストール
hermes --profile <profile> plugins install askie/grix-hermes-python --enable

3) 資格情報を書き込む
デフォルト profile → ~/.hermes/.env
名前付き profile → ~/.hermes/profiles/<profile>/.env
次の 3 行をそのファイルに書き込む（同名の変数があれば置き換え、他の内容はそのまま残す。ファイルを空にしないこと）：

GRIX_ENDPOINT={{api_endpoint}}
GRIX_AGENT_ID={{agent_id}}
GRIX_API_KEY={{api_key}}

⚠️ GRIX_ENDPOINT は末尾の ?agent_id=... まで含めてそのまま写すこと。途中で切らない。

4) ゲートウェイを再起動
hermes --profile <profile> gateway restart

5) 検証（必須）
hermes --profile <profile> gateway status   → running と表示されること
続いて profile ディレクトリ配下の logs/gateway.log を確認。"[Grix] Connected to wss://..." のような行があれば接続成功（grep は大文字小文字を区別しない）。このとき Grix 側で {{agent_name}} という Agent がオンライン表示になるはず。
ログに "no messaging platforms enabled" や "grix disabled" が出る場合はプラグインが有効になっていない。その profile の config.yaml の plugins.enabled に grix-hermes が含まれているか確認する。

⚠️ GRIX_API_KEY は一度きりの資格情報。.env 以外には書かず、ログへの出力や git へのコミットは禁止。`

const hermesTaskKo = `이 Grix Agent를 이 컴퓨터의 Hermes에 연결하세요. 순서대로 실행하고 완료되면 결과를 보고하세요.

전제 조건: 이 컴퓨터에 hermes CLI가 설치되어 있어야 합니다. 없으면 직접 설치하지 말고 먼저 알려주세요.

1) profile 선택
하나의 profile에는 Grix Agent 하나만 연결할 수 있다. 기본 profile이 이미 다른 Agent를 쓰고 있다면 먼저 새 profile을 만든다:
hermes profile create <profile>        （이름에는 소문자, 숫자, -, _만 사용 가능）
아래의 <profile>은 선택한 profile을 뜻한다. 기본 profile을 쓸 때는 모든 명령에서 --profile <profile> 부분을 통째로 뺀다.

2) 플러그인 설치
hermes --profile <profile> plugins install askie/grix-hermes-python --enable

3) 자격 증명 쓰기
기본 profile → ~/.hermes/.env
이름 있는 profile → ~/.hermes/profiles/<profile>/.env
아래 세 줄을 그 파일에 쓴다（같은 이름의 변수가 있으면 교체하고, 나머지 내용은 그대로 둔다. 파일을 비우지 말 것）:

GRIX_ENDPOINT={{api_endpoint}}
GRIX_AGENT_ID={{agent_id}}
GRIX_API_KEY={{api_key}}

⚠️ GRIX_ENDPOINT는 뒤의 ?agent_id=...까지 그대로 복사한다. 자르지 말 것.

4) 게이트웨이 재시작
hermes --profile <profile> gateway restart

5) 검증（필수）
hermes --profile <profile> gateway status   → running이어야 한다
이어서 profile 디렉터리의 logs/gateway.log를 본다. "[Grix] Connected to wss://..." 같은 줄이 있으면 연결 성공이다（grep은 대소문자 구분 없이）. 이때 Grix에서 {{agent_name}} Agent가 온라인으로 표시되어야 한다.
로그에 "no messaging platforms enabled"나 "grix disabled"가 보이면 플러그인이 활성화되지 않은 것이다. 해당 profile의 config.yaml에서 plugins.enabled에 grix-hermes가 있는지 확인한다.

⚠️ GRIX_API_KEY는 일회성 자격 증명이다. .env 외에는 쓰지 말고, 로그 출력이나 git 커밋도 금지.`

const hermesTaskDe = `Verbinde diesen Grix Agent mit Hermes auf dieser Maschine. Führe die Schritte der Reihe nach aus und melde dich mit dem Ergebnis zurück.

Voraussetzung: das hermes CLI ist auf dieser Maschine installiert. Falls nicht, sag mir zuerst Bescheid — installiere es nicht selbst.

1) Profil wählen
Ein Profil kann nur einen Grix Agent beherbergen. Beherbergt das Default-Profil schon einen anderen, lege zuerst ein neues an:
hermes profile create <profile>        (nur Kleinbuchstaben, Ziffern, - und _)
<profile> steht unten für das gewählte Profil; beim Default-Profil den Teil --profile <profile> aus jedem Befehl weglassen.

2) Plugin installieren
hermes --profile <profile> plugins install askie/grix-hermes-python --enable

3) Zugangsdaten schreiben
Default-Profil -> ~/.hermes/.env
benanntes Profil -> ~/.hermes/profiles/<profile>/.env
Schreibe diese drei Zeilen in die Datei (vorhandene gleichnamige Schlüssel ersetzen; den Rest der Datei unangetastet lassen):

GRIX_ENDPOINT={{api_endpoint}}
GRIX_AGENT_ID={{agent_id}}
GRIX_API_KEY={{api_key}}

GRIX_ENDPOINT wortwörtlich kopieren, inklusive des abschließenden ?agent_id=... — nicht abschneiden.

4) Gateway neu starten
hermes --profile <profile> gateway restart

5) Verifizieren (Pflicht)
hermes --profile <profile> gateway status   -> sollte running melden
Dann logs/gateway.log im Profilverzeichnis lesen: eine Zeile wie "[Grix] Connected to wss://..." heißt verbunden (grep ohne Groß-/Kleinschreibung), und der Agent namens {{agent_name}} erscheint in Grix als online.
Steht im Log "no messaging platforms enabled" oder "grix disabled", ist das Plugin nicht aktiviert — prüfe, ob plugins.enabled in der config.yaml des Profils grix-hermes enthält.

Der GRIX_API_KEY ist ein Einmal-Geheimnis: nur in die .env schreiben und nirgendwo sonst. Nicht in Logs ausgeben, nicht in git committen.`

const hermesTaskFr = `Connecte cet Agent Grix à Hermes sur cette machine. Exécute les étapes dans l'ordre et rends compte du résultat une fois terminé.

Prérequis : le CLI hermes est installé sur cette machine. Sinon, préviens-moi d'abord — ne l'installe pas toi-même.

1) Choisir un profil
Un profil ne peut héberger qu'un seul Agent Grix. Si le profil par défaut en héberge déjà un autre, crée d'abord un nouveau profil :
hermes profile create <profile>        (lettres minuscules, chiffres, - et _ uniquement)
<profile> ci-dessous désigne le profil choisi ; avec le profil par défaut, retire la partie --profile <profile> de chaque commande.

2) Installer le plugin
hermes --profile <profile> plugins install askie/grix-hermes-python --enable

3) Écrire les identifiants
profil par défaut -> ~/.hermes/.env
profil nommé -> ~/.hermes/profiles/<profile>/.env
Mets ces trois lignes dans ce fichier (remplace les clés si elles existent déjà ; laisse le reste du fichier intact) :

GRIX_ENDPOINT={{api_endpoint}}
GRIX_AGENT_ID={{agent_id}}
GRIX_API_KEY={{api_key}}

Copie GRIX_ENDPOINT tel quel, y compris le ?agent_id=... final — ne le tronque pas.

4) Redémarrer la passerelle
hermes --profile <profile> gateway restart

5) Vérifier (obligatoire)
hermes --profile <profile> gateway status   -> doit afficher running
Lis ensuite logs/gateway.log dans le répertoire du profil : une ligne comme "[Grix] Connected to wss://..." signifie que c'est bon (grep sans tenir compte de la casse), et l'Agent nommé {{agent_name}} apparaît en ligne dans Grix.
Si le log dit "no messaging platforms enabled" ou "grix disabled", le plugin n'est pas activé — vérifie que plugins.enabled dans le config.yaml de ce profil contient grix-hermes.

Le GRIX_API_KEY est un secret à usage unique : ne l'écrire que dans le .env et nulle part ailleurs. Ne pas l'afficher dans les logs, ne pas le committer dans git.`

const hermesTaskEs = `Conecta este Agent de Grix a Hermes en esta máquina. Ejecuta los pasos en orden e informa del resultado al terminar.

Requisito previo: el CLI de hermes está instalado en esta máquina. Si no lo está, avísame primero — no lo instales por tu cuenta.

1) Elegir un perfil
Un perfil solo puede alojar un Agent de Grix. Si el perfil por defecto ya aloja otro, crea antes uno nuevo:
hermes profile create <profile>        (solo minúsculas, dígitos, - y _)
<profile> abajo es el perfil elegido; con el perfil por defecto, quita la parte --profile <profile> de cada comando.

2) Instalar el plugin
hermes --profile <profile> plugins install askie/grix-hermes-python --enable

3) Escribir las credenciales
perfil por defecto -> ~/.hermes/.env
perfil con nombre -> ~/.hermes/profiles/<profile>/.env
Pon estas tres líneas en ese archivo (reemplaza las claves si ya existen; deja el resto del archivo intacto):

GRIX_ENDPOINT={{api_endpoint}}
GRIX_AGENT_ID={{agent_id}}
GRIX_API_KEY={{api_key}}

Copia GRIX_ENDPOINT literal, incluido el ?agent_id=... final — no lo trunques.

4) Reiniciar la pasarela
hermes --profile <profile> gateway restart

5) Verificar (obligatorio)
hermes --profile <profile> gateway status   -> debe mostrar running
Luego lee logs/gateway.log en el directorio del perfil: una línea como "[Grix] Connected to wss://..." significa que está dentro (grep sin distinguir mayúsculas), y el Agent llamado {{agent_name}} aparece en línea en Grix.
Si el log dice "no messaging platforms enabled" o "grix disabled", el plugin no está habilitado — comprueba que plugins.enabled en el config.yaml de ese perfil contiene grix-hermes.

El GRIX_API_KEY es un secreto de un solo uso: escríbelo solo en el .env y en ningún otro sitio. No lo imprimas en logs ni lo subas a git.`

const hermesTaskPt = `Conecte este Agent do Grix ao Hermes nesta máquina. Execute os passos em ordem e reporte o resultado ao terminar.

Pré-requisito: o CLI hermes está instalado nesta máquina. Se não estiver, me avise primeiro — não instale por conta própria.

1) Escolher um perfil
Um perfil só pode hospedar um Agent do Grix. Se o perfil padrão já hospeda outro, crie um novo primeiro:
hermes profile create <profile>        (apenas minúsculas, dígitos, - e _)
<profile> abaixo é o perfil escolhido; com o perfil padrão, remova a parte --profile <profile> de cada comando.

2) Instalar o plugin
hermes --profile <profile> plugins install askie/grix-hermes-python --enable

3) Escrever as credenciais
perfil padrão -> ~/.hermes/.env
perfil nomeado -> ~/.hermes/profiles/<profile>/.env
Coloque estas três linhas nesse arquivo (substitua as chaves se já existirem; deixe o resto do arquivo intacto):

GRIX_ENDPOINT={{api_endpoint}}
GRIX_AGENT_ID={{agent_id}}
GRIX_API_KEY={{api_key}}

Copie GRIX_ENDPOINT literalmente, incluindo o ?agent_id=... final — não o trunque.

4) Reiniciar o gateway
hermes --profile <profile> gateway restart

5) Verificar (obrigatório)
hermes --profile <profile> gateway status   -> deve reportar running
Depois leia logs/gateway.log no diretório do perfil: uma linha como "[Grix] Connected to wss://..." significa que entrou (grep sem diferenciar maiúsculas), e o Agent chamado {{agent_name}} aparece online no Grix.
Se o log disser "no messaging platforms enabled" ou "grix disabled", o plugin não está habilitado — verifique se plugins.enabled no config.yaml desse perfil contém grix-hermes.

O GRIX_API_KEY é um segredo de uso único: escreva-o apenas no .env e em nenhum outro lugar. Não o imprima em logs nem faça commit dele no git.`

const hermesTaskRu = `Подключи этого Grix Agent к Hermes на этой машине. Выполняй шаги по порядку и сообщи о результате по завершении.

Предварительное условие: на этой машине установлен hermes CLI. Если нет — сначала сообщи мне, не устанавливай самостоятельно.

1) Выбери profile
Один profile может принимать только одного Grix Agent. Если профиль по умолчанию уже занят другим — сначала создай новый:
hermes profile create <profile>        (только строчные буквы, цифры, - и _)
Ниже <profile> — выбранный профиль; для профиля по умолчанию убирай из каждой команды часть --profile <profile>.

2) Установи плагин
hermes --profile <profile> plugins install askie/grix-hermes-python --enable

3) Запиши учётные данные
профиль по умолчанию -> ~/.hermes/.env
именованный профиль -> ~/.hermes/profiles/<profile>/.env
Впиши в этот файл три строки ниже (если такие ключи уже есть — замени; остальное содержимое не трогай, файл не очищай):

GRIX_ENDPOINT={{api_endpoint}}
GRIX_AGENT_ID={{agent_id}}
GRIX_API_KEY={{api_key}}

GRIX_ENDPOINT копируй дословно, вместе с хвостом ?agent_id=... — не обрезай.

4) Перезапусти gateway
hermes --profile <profile> gateway restart

5) Проверка (обязательно)
hermes --profile <profile> gateway status   -> должно быть running
Затем посмотри logs/gateway.log в каталоге профиля: строка вида "[Grix] Connected to wss://..." означает успех (grep без учёта регистра); Agent с именем {{agent_name}} в Grix должен показываться онлайн.
Если в логе "no messaging platforms enabled" или "grix disabled" — плагин не включён; проверь, что plugins.enabled в config.yaml этого профиля содержит grix-hermes.

GRIX_API_KEY — одноразовый секрет: записывай его только в .env и никуда больше. Не выводи в логи и не коммить в git.`

const hermesTaskAr = `اربط وكيل Grix هذا بـ Hermes على هذا الجهاز. نفّذ الخطوات بالترتيب وأبلغني بالنتيجة عند الانتهاء.

المتطلب المسبق: hermes CLI مثبّت على هذا الجهاز. إن لم يكن كذلك فأخبرني أولًا — لا تثبّته بنفسك.

1) اختر profile
كل profile يستوعب وكيل Grix واحدًا فقط. إن كان الـ profile الافتراضي يستضيف وكيلًا آخر، أنشئ واحدًا جديدًا أولًا:
hermes profile create <profile>        (أحرف صغيرة وأرقام و - و _ فقط)
‏<profile> أدناه هو الـ profile الذي اخترته؛ عند استخدام الافتراضي احذف الجزء --profile <profile> من كل أمر.

2) ثبّت الإضافة
hermes --profile <profile> plugins install askie/grix-hermes-python --enable

3) اكتب بيانات الاعتماد
الـ profile الافتراضي -> ‎~/.hermes/.env
الـ profile المسمّى -> ‎~/.hermes/profiles/<profile>/.env
ضع هذه الأسطر الثلاثة في ذلك الملف (استبدل المفاتيح إن كانت موجودة؛ واترك بقية الملف كما هي، لا تُفرغه):

GRIX_ENDPOINT={{api_endpoint}}
GRIX_AGENT_ID={{agent_id}}
GRIX_API_KEY={{api_key}}

⚠️ انسخ GRIX_ENDPOINT حرفيًا بما فيه اللاحقة ‎?agent_id=... — لا تبتره.

4) أعد تشغيل البوابة
hermes --profile <profile> gateway restart

5) التحقق (إلزامي)
hermes --profile <profile> gateway status   -> يجب أن يعرض running
ثم اقرأ logs/gateway.log في مجلد الـ profile: سطر مثل "[Grix] Connected to wss://..." يعني نجاح الاتصال (استخدم grep دون تمييز حالة الأحرف)؛ وعندها يظهر الوكيل المسمّى {{agent_name}} متصلًا في Grix.
إن ظهر في السجل "no messaging platforms enabled" أو "grix disabled" فالإضافة غير مفعّلة — تحقق أن plugins.enabled في config.yaml لذلك الـ profile يتضمن grix-hermes.

⚠️ GRIX_API_KEY سرّ يُستخدم مرة واحدة: اكتبه في ‎.env فقط ولا مكان آخر. لا تطبعه في السجلات ولا ترفعه إلى git.`

const hermesTaskHi = `इस Grix Agent को इस मशीन के Hermes से कनेक्ट करें। चरण क्रम से चलाएँ और पूरा होने पर परिणाम बताएँ।

पूर्व-शर्त: इस मशीन पर hermes CLI इंस्टॉल हो। न हो तो पहले मुझे बताएँ — खुद इंस्टॉल न करें।

1) profile चुनें
एक profile में केवल एक Grix Agent जुड़ सकता है। डिफ़ॉल्ट profile में पहले से कोई और Agent हो तो पहले नया बनाएं:
hermes profile create <profile>        (नाम में केवल छोटे अक्षर, अंक, - और _)
नीचे <profile> आपका चुना profile है; डिफ़ॉल्ट profile उपयोग करते समय हर कमांड से --profile <profile> हिस्सा हटा दें।

2) प्लगइन इंस्टॉल करें
hermes --profile <profile> plugins install askie/grix-hermes-python --enable

3) क्रेडेंशियल लिखें
डिफ़ॉल्ट profile -> ~/.hermes/.env
नामित profile -> ~/.hermes/profiles/<profile>/.env
नीचे की तीन पंक्तियाँ उस फ़ाइल में लिखें (समान नाम के वेरिएबल हों तो बदलें; बाकी सामग्री ज्यों की त्यों रहे, फ़ाइल खाली न करें):

GRIX_ENDPOINT={{api_endpoint}}
GRIX_AGENT_ID={{agent_id}}
GRIX_API_KEY={{api_key}}

⚠️ GRIX_ENDPOINT को अंत के ?agent_id=... समेत हूबहू कॉपी करें — काटें नहीं।

4) गेटवे रीस्टार्ट करें
hermes --profile <profile> gateway restart

5) सत्यापन (अनिवार्य)
hermes --profile <profile> gateway status   -> running दिखना चाहिए
फिर profile डायरेक्टरी के logs/gateway.log देखें: "[Grix] Connected to wss://..." जैसी पंक्ति का मतलब कनेक्ट हो गया (grep केस-इनसेंसिटिव करें); तब Grix में {{agent_name}} नाम का Agent ऑनलाइन दिखेगा।
लॉग में "no messaging platforms enabled" या "grix disabled" दिखे तो प्लगइन सक्षम नहीं है — जाँचें कि उस profile की config.yaml में plugins.enabled में grix-hermes शामिल है।

⚠️ GRIX_API_KEY एक बार का सीक्रेट है: इसे केवल .env में लिखें, कहीं और नहीं। लॉग में न छापें, git में कमिट न करें।`
