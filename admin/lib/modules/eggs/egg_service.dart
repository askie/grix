import '../../core/network/api_client.dart';

class EggCategory {
  EggCategory({required this.id, required this.code, required this.status, required this.sortOrder, required this.i18n});
  final String id, code, status;
  final int sortOrder;
  final List<Map<String, dynamic>> i18n;
  String get name => i18n.isNotEmpty ? (i18n.first['name'] ?? id).toString() : id;
  bool get isActive => status == 'active';
  factory EggCategory.fromJson(Map<String, dynamic> j) => EggCategory(
    id: (j['id'] ?? '').toString(), code: (j['code'] ?? '').toString(),
    status: (j['status'] ?? '').toString(), sortOrder: (j['sort_order'] as num?)?.toInt() ?? 0,
    i18n: ((j['i18n'] as List?) ?? []).map((e) => (e as Map).cast<String, dynamic>()).toList(),
  );
}

class EggListItem {
  EggListItem({required this.id, required this.categoryId, required this.status, required this.installCount, required this.updatedAt, required this.pinned});
  final String id, categoryId, status;
  final int installCount;
  final int updatedAt;
  final bool pinned;
  factory EggListItem.fromJson(Map<String, dynamic> j) => EggListItem(
    id: (j['id'] ?? '').toString(), categoryId: (j['category_id'] ?? '').toString(),
    status: (j['status'] ?? '').toString(), installCount: (j['install_count'] as num?)?.toInt() ?? 0,
    updatedAt: (j['updated_at'] as num?)?.toInt() ?? 0,
    pinned: (j['pinned'] as bool?) ?? false,
  );
}

class EggDetail {
  EggDetail({required this.id, required this.categoryId, required this.color, required this.emoji, required this.status, required this.installCount, required this.pinned, required this.i18n});
  final String id, categoryId, color, emoji, status;
  final int installCount;
  final bool pinned;
  final List<Map<String, dynamic>> i18n;
  String get name => i18n.isNotEmpty ? (i18n.first['name'] ?? id).toString() : id;
  factory EggDetail.fromJson(Map<String, dynamic> j) => EggDetail(
    id: (j['id'] ?? '').toString(), categoryId: (j['category_id'] ?? '').toString(),
    color: (j['color'] ?? '').toString(), emoji: (j['emoji'] ?? '').toString(),
    status: (j['status'] ?? '').toString(), installCount: (j['install_count'] as num?)?.toInt() ?? 0,
    pinned: (j['pinned'] as bool?) ?? false,
    i18n: ((j['i18n'] as List?) ?? []).map((e) => (e as Map).cast<String, dynamic>()).toList(),
  );
}

class EggVersion {
  EggVersion({required this.eggId, required this.version, required this.personaZipUrl, required this.skillZipUrl, required this.publishedAt, required this.i18n});
  final String eggId, personaZipUrl, skillZipUrl;
  final int version, publishedAt;
  final List<Map<String, dynamic>> i18n;
  String get versionDesc => i18n.isNotEmpty ? (i18n.first['version_desc'] ?? '').toString() : '';
  factory EggVersion.fromJson(Map<String, dynamic> j) => EggVersion(
    eggId: (j['egg_id'] ?? '').toString(), version: (j['version'] as num?)?.toInt() ?? 0,
    personaZipUrl: (j['persona_zip_url'] ?? '').toString(), skillZipUrl: (j['skill_zip_url'] ?? '').toString(),
    publishedAt: (j['published_at'] as num?)?.toInt() ?? 0,
    i18n: ((j['i18n'] as List?) ?? []).map((e) => (e as Map).cast<String, dynamic>()).toList(),
  );
}

class EggService {
  // 分类
  static Future<List<EggCategory>> listCategories() async {
    final data = await ApiClient.instance.get('/eggs/categories');
    return ((data as Map)['categories'] as List? ?? []).map((e) => EggCategory.fromJson((e as Map).cast<String, dynamic>())).toList();
  }
  static Future<void> createCategory(Map<String, dynamic> body) => ApiClient.instance.post('/eggs/categories', data: body);
  static Future<void> updateCategory(String id, Map<String, dynamic> body) => ApiClient.instance.put('/eggs/categories/$id', data: body);
  static Future<void> updateCategoryStatus(String id, String status) => ApiClient.instance.post('/eggs/categories/$id/status', data: {'status': status});

  // 虾蛋
  static Future<({List<EggListItem> list, int total})> listEggs({String? status, String? categoryId, String? keyword, int page = 1, int pageSize = 20}) async {
    final data = await ApiClient.instance.get('/eggs', query: {
      if (status != null && status.isNotEmpty) 'status': status,
      if (categoryId != null && categoryId.isNotEmpty) 'category_id': categoryId,
      if (keyword != null && keyword.isNotEmpty) 'q': keyword,
      'page': page, 'page_size': pageSize,
    });
    final m = (data as Map).cast<String, dynamic>();
    return (list: ((m['list'] as List?) ?? []).map((e) => EggListItem.fromJson((e as Map).cast<String, dynamic>())).toList(), total: (m['total'] as num?)?.toInt() ?? 0);
  }
  static Future<EggDetail> getEgg(String id) async {
    final data = await ApiClient.instance.get('/eggs/$id');
    return EggDetail.fromJson(((data as Map)['egg'] as Map).cast<String, dynamic>());
  }
  static Future<void> createEgg(Map<String, dynamic> body) => ApiClient.instance.post('/eggs', data: body);
  static Future<void> updateEgg(String id, Map<String, dynamic> body) => ApiClient.instance.put('/eggs/$id', data: body);
  static Future<void> updateEggStatus(String id, String status, {String reason = ''}) => ApiClient.instance.post('/eggs/$id/status', data: {'status': status, 'reason': reason});
  static Future<void> setPinned(String id, bool pinned) => ApiClient.instance.post('/eggs/$id/pin', data: {'pinned': pinned});

  // 版本
  static Future<List<EggVersion>> listVersions(String eggId) async {
    final data = await ApiClient.instance.get('/eggs/$eggId/versions');
    return ((data as Map)['versions'] as List? ?? []).map((e) => EggVersion.fromJson((e as Map).cast<String, dynamic>())).toList();
  }
  static Future<Map<String, dynamic>> presign(String eggId, String filename) async {
    final data = await ApiClient.instance.post('/eggs/$eggId/versions/presign', data: {'filename': filename});
    return (data as Map).cast<String, dynamic>();
  }
  static Future<void> createVersion(String eggId, Map<String, dynamic> body) => ApiClient.instance.post('/eggs/$eggId/versions', data: body);
  static Future<void> updateVersion(String eggId, int version, Map<String, dynamic> body) => ApiClient.instance.put('/eggs/$eggId/versions/$version', data: body);
}
