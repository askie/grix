import 'package:get/get.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// 会话草稿索引：为会话列表提供"该会话是否有未发送文字草稿"的轻量查询。
///
/// 数据源与聊天输入框的草稿持久化共用同一套 SharedPreferences key
/// （`chat_draft_{userId}_{sessionId}`）：输入框保存/清除草稿时实时上报，
/// 冷启动后由 [ensureLoaded] 扫描一次补齐历史草稿。
class ChatDraftIndex {
  ChatDraftIndex._();

  static const String _keyPrefix = 'chat_draft_';

  /// 索引内容变化时自增，供列表 Obx 订阅刷新。
  static final RxInt version = 0.obs;

  static final Set<String> _sessionsWithDraft = <String>{};
  static String _loadedUserId = '';
  static Future<void>? _loading;

  static bool hasDraft(String sessionId) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return false;
    }
    return _sessionsWithDraft.contains(sid);
  }

  /// 输入框侧实时上报：草稿非空则登记，为空则摘除。
  static void update({required String sessionId, required bool hasDraft}) {
    final sid = sessionId.trim();
    if (sid.isEmpty) {
      return;
    }
    final changed = hasDraft
        ? _sessionsWithDraft.add(sid)
        : _sessionsWithDraft.remove(sid);
    if (changed) {
      version.value++;
    }
  }

  /// 冷启动/切换用户后从持久层扫描一次全部草稿 key。
  /// 同一用户只扫一次；切换用户时清空重建。
  static Future<void> ensureLoaded(String userId) {
    final uid = userId.trim();
    if (uid.isEmpty || uid == _loadedUserId) {
      return _loading ?? Future<void>.value();
    }
    final load = _loadFromPrefs(uid);
    _loading = load;
    return load;
  }

  static Future<void> _loadFromPrefs(String uid) async {
    final Set<String> scanned;
    try {
      final prefs = await SharedPreferences.getInstance();
      // 前缀匹配靠尾部 `_` 划定用户边界，前提是 userId 不含下划线
      // （当前为纯数字雪花 id）。若未来 userId 允许 `_`，此处会跨用户误匹配。
      final prefix = '$_keyPrefix${uid}_';
      scanned = <String>{};
      for (final key in prefs.getKeys()) {
        if (!key.startsWith(prefix)) {
          continue;
        }
        // 附件/回复草稿使用 `<draftKey>_attach` / `<draftKey>_reply` 派生 key，
        // 只认纯文字草稿 key。
        if (key.endsWith('_attach') || key.endsWith('_reply')) {
          continue;
        }
        final text = prefs.getString(key) ?? '';
        if (text.trim().isEmpty) {
          continue;
        }
        scanned.add(key.substring(prefix.length));
      }
    } catch (_) {
      // 持久层不可用（如测试环境无 mock）时索引保持现状，仅靠实时上报。
      return;
    }
    final isUserSwitch = _loadedUserId.isNotEmpty && _loadedUserId != uid;
    if (isUserSwitch) {
      // 切用户整体重建（此刻新用户尚未打开任何输入框，无实时上报可保留）。
      _sessionsWithDraft.clear();
    }
    // 同用户冷启动用 addAll 合并而非整体替换：扫描期间输入框可能已实时上报。
    _sessionsWithDraft.addAll(scanned);
    _loadedUserId = uid;
    version.value++;
  }

  /// 仅测试用：还原为未加载状态。
  static void resetForTest() {
    _sessionsWithDraft.clear();
    _loadedUserId = '';
    _loading = null;
    version.value++;
  }
}
