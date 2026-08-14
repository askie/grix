/// 绑定目录卡片（agent_open_session）未提交草稿的进程内暂存。
///
/// 卡片选中目录后路径只存在于 widget State 中，一旦聊天列表重建导致
/// State 被重新创建（keepAlive 只能护住滚出屏幕的场景，护不住在屏内的
/// 重建），未提交的路径就会丢失。把草稿按卡片实例 id 暂存在 State 之外，
/// 即可在任意重建后恢复，提交成功后清除。
class ChatOpenSessionDraftStore {
  ChatOpenSessionDraftStore._();

  static final Map<String, String> _draftByCardInstanceId = <String, String>{};

  /// 读取指定卡片实例的未提交草稿；无草稿返回 null。
  static String? read(String cardInstanceId) {
    final key = cardInstanceId.trim();
    if (key.isEmpty) {
      return null;
    }
    return _draftByCardInstanceId[key];
  }

  /// 写入/更新草稿。空字符串视为清除，避免空草稿覆盖初始目录。
  static void write(String cardInstanceId, String value) {
    final key = cardInstanceId.trim();
    if (key.isEmpty) {
      return;
    }
    if (value.isEmpty) {
      _draftByCardInstanceId.remove(key);
      return;
    }
    _draftByCardInstanceId[key] = value;
  }

  /// 清除草稿（提交成功后调用）。
  static void clear(String cardInstanceId) {
    final key = cardInstanceId.trim();
    if (key.isEmpty) {
      return;
    }
    _draftByCardInstanceId.remove(key);
  }
}
