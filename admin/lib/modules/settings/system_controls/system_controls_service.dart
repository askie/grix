import '../../../core/network/api_client.dart';
import 'system_controls_models.dart';

class SystemControlsService {
  Future<List<SystemControl>> get() async {
    final data =
        (await ApiClient.instance.get('/settings/system-controls')) as Map;
    return (data['items'] as List)
        .map(
          (item) =>
              SystemControl.fromJson((item as Map).cast<String, dynamic>()),
        )
        .toList();
  }

  Future<SystemControl> update(String key, bool value) async {
    final data = await ApiClient.instance.put(
      '/settings/system-controls/${Uri.encodeComponent(key)}',
      data: {'value': value},
    );
    return SystemControl.fromJson((data as Map).cast<String, dynamic>());
  }
}
