package service

// The OpenClaw plugin task in all app languages. zh/en are the reference texts
// (moved verbatim from the catalog definition when the language matrix was
// introduced). No %s slots — {{...}} placeholders are substituted client-side.

const openclawTaskZh = `把这个 Grix Agent 接入本机的 OpenClaw。请按顺序执行，完成后回报结果。

前提：本机已安装 OpenClaw（>= 2026.4.8）。如果没有，先告诉我，不要自行安装。

1) 安装插件
openclaw plugins install grix-connector

2) 写入 Grix 渠道配置
自己取一个账号 ID（小写字母、数字、连字符，例如 my-agent），下面用 <account-id> 表示。
⛔ 必须用 openclaw config set 写入，禁止手改 openclaw.json。

openclaw config set channels.grix.accounts.<account-id> '{"name":"{{agent_name}}","enabled":true,"wsUrl":"{{api_endpoint}}","agentId":"{{agent_id}}","apiKey":"{{api_key}}"}' --strict-json

⚠️ 字段名是驼峰的 wsUrl / agentId / apiKey，不是下划线形式。
如果 channels.grix.enabled 曾被显式关闭，再执行：
openclaw config set channels.grix.enabled true --strict-json

3) 校验配置
openclaw config validate
openclaw grix doctor
在 doctor 输出里确认该账号 configured 与 enabled 均为 true。
（openclaw grix 下只有 doctor 一个子命令，不存在 add / set 之类的命令，不要去猜。）

4) 验证接入（必做）
在 Grix 里给名为 {{agent_name}} 的 Agent 发一条消息，确认回复确实来自它本人。
只有在配置校验通过、但消息不通时，才执行一次 openclaw gateway restart，然后重新发消息验证。

⚠️ apiKey 是一次性凭据，只写入 OpenClaw 配置，不要打印到日志、不要提交到 git。`

const openclawTaskEn = `Connect this Grix Agent to OpenClaw on this machine. Follow the steps in order and report back when done.

Prerequisite: OpenClaw (>= 2026.4.8) is installed on this machine. If it is not, tell me first — do not install it yourself.

1) Install the plugin
openclaw plugins install grix-connector

2) Write the Grix channel config
Pick an account id (lowercase letters, digits, hyphens — e.g. my-agent); it is written as <account-id> below.
Write it with openclaw config set. Never hand-edit openclaw.json.

openclaw config set channels.grix.accounts.<account-id> '{"name":"{{agent_name}}","enabled":true,"wsUrl":"{{api_endpoint}}","agentId":"{{agent_id}}","apiKey":"{{api_key}}"}' --strict-json

The field names are camelCase — wsUrl / agentId / apiKey — not the snake_case form used by the connector daemon.
If channels.grix.enabled was explicitly turned off before, also run:
openclaw config set channels.grix.enabled true --strict-json

3) Check the config
openclaw config validate
openclaw grix doctor
Confirm the account shows configured: true and enabled: true.
(openclaw grix only exposes doctor — there is no add / set subcommand, do not guess one.)

4) Verify (required)
Send a message to the Agent named {{agent_name}} in Grix and confirm the reply really comes from it.
Only if the config checks out but messages do not route, run openclaw gateway restart once, then send the message again.

The apiKey is a one-time secret: write it into the OpenClaw config and nowhere else. Do not echo it into logs or commit it to git.`

