import '../../core/network/api_client.dart';
import 'dashboard_models.dart';

class DashboardService {
  static Future<DashboardStats> stats() async {
    final data = await ApiClient.instance.get('/dashboard/stats');
    return DashboardStats.fromJson((data as Map).cast<String, dynamic>());
  }
}
