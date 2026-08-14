import 'package:flutter_test/flutter_test.dart';
import 'package:grix_admin/modules/gateway/gateway_new_key_dialog.dart';

void main() {
  test('CC Switch Base URL 不带 /v1，按 origin 拼协议前缀', () {
    expect(
      GatewayNewKeyDialog.anthropicBaseUrlForCcSwitch('https://grix.dhf.pub'),
      'https://grix.dhf.pub/anthropic',
    );
    expect(
      GatewayNewKeyDialog.openaiBaseUrlForCcSwitch('https://grix.dhf.pub'),
      'https://grix.dhf.pub/openai',
    );
    expect(
      GatewayNewKeyDialog.anthropicBaseUrlForCcSwitch('https://gb.grix.im'),
      'https://gb.grix.im/anthropic',
    );
  });
}