const openclawTaskJa = `この Grix Agent をこのマシンの OpenClaw に接続してください。手順どおりに実行し、完了したら結果を報告してください。

前提：このマシンに OpenClaw（>= 2026.4.8）がインストール済みであること。なければ、自分でインストールせず、まず私に知らせてください。

1) プラグインをインストール
openclaw plugins install grix-connector

2) Grix チャネル設定を書き込む
アカウント ID を自分で決める（小文字英字・数字・ハイフン。例：my-agent）。以下では <account-id> と表記する。
⛔ 必ず openclaw config set で書き込むこと。openclaw.json の手編集は禁止。

openclaw config set channels.grix.accounts.<account-id> '{"name":"{{agent_name}}","enabled":true,"wsUrl":"{{api_endpoint}}","agentId":"{{agent_id}}","apiKey":"{{api_key}}"}' --strict-json

⚠️ フィールド名はキャメルケースの wsUrl / agentId / apiKey。アンダースコア形式ではない。
channels.grix.enabled が明示的に無効化されていた場合は、続けて実行：
openclaw config set channels.grix.enabled true --strict-json

3) 設定を検証
openclaw config validate
openclaw grix doctor
doctor の出力で、このアカウントの configured と enabled が両方 true であることを確認する。
（openclaw grix にあるサブコマンドは doctor だけ。add / set などは存在しないので、推測で実行しないこと。）

4) 接続を検証（必須）
Grix で {{agent_name}} という名前の Agent にメッセージを送り、返信が本当に本人から来ることを確認する。
設定検証は通るのにメッセージが届かない場合に限り、openclaw gateway restart を一度だけ実行し、再度メッセージで確認する。

⚠️ apiKey は一度きりの資格情報。OpenClaw の設定以外には書かず、ログへの出力や git へのコミットは禁止。`

const openclawTaskKo = `이 Grix Agent를 이 컴퓨터의 OpenClaw에 연결하세요. 순서대로 실행하고 완료되면 결과를 보고하세요.

전제 조건: 이 컴퓨터에 OpenClaw(>= 2026.4.8)가 설치되어 있어야 합니다. 없으면 직접 설치하지 말고 먼저 알려주세요.

1) 플러그인 설치
openclaw plugins install grix-connector

2) Grix 채널 설정 쓰기
계정 ID를 직접 정한다（소문자, 숫자, 하이픈. 예: my-agent）. 아래에서는 <account-id>로 표기한다.
⛔ 반드시 openclaw config set으로 써야 하며 openclaw.json을 손으로 고치는 것은 금지.

openclaw config set channels.grix.accounts.<account-id> '{"name":"{{agent_name}}","enabled":true,"wsUrl":"{{api_endpoint}}","agentId":"{{agent_id}}","apiKey":"{{api_key}}"}' --strict-json

⚠️ 필드명은 카멜케이스인 wsUrl / agentId / apiKey이며 언더스코어 형식이 아니다.
channels.grix.enabled가 명시적으로 꺼져 있었다면 이어서 실행:
openclaw config set channels.grix.enabled true --strict-json

3) 설정 검증
openclaw config validate
openclaw grix doctor
doctor 출력에서 해당 계정의 configured와 enabled가 모두 true인지 확인한다.
（openclaw grix의 하위 명령은 doctor 하나뿐이다. add / set 같은 명령은 없으니 추측해서 실행하지 말 것.）

4) 연결 검증（필수）
Grix에서 {{agent_name}}이라는 Agent에게 메시지를 보내 답장이 정말 그 Agent에게서 오는지 확인한다.
설정 검증은 통과했는데 메시지가 오가지 않을 때만 openclaw gateway restart를 한 번 실행하고 다시 메시지로 확인한다.

⚠️ apiKey는 일회성 자격 증명이다. OpenClaw 설정 외에는 쓰지 말고, 로그 출력이나 git 커밋도 금지.`

const openclawTaskDe = `Verbinde diesen Grix Agent mit OpenClaw auf dieser Maschine. Führe die Schritte der Reihe nach aus und melde dich mit dem Ergebnis zurück.

Voraussetzung: OpenClaw (>= 2026.4.8) ist auf dieser Maschine installiert. Falls nicht, sag mir zuerst Bescheid — installiere es nicht selbst.

1) Plugin installieren
openclaw plugins install grix-connector

2) Die Grix-Kanal-Konfiguration schreiben
Wähle eine Account-ID (Kleinbuchstaben, Ziffern, Bindestriche — z. B. my-agent); unten steht sie als <account-id>.
Schreibe sie mit openclaw config set. Niemals openclaw.json von Hand editieren.

openclaw config set channels.grix.accounts.<account-id> '{"name":"{{agent_name}}","enabled":true,"wsUrl":"{{api_endpoint}}","agentId":"{{agent_id}}","apiKey":"{{api_key}}"}' --strict-json

Die Feldnamen sind camelCase — wsUrl / agentId / apiKey — nicht die snake_case-Form des Connector-Daemons.
Falls channels.grix.enabled zuvor explizit abgeschaltet wurde, zusätzlich ausführen:
openclaw config set channels.grix.enabled true --strict-json

3) Konfiguration prüfen
openclaw config validate
openclaw grix doctor
Bestätige, dass der Account configured: true und enabled: true zeigt.
(openclaw grix kennt nur doctor — es gibt kein add / set, keines erraten.)

4) Verifizieren (Pflicht)
Schicke in Grix eine Nachricht an den Agent namens {{agent_name}} und bestätige, dass die Antwort wirklich von ihm kommt.
Nur wenn die Konfiguration in Ordnung ist, aber keine Nachrichten durchkommen, einmal openclaw gateway restart ausführen und die Nachricht erneut senden.

Der apiKey ist ein Einmal-Geheimnis: nur in die OpenClaw-Konfiguration schreiben und nirgendwo sonst. Nicht in Logs ausgeben, nicht in git committen.`

