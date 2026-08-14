import 'dart:async';

import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../app/theme/app_palette.dart';
import '../../core/network/page_result.dart';
import '../../shared/widgets/async_view.dart';
import '../../shared/widgets/paginator.dart';
import 'admin_user_item.dart';
import 'user_service.dart';

/// 选择模式：单选 / 多选。
enum UserPickerMode { single, multiple }

/// 用户数据加载器。
///
/// 默认从全量用户库分页搜索（[UserService.list]），调用方也可以注入
/// 自定义实现，例如"列出某 feature gate 白名单内的用户"。
typedef UserPickerLoader =
    Future<PageResult<AdminUserItem>> Function({
      String? query,
      int page,
      int pageSize,
    });

/// 通用用户选择对话框。
///
/// 提供搜索 + 分页 + 单/多选 + 已选预览的统一交互；数据源通过 [loader]
/// 注入，可在任意需要"从用户列表里挑人"的场景复用。
///
/// 使用示例：
/// ```dart
/// final picked = await UserPickerDialog.show(
///   title: '选择白名单用户',
///   mode: UserPickerMode.multiple,
/// );
/// ```
/// 取消返回 `null`，确认返回选中的用户列表。
class UserPickerDialog extends StatefulWidget {
  const UserPickerDialog({
    super.key,
    required this.title,
    this.mode = UserPickerMode.multiple,
    this.confirmText = '确定',
    this.cancelText = '取消',
    this.searchHint = '搜索 ID / 账号 / 昵称 / 邮箱',
    this.initialSelected = const [],
    this.loader,
    this.emptyText = '没有符合条件的用户',
  });

  /// 弹窗标题。
  final String title;

  /// 选择模式。
  final UserPickerMode mode;

  /// 确认按钮文案。
  final String confirmText;

  /// 取消按钮文案。
  final String cancelText;

  /// 搜索框占位符。
  final String searchHint;

  /// 初始已选用户。
  final List<AdminUserItem> initialSelected;

  /// 数据加载器；默认走 [UserService.list]。
  final UserPickerLoader? loader;

  /// 空列表文案。
  final String emptyText;

  /// 静态调用入口。返回选中用户；用户取消则返回 `null`。
  static Future<List<AdminUserItem>?> show({
    required String title,
    UserPickerMode mode = UserPickerMode.multiple,
    String confirmText = '确定',
    String cancelText = '取消',
    String searchHint = '搜索 ID / 账号 / 昵称 / 邮箱',
    List<AdminUserItem> initialSelected = const [],
    UserPickerLoader? loader,
    String emptyText = '没有符合条件的用户',
  }) {
    return Get.dialog<List<AdminUserItem>>(
      UserPickerDialog(
        title: title,
        mode: mode,
        confirmText: confirmText,
        cancelText: cancelText,
        searchHint: searchHint,
        initialSelected: initialSelected,
        loader: loader,
        emptyText: emptyText,
      ),
      barrierDismissible: false,
    );
  }

  @override
  State<UserPickerDialog> createState() => _UserPickerDialogState();
}

class _UserPickerDialogState extends State<UserPickerDialog> {
  final TextEditingController _searchCtrl = TextEditingController();

  String _keyword = '';
  int _page = 1;
  int _pageSize = 20;
  int _total = 0;
  bool _loading = false;
  String? _error;
  List<AdminUserItem> _items = const [];

  /// 已选用户表，按 id 索引，便于快速判断与去重。
  final Map<String, AdminUserItem> _selected = {};

  Timer? _searchDebounce;

  /// 用于丢弃过期请求结果。
  int _requestSeq = 0;

  UserPickerLoader get _loader =>
      widget.loader ?? _defaultLoader;

  Future<PageResult<AdminUserItem>> _defaultLoader({
    String? query,
    int page = 1,
    int pageSize = 20,
  }) {
    return UserService.list(query: query, page: page, pageSize: pageSize);
  }

