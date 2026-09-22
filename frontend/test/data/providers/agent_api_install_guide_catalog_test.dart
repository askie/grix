import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/providers/agent_service.dart';

void main() {
  group('AgentApiInstallGuideCatalog.enabledTypeSet', () {
    test('uses enabled_client_types, including types without a guide', () {
      final catalog = AgentApiInstallGuideCatalog.fromJson({
        'default_type': 'claude',
        'list': [
          {'type': 'claude'},
        ],
        'enabled_client_types': ['claude', 'Gemini'],
      });
      expect(catalog.enabledTypeSet, {'claude', 'gemini'});
    });

    test('falls back to guide types when the server omits the field', () {
      final catalog = AgentApiInstallGuideCatalog.fromJson({
        'list': [
          {'type': 'hermes'},
        ],
      });
      expect(catalog.enabledClientTypes, isNull);
      expect(catalog.enabledTypeSet, {'hermes'});
    });
  });
}