const openclawTaskFr = `Connecte cet Agent Grix à OpenClaw sur cette machine. Exécute les étapes dans l'ordre et rends compte du résultat une fois terminé.

Prérequis : OpenClaw (>= 2026.4.8) est installé sur cette machine. Sinon, préviens-moi d'abord — ne l'installe pas toi-même.

1) Installer le plugin
openclaw plugins install grix-connector

2) Écrire la config du canal Grix
Choisis un identifiant de compte (lettres minuscules, chiffres, tirets — p. ex. my-agent) ; il est noté <account-id> ci-dessous.
Écris-le avec openclaw config set. Ne jamais éditer openclaw.json à la main.

openclaw config set channels.grix.accounts.<account-id> '{"name":"{{agent_name}}","enabled":true,"wsUrl":"{{api_endpoint}}","agentId":"{{agent_id}}","apiKey":"{{api_key}}"}' --strict-json

Les noms de champs sont en camelCase — wsUrl / agentId / apiKey — pas la forme snake_case du daemon connector.
Si channels.grix.enabled avait été explicitement désactivé, exécute aussi :
openclaw config set channels.grix.enabled true --strict-json

3) Vérifier la config
openclaw config validate
openclaw grix doctor
Confirme que le compte affiche configured: true et enabled: true.
(openclaw grix n'expose que doctor — il n'y a pas de sous-commande add / set, n'en invente pas.)

4) Vérifier la connexion (obligatoire)
Envoie un message dans Grix à l'Agent nommé {{agent_name}} et confirme que la réponse vient bien de lui.
Seulement si la config est bonne mais que les messages ne passent pas, exécute une fois openclaw gateway restart, puis renvoie le message.

L'apiKey est un secret à usage unique : ne l'écrire que dans la config OpenClaw et nulle part ailleurs. Ne pas l'afficher dans les logs, ne pas le committer dans git.`

const openclawTaskEs = `Conecta este Agent de Grix a OpenClaw en esta máquina. Ejecuta los pasos en orden e informa del resultado al terminar.

Requisito previo: OpenClaw (>= 2026.4.8) está instalado en esta máquina. Si no lo está, avísame primero — no lo instales por tu cuenta.

1) Instalar el plugin
openclaw plugins install grix-connector

2) Escribir la config del canal Grix
Elige un id de cuenta (minúsculas, dígitos, guiones — p. ej. my-agent); abajo se escribe como <account-id>.
Escríbelo con openclaw config set. Nunca edites openclaw.json a mano.

openclaw config set channels.grix.accounts.<account-id> '{"name":"{{agent_name}}","enabled":true,"wsUrl":"{{api_endpoint}}","agentId":"{{agent_id}}","apiKey":"{{api_key}}"}' --strict-json

Los nombres de campo van en camelCase — wsUrl / agentId / apiKey — no en la forma snake_case del daemon del connector.
Si channels.grix.enabled se desactivó explícitamente antes, ejecuta también:
openclaw config set channels.grix.enabled true --strict-json

3) Comprobar la config
openclaw config validate
openclaw grix doctor
Confirma que la cuenta muestra configured: true y enabled: true.
(openclaw grix solo expone doctor — no existe add / set, no inventes subcomandos.)

4) Verificar (obligatorio)
Envía un mensaje en Grix al Agent llamado {{agent_name}} y confirma que la respuesta viene realmente de él.
Solo si la config está bien pero los mensajes no llegan, ejecuta una vez openclaw gateway restart y vuelve a enviar el mensaje.

El apiKey es un secreto de un solo uso: escríbelo solo en la config de OpenClaw y en ningún otro sitio. No lo imprimas en logs ni lo subas a git.`