  @override
  void initState() {
    super.initState();
    for (final u in widget.initialSelected) {
      _selected[u.id] = u;
    }
    _load();
  }

  @override
  void dispose() {
    _searchDebounce?.cancel();
    _searchCtrl.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    final seq = ++_requestSeq;
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final result = await _loader(
        query: _keyword,
        page: _page,
        pageSize: _pageSize,
      );
      if (!mounted || seq != _requestSeq) return;
      setState(() {
        _items = result.items;
        _total = result.total;
        _page = result.page;
        _pageSize = result.pageSize;
      });
    } catch (e) {
      if (!mounted || seq != _requestSeq) return;
      setState(() => _error = e.toString());
    } finally {
      if (mounted && seq == _requestSeq) {
        setState(() => _loading = false);
      }
    }
  }

  void _onKeywordChanged(String value) {
    _searchDebounce?.cancel();
    _searchDebounce = Timer(const Duration(milliseconds: 300), () {
      _applyKeyword(value);
    });
  }

  void _onSubmitSearch(String value) {
    _searchDebounce?.cancel();
    _applyKeyword(value);
  }

  void _applyKeyword(String value) {
    final next = value.trim();
    if (next == _keyword) return;
    _keyword = next;
    _page = 1;
    _load();
  }

  void _clearKeyword() {
    _searchDebounce?.cancel();
    _searchCtrl.clear();
    if (_keyword.isEmpty) return;
    _keyword = '';
    _page = 1;
    _load();
  }

  void _toggle(AdminUserItem user) {
    setState(() {
      if (widget.mode == UserPickerMode.single) {
        _selected
          ..clear()
          ..[user.id] = user;
      } else {
        if (_selected.containsKey(user.id)) {
          _selected.remove(user.id);
        } else {
          _selected[user.id] = user;
        }
      }
    });
  }

  void _removeSelected(String id) {
    setState(() => _selected.remove(id));
  }

  void _confirm() {
    Get.back<List<AdminUserItem>>(result: _selected.values.toList());
  }

  void _cancel() {
    Get.back<List<AdminUserItem>>(result: null);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final media = MediaQuery.of(context);
    const inset = 24.0;
    // Dialog 自身会预留 inset，内部宽高需要相应减去；并设最小宽度避免极窄屏挤压。
    final maxW = (media.size.width - inset * 2).clamp(280.0, 560.0);
    final maxH = (media.size.height - inset * 2).clamp(360.0, 680.0);

    return Dialog(
      insetPadding: const EdgeInsets.all(inset),
      child: SizedBox(
        width: maxW,
        height: maxH,
        child: Column(
          children: [
            _buildHeader(theme),
            const Divider(height: 1),
            _buildSearchBar(),
            Expanded(child: _buildList()),
            const Divider(height: 1),
            _buildPaginator(),
            if (widget.mode == UserPickerMode.multiple) _buildSelectedBar(theme),
            _buildFooter(theme),
          ],
        ),
      ),
    );
  }

  Widget _buildHeader(ThemeData theme) {
    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 16, 12, 12),
      child: Row(
        children: [
          Expanded(
            child: Text(widget.title, style: theme.textTheme.titleMedium),
          ),
          IconButton(
            tooltip: '关闭',
            onPressed: _cancel,
            icon: const Icon(Icons.close),
          ),
        ],
      ),
    );
  }

  Widget _buildSearchBar() {
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 12, 16, 8),
      child: ValueListenableBuilder<TextEditingValue>(
        valueListenable: _searchCtrl,
        builder: (context, value, _) {
          return TextField(
            controller: _searchCtrl,
            decoration: InputDecoration(
              hintText: widget.searchHint,
              prefixIcon: const Icon(Icons.search),
              isDense: true,
              suffixIcon: value.text.isEmpty
                  ? null
                  : IconButton(
                      tooltip: '清除',
                      icon: const Icon(Icons.close, size: 18),
                      onPressed: _clearKeyword,
                    ),
            ),
            onChanged: _onKeywordChanged,
            onSubmitted: _onSubmitSearch,
            textInputAction: TextInputAction.search,
          );
        },
      ),
    );
  }

  Widget _buildList() {
    return AsyncView(
      loading: _loading,
      error: _error,
      isEmpty: _items.isEmpty,
      onRetry: _load,
      emptyText: widget.emptyText,
      builder: (_) => ListView.builder(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
        itemCount: _items.length,
        itemBuilder: (context, index) => _buildUserTile(_items[index]),
      ),
    );
  }

  Widget _buildPaginator() {
    if (_total <= 0) return const SizedBox.shrink();
    return Paginator(
      page: _page,
      pageSize: _pageSize,
      total: _total,
      onPageChanged: (p) {
        _page = p;
        _load();
      },
    );
  }

  Widget _buildUserTile(AdminUserItem user) {
    final selected = _selected.containsKey(user.id);
    final subtitle = [
      'ID ${user.id}',
      '@${user.username}',
      if (user.email.isNotEmpty) user.email,
    ].join(' · ');

    Widget? trailing;
    if (widget.mode == UserPickerMode.multiple) {
      trailing = Checkbox(value: selected, onChanged: (_) => _toggle(user));
    } else if (selected) {
      trailing = const Icon(Icons.check, color: AppPalette.brand);
    }

    return ListTile(
      dense: true,
      selected: selected,
      onTap: () => _toggle(user),
      leading: CircleAvatar(
        backgroundColor: AppPalette.brandSoft,
        child: Text(
          _avatarLetter(user),
          style: const TextStyle(
            color: AppPalette.brand,
            fontWeight: FontWeight.w600,
          ),
        ),
      ),
      title: Text(
        user.displayName,
        maxLines: 1,
        overflow: TextOverflow.ellipsis,
      ),
      subtitle: Text(subtitle, maxLines: 1, overflow: TextOverflow.ellipsis),
      trailing: trailing,
    );
  }

  String _avatarLetter(AdminUserItem user) {
    final src = user.displayName.isNotEmpty ? user.displayName : user.username;
    if (src.isEmpty) return '#';
    return src.characters.first.toUpperCase();
  }

  Widget _buildSelectedBar(ThemeData theme) {
    if (_selected.isEmpty) {
      return Padding(
        padding: const EdgeInsets.fromLTRB(16, 10, 16, 6),
        child: Row(
          children: [
            Text(
              '已选 0 人',
              style: theme.textTheme.bodySmall?.copyWith(
                color: AppPalette.textTertiary,
              ),
            ),
          ],
        ),
      );
    }
    final users = _selected.values.toList();
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 10, 16, 6),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            '已选 ${users.length} 人',
            style: theme.textTheme.bodySmall?.copyWith(
              color: AppPalette.textSecondary,
            ),
          ),
          const SizedBox(height: 6),
          SizedBox(
            height: 36,
            child: ListView.separated(
              scrollDirection: Axis.horizontal,
              itemCount: users.length,
              separatorBuilder: (_, _) => const SizedBox(width: 6),
              itemBuilder: (_, i) {
                final u = users[i];
                return InputChip(
                  label: Text(u.displayName, overflow: TextOverflow.ellipsis),
                  onDeleted: () => _removeSelected(u.id),
                );
              },
            ),
          ),
        ],
      ),
    );
  }

  Widget _buildFooter(ThemeData theme) {
    final canConfirm = _selected.isNotEmpty;
    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.end,
        children: [
          TextButton(onPressed: _cancel, child: Text(widget.cancelText)),
          const SizedBox(width: 8),
          FilledButton(
            onPressed: canConfirm ? _confirm : null,
            child: Text(widget.confirmText),
          ),
        ],
      ),
    );
  }
}
