import 'package:get/get.dart';

String buildWebhookHelpText(String webhookUrl) {
  final safeURL = webhookUrl.trim();
  return '''${'chat_webhook_help_intro'.tr}

1. ${'chat_webhook_help_step1'.tr}
2. ${'chat_webhook_help_step2'.tr}
3. ${'chat_webhook_help_step3'.tr}

URL:
$safeURL

curl:
curl -X POST '$safeURL' \\
  -H 'Content-Type: application/json' \\
  -d '{
    "content": "hello from webhook",
    "msg_type": "text",
    "client_msg_id": "ext-001"
  }'

JSON:
{
  "content": "hello from webhook",
  "msg_type": "text",
  "client_msg_id": "ext-001"
}
''';
}