const openclawTaskPt = `Conecte este Agent do Grix ao OpenClaw nesta máquina. Execute os passos em ordem e reporte o resultado ao terminar.

Pré-requisito: OpenClaw (>= 2026.4.8) está instalado nesta máquina. Se não estiver, me avise primeiro — não instale por conta própria.

1) Instalar o plugin
openclaw plugins install grix-connector

2) Escrever a config do canal Grix
Escolha um id de conta (minúsculas, dígitos, hífens — p. ex. my-agent); abaixo ele aparece como <account-id>.
Escreva com openclaw config set. Nunca edite openclaw.json à mão.

openclaw config set channels.grix.accounts.<account-id> '{"name":"{{agent_name}}","enabled":true,"wsUrl":"{{api_endpoint}}","agentId":"{{agent_id}}","apiKey":"{{api_key}}"}' --strict-json

Os nomes dos campos são camelCase — wsUrl / agentId / apiKey — não a forma snake_case usada pelo daemon do connector.
Se channels.grix.enabled tiver sido desativado explicitamente antes, execute também:
openclaw config set channels.grix.enabled true --strict-json

3) Checar a config
openclaw config validate
openclaw grix doctor
Confirme que a conta mostra configured: true e enabled: true.
(openclaw grix só expõe doctor — não existe add / set, não invente subcomandos.)

4) Verificar (obrigatório)
Envie uma mensagem no Grix para o Agent chamado {{agent_name}} e confirme que a resposta vem realmente dele.
Só se a config estiver ok mas as mensagens não fluírem, execute uma vez openclaw gateway restart e envie a mensagem de novo.

O apiKey é um segredo de uso único: escreva-o apenas na config do OpenClaw e em nenhum outro lugar. Não o imprima em logs nem faça commit dele no git.`

const openclawTaskRu = `Подключи этого Grix Agent к OpenClaw на этой машине. Выполняй шаги по порядку и сообщи о результате по завершении.

Предварительное условие: на этой машине установлен OpenClaw (>= 2026.4.8). Если нет — сначала сообщи мне, не устанавливай самостоятельно.

1) Установи плагин
openclaw plugins install grix-connector

2) Запиши конфигурацию канала Grix
Придумай id аккаунта (строчные буквы, цифры, дефисы — например my-agent); ниже он обозначен как <account-id>.
Записывать только через openclaw config set. Никогда не правь openclaw.json вручную.

openclaw config set channels.grix.accounts.<account-id> '{"name":"{{agent_name}}","enabled":true,"wsUrl":"{{api_endpoint}}","agentId":"{{agent_id}}","apiKey":"{{api_key}}"}' --strict-json

Имена полей в camelCase — wsUrl / agentId / apiKey — а не в snake_case, как у демона коннектора.
Если channels.grix.enabled ранее был явно выключен, дополнительно выполни:
openclaw config set channels.grix.enabled true --strict-json

3) Проверь конфигурацию
openclaw config validate
openclaw grix doctor
Убедись, что у аккаунта configured: true и enabled: true.
(У openclaw grix есть только doctor — подкоманд add / set не существует, не выдумывай их.)

4) Проверка подключения (обязательно)
Отправь в Grix сообщение Agent с именем {{agent_name}} и убедись, что ответ действительно приходит от него.
Только если конфигурация в порядке, а сообщения не ходят, один раз выполни openclaw gateway restart и отправь сообщение снова.

apiKey — одноразовый секрет: записывай его только в конфигурацию OpenClaw и никуда больше. Не выводи в логи и не коммить в git.`

