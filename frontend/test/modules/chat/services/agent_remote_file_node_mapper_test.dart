import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/services/agent_remote_file_node_mapper.dart';

void main() {
  group('mapAgentRemoteFileNode', () {
    test('uses id when id is present', () {
      final node = mapAgentRemoteFileNode(<String, dynamic>{
        'id': '/workspace/demo',
        'name': 'demo',
        'is_directory': true,
      });

      expect(node.id, '/workspace/demo');
    });

    test('falls back to path when id is empty', () {
      final node = mapAgentRemoteFileNode(<String, dynamic>{
        'id': '   ',
        'path': '/workspace/from-path',
        'name': 'from-path',
        'is_directory': true,
      });

      expect(node.id, '/workspace/from-path');
    });

    test('falls back to current_path when id and path are empty', () {
      final node = mapAgentRemoteFileNode(<String, dynamic>{
        'id': '',
        'path': '',
        'current_path': '/workspace/from-current-path',
        'name': 'from-current-path',
        'is_directory': true,
      });

      expect(node.id, '/workspace/from-current-path');
    });
  });
}
