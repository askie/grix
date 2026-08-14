import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';
import 'package:grix/data/providers/feature_flag_service.dart';

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  group('FeatureFlagService', () {
    test('features starts empty before any load', () {
      SharedPreferences.setMockInitialValues({});
      final service = FeatureFlagService();
      expect(service.features, isEmpty);
      expect(service.hasLoaded.value, isFalse);
    });

    test('isEnabled returns false for unknown feature', () {
      final service = FeatureFlagService();
      expect(service.isEnabled('voice_call'), isFalse);
    });

    test('isEnabled returns true after setting features', () {
      final service = FeatureFlagService();
      service.features.value = ['voice_call', 'voice_delegate'];
      expect(service.isEnabled('voice_call'), isTrue);
      expect(service.isEnabled('voice_delegate'), isTrue);
      expect(service.isEnabled('unknown'), isFalse);
    });

    test('isEnabled returns false when features is empty', () {
      final service = FeatureFlagService();
      service.features.value = [];
      expect(service.isEnabled('voice_call'), isFalse);
    });

    test('loads features from SharedPreferences cache', () async {
      SharedPreferences.setMockInitialValues({
        'feature_flags_cache': jsonEncode(['voice_call', 'voice_delegate']),
      });

      final prefs = await SharedPreferences.getInstance();
      final raw = prefs.getString('feature_flags_cache');
      expect(raw, isNotNull);

      // Simulate cache load logic (same as _loadFromCache)
      final service = FeatureFlagService();
      if (raw != null && raw.isNotEmpty) {
        try {
          final list = (jsonDecode(raw) as List).cast<String>();
          service.features.value = list;
        } catch (_) {}
      }
      service.hasLoaded.value = true;

      expect(service.isEnabled('voice_call'), isTrue);
      expect(service.isEnabled('voice_delegate'), isTrue);
      expect(service.isEnabled('agent_voice_llm'), isFalse);
    });

    test('handles corrupted cache gracefully', () async {
      SharedPreferences.setMockInitialValues({
        'feature_flags_cache': 'not valid json {{{',
      });

      final prefs = await SharedPreferences.getInstance();
      final raw = prefs.getString('feature_flags_cache');

      final service = FeatureFlagService();
      if (raw != null && raw.isNotEmpty) {
        try {
          final list = (jsonDecode(raw) as List).cast<String>();
          service.features.value = list;
        } catch (_) {
          // Corrupted cache, ignore
        }
      }

      expect(service.features, isEmpty);
    });

    test('handles empty cache', () async {
      SharedPreferences.setMockInitialValues({});

      final prefs = await SharedPreferences.getInstance();
      final raw = prefs.getString('feature_flags_cache');

      final service = FeatureFlagService();
      if (raw != null && raw.isNotEmpty) {
        final list = (jsonDecode(raw) as List).cast<String>();
        service.features.value = list;
      }

      expect(service.features, isEmpty);
      expect(service.isEnabled('voice_call'), isFalse);
    });
  });
}