const openclawTaskAr = `اربط وكيل Grix هذا بـ OpenClaw على هذا الجهاز. نفّذ الخطوات بالترتيب وأبلغني بالنتيجة عند الانتهاء.

المتطلب المسبق: OpenClaw (>= 2026.4.8) مثبّت على هذا الجهاز. إن لم يكن كذلك فأخبرني أولًا — لا تثبّته بنفسك.

1) ثبّت الإضافة
openclaw plugins install grix-connector

2) اكتب إعدادات قناة Grix
اختر معرّف حساب (أحرف صغيرة وأرقام وشرطات — مثل my-agent)؛ يُكتب أدناه <account-id>.
⛔ يجب الكتابة عبر openclaw config set. يُمنع تعديل openclaw.json يدويًا.

openclaw config set channels.grix.accounts.<account-id> '{"name":"{{agent_name}}","enabled":true,"wsUrl":"{{api_endpoint}}","agentId":"{{agent_id}}","apiKey":"{{api_key}}"}' --strict-json

⚠️ أسماء الحقول بصيغة camelCase — wsUrl / agentId / apiKey — وليست بصيغة الشرطة السفلية.
إن كان channels.grix.enabled قد عُطّل صراحةً من قبل، نفّذ أيضًا:
openclaw config set channels.grix.enabled true --strict-json

3) تحقّق من الإعدادات
openclaw config validate
openclaw grix doctor
تأكد في مخرجات doctor أن الحساب يظهر بـ configured: true و enabled: true.
(لا يوجد تحت openclaw grix سوى الأمر doctor — لا توجد أوامر add / set فلا تخمّنها.)

4) تحقّق من الاتصال (إلزامي)
أرسل في Grix رسالة إلى الوكيل المسمّى {{agent_name}} وتأكد أن الرد يأتي منه فعلًا.
فقط إذا نجح التحقق من الإعدادات لكن الرسائل لا تمر، نفّذ openclaw gateway restart مرة واحدة ثم أعد إرسال الرسالة.

⚠️ apiKey سرّ يُستخدم مرة واحدة: اكتبه في إعدادات OpenClaw فقط ولا مكان آخر. لا تطبعه في السجلات ولا ترفعه إلى git.`

const openclawTaskHi = `इस Grix Agent को इस मशीन के OpenClaw से कनेक्ट करें। चरण क्रम से चलाएँ और पूरा होने पर परिणाम बताएँ।

पूर्व-शर्त: इस मशीन पर OpenClaw (>= 2026.4.8) इंस्टॉल हो। न हो तो पहले मुझे बताएँ — खुद इंस्टॉल न करें।

1) प्लगइन इंस्टॉल करें
openclaw plugins install grix-connector

2) Grix चैनल कॉन्फ़िग लिखें
खुद एक account id चुनें (छोटे अक्षर, अंक, हाइफ़न — जैसे my-agent); नीचे इसे <account-id> लिखा गया है।
⛔ इसे openclaw config set से ही लिखें; openclaw.json को हाथ से न बदलें।

openclaw config set channels.grix.accounts.<account-id> '{"name":"{{agent_name}}","enabled":true,"wsUrl":"{{api_endpoint}}","agentId":"{{agent_id}}","apiKey":"{{api_key}}"}' --strict-json

⚠️ फ़ील्ड नाम camelCase हैं — wsUrl / agentId / apiKey — अंडरस्कोर रूप नहीं।
अगर channels.grix.enabled पहले स्पष्ट रूप से बंद किया गया था, तो यह भी चलाएँ:
openclaw config set channels.grix.enabled true --strict-json

3) कॉन्फ़िग जाँचें
openclaw config validate
openclaw grix doctor
doctor आउटपुट में पुष्टि करें कि उस खाते के configured और enabled दोनों true हैं।
(openclaw grix में केवल doctor उपकमांड है — add / set जैसा कुछ नहीं है, अंदाज़ा न लगाएँ।)

4) कनेक्शन सत्यापित करें (अनिवार्य)
Grix में {{agent_name}} नाम के Agent को संदेश भेजें और पुष्टि करें कि जवाब वाकई उसी से आ रहा है।
कॉन्फ़िग जाँच पास हो लेकिन संदेश न पहुँचे, तभी एक बार openclaw gateway restart चलाएँ और फिर से संदेश भेजकर जाँचें।

⚠️ apiKey एक बार का सीक्रेट है: इसे केवल OpenClaw कॉन्फ़िग में लिखें, कहीं और नहीं। लॉग में न छापें, git में कमिट न करें।`
