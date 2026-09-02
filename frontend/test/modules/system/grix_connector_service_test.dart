import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/system/grix_connector_service.dart';

void main() {
  group('GrixConnectorService.connectorPresenceCommand', () {
    test('uses Windows command discovery under cmd.exe', () {
      expect(
        GrixConnectorService.connectorPresenceCommand(windows: true),
        'where grix-connector',
      );
    });

    test('uses POSIX command discovery under login shell', () {
      expect(
        GrixConnectorService.connectorPresenceCommand(windows: false),
        'command -v grix-connector',
      );
    });
  });

  group('GrixConnectorService.hasUpdate', () {
    test('is false when installed and latest versions are equal', () {
      final service = GrixConnectorService()
        ..isRunning.value = true
        ..installedVersion.value = '3.2.8'
        ..latestVersion.value = '3.2.8';

      expect(service.hasUpdate, isFalse);
    });

    test('normalizes visible semver before comparing', () {
      final service = GrixConnectorService()
        ..isRunning.value = true
        ..installedVersion.value = 'grix-connector/3.2.8+local'
        ..latestVersion.value = 'v3.2.8';

      expect(service.hasUpdate, isFalse);
    });

    test('is true only when the latest semver is newer', () {
      final service = GrixConnectorService()
        ..isRunning.value = true
        ..installedVersion.value = '3.2.8'
        ..latestVersion.value = '3.2.9';

      expect(service.hasUpdate, isTrue);
    });

    // 老版 daemon 的 /healthz 不带 version 字段，版本号就是读不出来的。
    // 它恰恰是需要被这次更新修掉的版本，所以运行中报不出版本 = 提示更新。
    test(
      'prompts an update when a running daemon cannot report its version',
      () {
        final service = GrixConnectorService()
          ..isRunning.value = true
          ..installedVersion.value = ''
          ..latestVersion.value = '3.3.4';

        expect(service.hasUpdate, isTrue);
      },
    );

    test('does not prompt an update when the daemon is not running', () {
      final service = GrixConnectorService()
        ..isRunning.value = false
        ..installedVersion.value = ''
        ..latestVersion.value = '3.3.4';

      expect(service.hasUpdate, isFalse);
    });

    // 回归：CLI 曾把一行启动日志打到 stdout 被当成版本号，于是装着最新版也一直提示更新。
    // 现在版本号只认合法 semver，日志文本永远不会被当成"另一个版本"。
    test('never treats arbitrary text as a comparable version', () {
      final service = GrixConnectorService()
        ..isRunning.value = true
        ..installedVersion.value = '3.3.3'
        ..latestVersion.value = '13:15:59 [sentry] Sentry 错误上报已启用';

      expect(service.hasUpdate, isFalse);
    });

    test('is false when the latest version is unknown', () {
      final service = GrixConnectorService()
        ..isRunning.value = true
        ..installedVersion.value = '3.3.3'
        ..latestVersion.value = '';

      expect(service.hasUpdate, isFalse);
    });
  });

  group('connectorRestartBackoff', () {
    test('第一次失败立即进入 10s 重试节奏', () {
      expect(connectorRestartBackoff(1), const Duration(seconds: 10));
    });

    test('连续失败按指数退避拉长间隔', () {
      expect(connectorRestartBackoff(2), const Duration(seconds: 20));
      expect(connectorRestartBackoff(3), const Duration(seconds: 40));
      expect(connectorRestartBackoff(4), const Duration(seconds: 80));
    });

    test('退避封顶在 5 分钟，长期失败也不会无限拉长', () {
      expect(connectorRestartBackoff(6), const Duration(minutes: 5));
      expect(connectorRestartBackoff(50), const Duration(minutes: 5));
    });
  });
}
