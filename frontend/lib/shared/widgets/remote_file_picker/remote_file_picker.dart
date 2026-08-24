import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:file_picker/file_picker.dart';
import 'package:flutter/foundation.dart'
    show kIsWeb, defaultTargetPlatform, TargetPlatform;
import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:image_picker/image_picker.dart' as gallery_picker;
import 'package:path/path.dart' as p;
import 'package:url_launcher/url_launcher.dart';

import 'remote_file_dir.dart';
import 'remote_file_download_plan.dart';

import '../../../app/themes/app_theme.dart';
import '../../../data/providers/user_favorite_path_service.dart';
import '../../../modules/text_document/services/text_document_open_service.dart';
import '../../services/remote_file_host_connectivity.dart';
import '../../utils/time_formatter.dart';
import '../../utils/toast_util.dart';
import 'remote_file_picker_controller.dart';
import 'remote_file_picker_model.dart';

export 'remote_file_picker_model.dart';

class RemoteFilePicker extends StatefulWidget {
  const RemoteFilePicker({
    super.key,
    required this.listProvider,
    this.createFolderProvider,
    this.favoriteApi,
    this.selectionMode = RemoteFileSelectionMode.multiple,
    this.pickTarget = RemoteFilePickTarget.files,
    this.allowedExtensions,
    this.showHidden = false,
    this.title,
    this.rootLabel = 'remote_file_picker_root_label',
    this.initialPath,
    this.storageKey,
    this.uploadBaseUrl,
    this.onConfirm,
    this.onCancel,
    this.showActionBar = true,
    this.paddingBottom = 0,
  });

  final RemoteFileListProvider listProvider;
  final RemoteCreateFolderProvider? createFolderProvider;
  final UserFavoritePathService? favoriteApi;
  final RemoteFileSelectionMode selectionMode;
  final RemoteFilePickTarget pickTarget;
  final List<String>? allowedExtensions;
  final bool showHidden;
  final String? title;
  final String rootLabel;
  final String? initialPath;
  final String? storageKey;

  /// HTTP base URL for Tailnet file transfer and text preview
  /// (e.g. http://100.x.x.x:PORT).
  /// When non-null and non-empty, transfer actions and supported text-file
  /// preview buttons are enabled.
  final String? uploadBaseUrl;
  final ValueChanged<RemoteFilePickerResult>? onConfirm;
  final VoidCallback? onCancel;
  final bool showActionBar;
  final double paddingBottom;

  static Future<RemoteFilePickerResult?> show(
    BuildContext context, {
    required RemoteFileListProvider listProvider,
    RemoteCreateFolderProvider? createFolderProvider,
    UserFavoritePathService? favoriteApi,
    RemoteFileSelectionMode selectionMode = RemoteFileSelectionMode.multiple,
    RemoteFilePickTarget pickTarget = RemoteFilePickTarget.files,
    List<String>? allowedExtensions,
    bool showHidden = false,
    String? title,
    String rootLabel = 'remote_file_picker_root_label',
    String? initialPath,
    String? storageKey,
    String? uploadBaseUrl,
  }) {
    if (isDesktopPlatform) {
      return _showAsDialog(
        context,
        listProvider: listProvider,
        createFolderProvider: createFolderProvider,
        favoriteApi: favoriteApi,
        selectionMode: selectionMode,
        pickTarget: pickTarget,
        allowedExtensions: allowedExtensions,
        showHidden: showHidden,
        title: title,
        rootLabel: rootLabel,
        initialPath: initialPath,
        storageKey: storageKey,
        uploadBaseUrl: uploadBaseUrl,
      );
    }
    return _showAsBottomSheet(
      context,
      listProvider: listProvider,
      createFolderProvider: createFolderProvider,
      favoriteApi: favoriteApi,
      selectionMode: selectionMode,
      pickTarget: pickTarget,
      allowedExtensions: allowedExtensions,
      showHidden: showHidden,
      title: title,
      rootLabel: rootLabel,
      initialPath: initialPath,
      storageKey: storageKey,
      uploadBaseUrl: uploadBaseUrl,
    );
  }

  static Future<RemoteFilePickerResult?> _showAsBottomSheet(
    BuildContext context, {
    required RemoteFileListProvider listProvider,
    RemoteCreateFolderProvider? createFolderProvider,
    UserFavoritePathService? favoriteApi,
    RemoteFileSelectionMode selectionMode = RemoteFileSelectionMode.multiple,
    RemoteFilePickTarget pickTarget = RemoteFilePickTarget.files,
    List<String>? allowedExtensions,
    bool showHidden = false,
    String? title,
    String rootLabel = 'remote_file_picker_root_label',
    String? initialPath,
    String? storageKey,
    String? uploadBaseUrl,
  }) {
    return showModalBottomSheet<RemoteFilePickerResult>(
      context: context,
      isScrollControlled: true,
      constraints: const BoxConstraints(maxWidth: 420),
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (_) => _RemoteFilePickerSheetWrapper(
        listProvider: listProvider,
        createFolderProvider: createFolderProvider,
        favoriteApi: favoriteApi,
        selectionMode: selectionMode,
        pickTarget: pickTarget,
        allowedExtensions: allowedExtensions,
        showHidden: showHidden,
        title: title,
        rootLabel: rootLabel,
        initialPath: initialPath,
        storageKey: storageKey,
        uploadBaseUrl: uploadBaseUrl,
      ),
    );
  }

  static Future<RemoteFilePickerResult?> _showAsDialog(
    BuildContext context, {
    required RemoteFileListProvider listProvider,
    RemoteCreateFolderProvider? createFolderProvider,
    UserFavoritePathService? favoriteApi,
    RemoteFileSelectionMode selectionMode = RemoteFileSelectionMode.multiple,
    RemoteFilePickTarget pickTarget = RemoteFilePickTarget.files,
    List<String>? allowedExtensions,
    bool showHidden = false,
    String? title,
    String rootLabel = 'remote_file_picker_root_label',
    String? initialPath,
    String? storageKey,
    String? uploadBaseUrl,
  }) {
    return showDialog<RemoteFilePickerResult>(
      context: context,
      builder: (_) => Dialog(
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
        child: SizedBox(
          width: 520,
          height: 560,
          child: _RemoteFilePickerDialogWrapper(
            listProvider: listProvider,
            createFolderProvider: createFolderProvider,
            favoriteApi: favoriteApi,
            selectionMode: selectionMode,
            pickTarget: pickTarget,
            allowedExtensions: allowedExtensions,
            showHidden: showHidden,
            title: title,
            rootLabel: rootLabel,
            initialPath: initialPath,
            storageKey: storageKey,
            uploadBaseUrl: uploadBaseUrl,
          ),
        ),
      ),
    );
  }

  @override
  State<RemoteFilePicker> createState() => _RemoteFilePickerState();
}

class _RemoteFilePickerState extends State<RemoteFilePicker> {
  late final RemoteFilePickerController _controller;
  List<FavoritePathItem> _favorites = [];
  bool _favoritesLoading = false;
  bool _showFavorites = false;
  final Set<String> _pendingFavoritePaths = {};
  final Set<FavoritePathItem> _selectedFavorites = {};

  /// 收藏夹机器过滤：null 表示未显式选择，默认跟随当前机器；
  /// 显式选某台机器（含空串"未知机器"）后固定为该值。
  String? _favoriteMachineFilter;

  /// 当前生效的收藏机器过滤值。优先用户显式选择，否则跟随当前列表机器。
  String? get _activeFavoriteMachine =>
      _favoriteMachineFilter ?? _controller.currentMachineName;

  /// 当前机器下已收藏的路径集合（用于文件浏览里标星）。
  /// 按当前机器过滤：同一路径在别的机器收藏不影响本机标星；
  /// 当前机器名未知时（如 agent 未提供）以空串归并，正好匹配无机器名的老收藏。
  Set<String> get _favoritePaths {
    final cur = _controller.currentMachineName ?? '';
    return _favorites
        .where((f) => f.machineName == cur)
        .map((e) => e.path)
        .toSet();
  }

  /// 收藏夹按当前生效机器过滤后的可见列表。机器未知时展示全部。
  List<FavoritePathItem> get _visibleFavorites {
    final m = _activeFavoriteMachine;
    if (m == null) return _favorites;
    return _favorites.where((f) => f.machineName == m).toList();
  }

  /// 收藏里出现过的所有机器名（含当前机器，即使其下暂无收藏），用于选择 sheet。
  List<String> get _favoriteMachines {
    final set = <String>{};
    for (final f in _favorites) {
      set.add(f.machineName);
    }
    final cur = _controller.currentMachineName;
    if (cur != null && cur.isNotEmpty) set.add(cur);
    final list = set.toList()..sort();
    return list;
  }

  String _machineDisplayName(String machine) =>
      machine.isEmpty ? 'remote_file_picker_machine_unknown'.tr : machine;

  /// 覆盖 ping → 选择文件 → 上传 整个流程，用于锁定上传按钮防重复点击。
  bool _isUploadBusy = false;
  bool _isUploading = false;
  int _uploadTotal = 0;
  int _uploadCurrent = 0;
  String _uploadCurrentName = '';
  double _uploadCurrentProgress = 0;

  bool _isDownloadBusy = false;
  bool _isDownloading = false;
  bool _downloadCancelled = false;
  CancelToken? _downloadCancelToken;
  int _downloadTotal = 0;
  int _downloadCurrent = 0;
  String _downloadCurrentName = '';
  double _downloadCurrentProgress = 0;
  String? _previewingFileId;

  bool get _hasFavoriteApi => widget.favoriteApi != null;

  @override
  void initState() {
    super.initState();
    _controller = RemoteFilePickerController(
      listProvider: widget.listProvider,
      createFolderProvider: widget.createFolderProvider,
      selectionMode: widget.selectionMode,
      pickTarget: widget.pickTarget,
      allowedExtensions: widget.allowedExtensions,
      showHidden: widget.showHidden,
      rootLabel: widget.rootLabel,
      initialPath: widget.initialPath,
      storageKey: widget.storageKey,
    );
    _controller.addListener(_onChanged);
    _controller.loadRoot();
    if (_hasFavoriteApi) _loadFavorites();
  }

  @override
  void dispose() {
    // 组件销毁时中断在途下载，避免回调写入已卸载的 State。
    _downloadCancelToken?.cancel('disposed');
    _controller.removeListener(_onChanged);
    _controller.dispose();
    super.dispose();
  }

  void _onChanged() {
    if (mounted) setState(() {});
  }

  Future<void> _loadFavorites() async {
    if (!_hasFavoriteApi) return;
    setState(() => _favoritesLoading = true);
    try {
      final items = await widget.favoriteApi!.list();
      if (mounted) {
        setState(() {
          _favorites = items;
          _pendingFavoritePaths.removeWhere((p) => _favoritePaths.contains(p));
        });
      }
    } finally {
      if (mounted) setState(() => _favoritesLoading = false);
    }
  }

  Future<void> _toggleFavorite(RemoteFileNode node) async {
    if (!_hasFavoriteApi) return;
    final path = node.id;
    if (_pendingFavoritePaths.contains(path)) return;
    // 已收藏 → 取消收藏（按当前机器匹配对应收藏项）。
    if (_favoritePaths.contains(path)) {
      final cur = _controller.currentMachineName ?? '';
      FavoritePathItem? existing;
      for (final f in _favorites) {
        if (f.path == path && f.machineName == cur) {
          existing = f;
          break;
        }
      }
      if (existing != null) await _removeFavorite(existing);
      return;
    }
    setState(() => _pendingFavoritePaths.add(path));
    try {
      final item = await widget.favoriteApi!.add(
        path,
        node.name,
        node.isDirectory,
        machineName: _controller.currentMachineName ?? '',
      );
      if (item != null && mounted) {
        setState(() {
          _favorites.insert(0, item);
        });
      }
    } finally {
      if (mounted) setState(() => _pendingFavoritePaths.remove(path));
    }
  }

  Future<void> _removeFavorite(FavoritePathItem item) async {
    if (!_hasFavoriteApi) return;
    final ok = await widget.favoriteApi!.delete(item.id);
    if (ok && mounted) {
      setState(() {
        _favorites.remove(item);
      });
    }
  }

  /// 点击收藏项时，切换到文件浏览模式并导航到目标目录。
  /// 如果收藏的是目录，直接打开该目录；如果是文件，打开其所在目录。
  Future<void> _navigateToFavorite(FavoritePathItem item) async {
    // 确定要浏览的目录路径
    final dirPath = item.isDirectory ? item.path : _parentPath(item.path);
    if (dirPath == null) return;

    final dirName = _pathBasename(dirPath) ?? dirPath;

    setState(() {
      _showFavorites = false;
    });

    await _controller.loadPath(dirPath, dirName);
  }

  /// 获取路径的父目录，跨平台兼容。
  static String? _parentPath(String path) {
    final normalized = path.replaceAll('\\', '/');
    final trimmed = normalized.endsWith('/') && normalized.length > 1
        ? normalized.substring(0, normalized.length - 1)
        : normalized;
    if (trimmed.isEmpty) return null;
    if (trimmed == '/') return null;
    if (trimmed.length == 2 && trimmed[1] == ':') return null;
    if (trimmed.length == 3 &&
        trimmed.endsWith(':/') &&
        trimmed[0].toUpperCase() != trimmed[0].toLowerCase()) {
      return null;
    }
    final lastSlash = trimmed.lastIndexOf('/');
    if (lastSlash <= 0) {
      return lastSlash == 0 ? '/' : null;
    }
    final parent = trimmed.substring(0, lastSlash);
    if (parent.isEmpty) return '/';
    return parent;
  }

  static String? _pathBasename(String? path) {
    if (path == null) return null;
    final normalized = path.replaceAll('\\', '/');
    final trimmed = normalized.endsWith('/') && normalized.length > 1
        ? normalized.substring(0, normalized.length - 1)
        : normalized;
    if (trimmed.isEmpty || trimmed == '/') return null;
    final lastSlash = trimmed.lastIndexOf('/');
    if (lastSlash < 0) return trimmed;
    return trimmed.substring(lastSlash + 1);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      children: [
        if (widget.title != null)
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 4, 16, 4),
            child: Row(
              children: [
                const Icon(Icons.folder_open_rounded, size: 18),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    widget.title!,
                    style: const TextStyle(
                      fontSize: 15,
                      fontWeight: FontWeight.w600,
                    ),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                  ),
                ),
                if (!_showFavorites) ...[
                  IconButton(
                    icon: const Icon(Icons.home_rounded, size: 20),
                    padding: const EdgeInsets.symmetric(horizontal: 8),
                    constraints: const BoxConstraints(
                      minWidth: 36,
                      minHeight: 36,
                    ),
                    onPressed: _controller.goToFilesystemRoot,
                    tooltip: 'remote_file_picker_go_root'.tr,
                  ),
                  IconButton(
                    icon: const Icon(Icons.person_rounded, size: 20),
                    padding: const EdgeInsets.symmetric(horizontal: 8),
                    constraints: const BoxConstraints(
                      minWidth: 36,
                      minHeight: 36,
                    ),
                    onPressed: _controller.goToHome,
                    tooltip: 'remote_file_picker_go_home'.tr,
                  ),
                ],
                if (_hasFavoriteApi)
                  IconButton(
                    icon: Icon(
                      _showFavorites
                          ? Icons.folder_open_rounded
                          : Icons.star_rounded,
                      size: 20,
                      color: _showFavorites ? AppTheme.primaryColor : null,
                    ),
                    padding: const EdgeInsets.symmetric(horizontal: 8),
                    constraints: const BoxConstraints(
                      minWidth: 36,
                      minHeight: 36,
                    ),
                    onPressed: () => setState(() {
                      if (_showFavorites) _selectedFavorites.clear();
                      _showFavorites = !_showFavorites;
                    }),
                    tooltip: 'remote_file_picker_favorites'.tr,
                  ),
              ],
            ),
          ),
        if (!_showFavorites) ...[
          _buildBreadcrumbAndBack(theme),
          if (_isUploading) _buildUploadProgress(theme),
          if (_isDownloading) _buildDownloadProgress(theme),
          const Divider(height: 1),
        ],
        Expanded(
          child: _showFavorites
              ? _buildFavoritesPanel(theme)
              : _buildBody(theme),
        ),
        if (widget.showActionBar) _buildActionBar(theme),
      ],
    );
  }

  Widget _buildBreadcrumbAndBack(ThemeData theme) {
    final stack = _controller.pathStack;
    final canBack = _controller.canNavigateBack || _controller.canNavigateUp;
    return Container(
      height: 40,
      padding: const EdgeInsets.only(left: 4, right: 12),
      child: Row(
        children: [
          if (canBack)
            IconButton(
              icon: const Icon(Icons.arrow_back_rounded, size: 20),
              padding: const EdgeInsets.symmetric(horizontal: 8),
              constraints: const BoxConstraints(minWidth: 36, minHeight: 36),
              onPressed: _controller.canNavigateBack
                  ? _controller.navigateBack
                  : _controller.navigateUp,
              tooltip: 'remote_file_picker_back'.tr,
            )
          else
            const SizedBox(width: 12),
          Expanded(child: _buildBreadcrumb(theme, stack)),
          IconButton(
            icon: Icon(
              _sortModeIcon(_controller.sortMode),
              size: 20,
              color: _controller.sortMode == RemoteFileSortMode.nameAsc
                  ? null
                  : AppTheme.primaryColor,
            ),
            padding: const EdgeInsets.symmetric(horizontal: 8),
            constraints: const BoxConstraints(minWidth: 36, minHeight: 36),
            onPressed: _controller.cycleSortMode,
            tooltip: _sortModeTooltip(_controller.sortMode),
          ),
          IconButton(
            icon: Icon(
              _controller.showHidden
                  ? Icons.visibility_rounded
                  : Icons.visibility_off_rounded,
              size: 20,
              color: _controller.showHidden ? AppTheme.primaryColor : null,
            ),
            padding: const EdgeInsets.symmetric(horizontal: 8),
            constraints: const BoxConstraints(minWidth: 36, minHeight: 36),
            onPressed: _controller.toggleShowHidden,
            tooltip: _controller.showHidden
                ? 'remote_file_picker_hide_hidden'.tr
                : 'remote_file_picker_show_hidden'.tr,
          ),
          if (_controller.canCreateFolder)
            IconButton(
              icon: const Icon(Icons.create_new_folder_outlined, size: 20),
              padding: const EdgeInsets.symmetric(horizontal: 8),
              constraints: const BoxConstraints(minWidth: 36, minHeight: 36),
              onPressed: _controller.isCreatingFolder
                  ? null
                  : _showCreateFolderDialog,
              tooltip: 'remote_file_picker_create_folder'.tr,
            ),
          if (widget.uploadBaseUrl != null && widget.uploadBaseUrl!.isNotEmpty)
            _isUploadBusy
                ? const SizedBox(
                    width: 36,
                    height: 36,
                    child: Center(
                      child: SizedBox(
                        width: 16,
                        height: 16,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      ),
                    ),
                  )
                : IconButton(
                    icon: const Icon(Icons.upload_rounded, size: 20),
                    padding: const EdgeInsets.symmetric(horizontal: 8),
                    constraints: const BoxConstraints(
                      minWidth: 36,
                      minHeight: 36,
                    ),
                    onPressed: _handleUpload,
                    tooltip: 'remote_file_picker_upload_tooltip'.tr,
                  ),
        ],
      ),
    );
  }

  IconData _sortModeIcon(RemoteFileSortMode mode) {
    switch (mode) {
      case RemoteFileSortMode.timeDesc:
        return Icons.schedule_rounded;
      case RemoteFileSortMode.nameAsc:
        return Icons.sort_by_alpha_rounded;
    }
  }

  String _sortModeTooltip(RemoteFileSortMode mode) {
    switch (mode) {
      case RemoteFileSortMode.nameAsc:
        return 'remote_file_picker_sort_name_asc'.tr;
      case RemoteFileSortMode.timeDesc:
        return 'remote_file_picker_sort_time_desc'.tr;
    }
  }

  Widget _buildBreadcrumb(
    ThemeData theme,
    List<({String? id, String name})> stack,
  ) {
    if (stack.isEmpty) return const SizedBox.shrink();

    return ListView.separated(
      scrollDirection: Axis.horizontal,
      itemCount: stack.length,
      separatorBuilder: (_, __) => Padding(
        padding: const EdgeInsets.symmetric(horizontal: 2),
        child: Icon(
          Icons.chevron_right_rounded,
          size: 16,
          color: theme.colorScheme.outline,
        ),
      ),
      itemBuilder: (context, index) {
        final isLast = index == stack.length - 1;
        final entry = stack[index];
        return InkWell(
          onTap: isLast ? null : () => _controller.navigateToIndex(index),
          borderRadius: BorderRadius.circular(4),
          child: Padding(
            padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 8),
            child: Text(
              _crumbLabel(entry.name),
              style: TextStyle(
                fontSize: 13,
                color: isLast
                    ? theme.colorScheme.onSurface
                    : AppTheme.primaryColor,
                fontWeight: isLast ? FontWeight.w600 : FontWeight.normal,
              ),
            ),
          ),
        );
      },
    );
  }

  Widget _buildFavoritesPanel(ThemeData theme) {
    if (_favoritesLoading) {
      return const Center(child: CircularProgressIndicator());
    }
    // 机器名要等首个目录列表（connector 往返）返回才知道。在它到位前不要
    // 抢先按"全部机器"展示收藏（会把其它机器的收藏混进来），而是等一下，
    // 这样收藏夹始终默认过滤到当前机器。只有目录加载结束仍拿不到机器名
    // （agent 未提供）时，才退回展示全部。
    if (_activeFavoriteMachine == null && _controller.isLoading) {
      return const Center(child: CircularProgressIndicator());
    }
    final machines = _favoriteMachines;
    final visible = _visibleFavorites;
    return Column(
      children: [
        if (machines.isNotEmpty) _buildMachineFilterBar(theme, machines),
        Expanded(
          child: visible.isEmpty
              ? _buildFavoritesEmpty(theme)
              : _buildFavoritesList(theme, visible),
        ),
      ],
    );
  }

  Widget _buildFavoritesEmpty(ThemeData theme) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            Icons.star_border_rounded,
            size: 48,
            color: theme.colorScheme.outline,
          ),
          const SizedBox(height: 12),
          Text(
            'remote_file_picker_favorites_empty'.tr,
            style: TextStyle(
              fontSize: 13,
              color: theme.colorScheme.secondary.withValues(alpha: 0.7),
            ),
          ),
        ],
      ),
    );
  }

  /// 机器过滤条：显示当前生效机器，点击弹出选择 sheet。
  Widget _buildMachineFilterBar(ThemeData theme, List<String> machines) {
    final active = _activeFavoriteMachine;
    final label = active == null
        ? 'remote_file_picker_machine_all'.tr
        : _machineDisplayName(active);
    return InkWell(
      onTap: () => _showMachineSelectorSheet(machines),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
        child: Row(
          children: [
            Icon(
              Icons.computer_rounded,
              size: 18,
              color: theme.colorScheme.primary,
            ),
            const SizedBox(width: 8),
            Text(
              'remote_file_picker_machine_label'.tr,
              style: TextStyle(
                fontSize: 13,
                color: theme.colorScheme.secondary.withValues(alpha: 0.7),
              ),
            ),
            const SizedBox(width: 6),
            Expanded(
              child: Text(
                label,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: const TextStyle(
                  fontSize: 13,
                  fontWeight: FontWeight.w600,
                ),
              ),
            ),
            Icon(
              Icons.expand_more_rounded,
              size: 20,
              color: theme.colorScheme.outline,
            ),
          ],
        ),
      ),
    );
  }

  /// 机器选择 sheet：列出收藏涉及的所有机器，点选后过滤收藏列表。
  void _showMachineSelectorSheet(List<String> machines) {
    final active = _activeFavoriteMachine;
    showModalBottomSheet<void>(
      context: context,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (sheetCtx) {
        final theme = Theme.of(sheetCtx);
        return SafeArea(
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
                child: Align(
                  alignment: Alignment.centerLeft,
                  child: Text(
                    'remote_file_picker_machine_select_title'.tr,
                    style: const TextStyle(
                      fontSize: 15,
                      fontWeight: FontWeight.w600,
                    ),
                  ),
                ),
              ),
              ...machines.map((m) {
                final count = _favorites
                    .where((f) => f.machineName == m)
                    .length;
                final selected = active == m;
                return ListTile(
                  leading: Icon(
                    Icons.computer_rounded,
                    color: selected ? theme.colorScheme.primary : null,
                  ),
                  title: Text(_machineDisplayName(m)),
                  subtitle: Text(
                    'remote_file_picker_machine_count'.trParams({
                      'count': '$count',
                    }),
                  ),
                  trailing: selected
                      ? Icon(
                          Icons.check_rounded,
                          color: theme.colorScheme.primary,
                        )
                      : null,
                  onTap: () {
                    setState(() {
                      _favoriteMachineFilter = m;
                      _selectedFavorites.clear();
                    });
                    Navigator.of(sheetCtx).pop();
                  },
                );
              }),
              const SizedBox(height: 8),
            ],
          ),
        );
      },
    );
  }

  Widget _buildFavoritesList(ThemeData theme, List<FavoritePathItem> visible) {
    return ListView.builder(
      itemCount: visible.length,
      itemBuilder: (context, index) {
        final item = visible[index];
        final isSelected = _selectedFavorites.contains(item);
        return ListTile(
          leading: Icon(
            item.isDirectory
                ? Icons.folder_rounded
                : Icons.insert_drive_file_rounded,
            color: item.isDirectory ? const Color(0xFFF5A623) : null,
            size: 28,
          ),
          title: Text(
            item.name,
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: const TextStyle(fontWeight: FontWeight.w500),
          ),
          subtitle: Text(
            _truncatePathFromFront(item.path, 50),
            maxLines: 1,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(
              fontSize: 12,
              color: theme.colorScheme.secondary.withValues(alpha: 0.6),
            ),
          ),
          trailing: Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              IconButton(
                icon: Icon(
                  Icons.close_rounded,
                  size: 18,
                  color: theme.colorScheme.outline,
                ),
                onPressed: () => _removeFavorite(item),
                tooltip: 'remote_file_picker_favorites_remove'.tr,
              ),
              // 选择形态跟随 selectionMode，与文件浏览保持一致：
              // 多选用方框勾选框，单选用圆圈单选钮。
              _controller.isMultiSelect
                  ? Checkbox(
                      value: isSelected,
                      onChanged: (_) => _toggleFavoriteSelection(item),
                    )
                  : GestureDetector(
                      behavior: HitTestBehavior.opaque,
                      onTap: () => _toggleFavoriteSelection(item),
                      child: Padding(
                        padding: const EdgeInsets.all(8),
                        child: Icon(
                          isSelected
                              ? Icons.check_circle_rounded
                              : Icons.radio_button_unchecked_rounded,
                          color: isSelected
                              ? theme.primaryColor
                              : theme.colorScheme.outline.withValues(
                                  alpha: 0.6,
                                ),
                        ),
                      ),
                    ),
            ],
          ),
          selected: isSelected,
          onTap: () => _navigateToFavorite(item),
        );
      },
    );
  }

  void _toggleFavoriteSelection(FavoritePathItem item) {
    setState(() {
      if (_selectedFavorites.contains(item)) {
        _selectedFavorites.remove(item);
      } else {
        // 单选模式：选新的先清掉旧的，保证最多只选一个。
        if (!_controller.isMultiSelect) _selectedFavorites.clear();
        _selectedFavorites.add(item);
      }
    });
  }

  void _confirmSelectedFavorites() {
    if (_selectedFavorites.isEmpty) return;
    final nodes = _selectedFavorites
        .map(
          (item) => RemoteFileNode(
            id: item.path,
            name: item.name,
            isDirectory: item.isDirectory,
          ),
        )
        .toList();
    widget.onConfirm?.call(RemoteFilePickerResult(selectedFiles: nodes));
  }

  String _truncatePathFromFront(String path, int maxLen) {
    if (path.length <= maxLen) return path;
    return '...${path.substring(path.length - maxLen + 3)}';
  }

  Widget _buildBody(ThemeData theme) {
    if (_controller.isLoading) {
      return const Center(child: CircularProgressIndicator());
    }

    if (_controller.error != null) {
      final showGoToRoot =
          _controller.pathStack.isNotEmpty &&
          _controller.pathStack.first.id != null;
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(
                Icons.error_outline_rounded,
                size: 48,
                color: theme.colorScheme.outline,
              ),
              const SizedBox(height: 12),
              Text(
                _controller.error!.tr,
                style: TextStyle(
                  fontSize: 13,
                  color: theme.colorScheme.secondary.withValues(alpha: 0.7),
                ),
                textAlign: TextAlign.center,
              ),
              const SizedBox(height: 16),
              Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  FilledButton.tonal(
                    onPressed: _controller.retry,
                    child: Text('common_retry'.tr),
                  ),
                  if (showGoToRoot) ...[
                    const SizedBox(width: 12),
                    OutlinedButton(
                      onPressed: _controller.goToRoot,
                      child: Text('remote_file_picker_go_root'.tr),
                    ),
                  ],
                ],
              ),
            ],
          ),
        ),
      );
    }

    final items = _controller.items;
    if (items.isEmpty) {
      return Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(
              Icons.folder_off_rounded,
              size: 48,
              color: theme.colorScheme.outline,
            ),
            const SizedBox(height: 12),
            Text(
              'remote_file_picker_empty'.tr,
              style: TextStyle(
                fontSize: 13,
                color: theme.colorScheme.secondary.withValues(alpha: 0.7),
              ),
            ),
          ],
        ),
      );
    }

    return ListView.builder(
      itemCount: items.length,
      itemBuilder: (context, index) => _buildItemRow(theme, items[index]),
    );
  }

  Widget _buildItemRow(ThemeData theme, RemoteFileNode node) {
    final isFavorited = _favoritePaths.contains(node.id);
    final isPending = _pendingFavoritePaths.contains(node.id);

    if (node.isDirectory) {
      final selected =
          _controller.canSelectDirectory &&
          _controller.selectedItems.contains(node);
      return ListTile(
        leading: const Icon(
          Icons.folder_rounded,
          color: Color(0xFFF5A623),
          size: 28,
        ),
        title: Text(node.name, maxLines: 1, overflow: TextOverflow.ellipsis),
        subtitle: _dirSubtitle(theme, node),
        trailing: _buildTrailingWithFavorite(
          theme: theme,
          isFavorited: isFavorited,
          isPending: isPending,
          onFavoriteTap: () => _toggleFavorite(node),
          child: _controller.canSelectDirectory
              ? (_controller.isMultiSelect
                    ? Checkbox(
                        value: selected,
                        onChanged: (_) => _controller.toggleSelect(node),
                      )
                    : GestureDetector(
                        onTap: () => _controller.toggleSelect(node),
                        behavior: HitTestBehavior.opaque,
                        child: Padding(
                          padding: const EdgeInsets.all(8),
                          child: Icon(
                            selected
                                ? Icons.check_circle_rounded
                                : Icons.radio_button_unchecked_rounded,
                            color: selected
                                ? theme.primaryColor
                                : theme.colorScheme.outline.withValues(
                                    alpha: 0.6,
                                  ),
                          ),
                        ),
                      ))
              : Icon(
                  Icons.chevron_right_rounded,
                  color: theme.colorScheme.outline,
                ),
        ),
        selected: selected,
        onTap: () => _controller.navigateTo(node),
      );
    }

    final canSelectFile = _controller.canSelectFile;
    final canPreview = _canPreviewTextFile(node);
    final selected = canSelectFile && _controller.selectedItems.contains(node);
    return ListTile(
      enabled: canSelectFile || canPreview,
      leading: Icon(
        _fileIcon(node),
        size: 28,
        color: theme.colorScheme.onSurface.withValues(alpha: 0.6),
      ),
      title: Text(node.name, maxLines: 1, overflow: TextOverflow.ellipsis),
      subtitle: _fileSubtitle(theme, node),
      trailing: _buildFileTrailing(
        theme: theme,
        node: node,
        canPreview: canPreview,
        canSelectFile: canSelectFile,
        selected: selected,
        isFavorited: isFavorited,
        isPending: isPending,
        onFavoriteTap: () => _toggleFavorite(node),
      ),
      selected: selected,
      onTap: canSelectFile ? () => _onFileTap(node) : null,
    );
  }

  Widget _buildFileTrailing({
    required ThemeData theme,
    required RemoteFileNode node,
    required bool canPreview,
    required bool canSelectFile,
    required bool selected,
    required bool isFavorited,
    required bool isPending,
    required VoidCallback onFavoriteTap,
  }) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        if (canPreview)
          IconButton(
            key: ValueKey('remote-text-preview:${node.id}'),
            tooltip: 'remote_file_picker_preview_text'.tr,
            onPressed: _previewingFileId == null
                ? () => _previewTextFile(node)
                : null,
            icon: _previewingFileId == node.id
                ? const SizedBox(
                    width: 18,
                    height: 18,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Icon(Icons.visibility_outlined),
          ),
        if (_hasFavoriteApi)
          _buildFavoriteButton(
            theme: theme,
            isFavorited: isFavorited,
            isPending: isPending,
            onFavoriteTap: onFavoriteTap,
          ),
        if (_hasFavoriteApi && canSelectFile) const SizedBox(width: 4),
        if (canSelectFile)
          _controller.isMultiSelect
              ? Checkbox(value: selected, onChanged: (_) => _onFileTap(node))
              : Icon(
                  selected
                      ? Icons.check_circle_rounded
                      : Icons.radio_button_unchecked_rounded,
                  color: selected
                      ? theme.primaryColor
                      : theme.colorScheme.outline.withValues(alpha: 0.6),
                ),
      ],
    );
  }

  Widget _buildTrailingWithFavorite({
    required ThemeData theme,
    required bool isFavorited,
    required bool isPending,
    required VoidCallback onFavoriteTap,
    required Widget child,
  }) {
    if (!_hasFavoriteApi) return child;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        _buildFavoriteButton(
          theme: theme,
          isFavorited: isFavorited,
          isPending: isPending,
          onFavoriteTap: onFavoriteTap,
        ),
        const SizedBox(width: 4),
        child,
      ],
    );
  }

  Widget _buildFavoriteButton({
    required ThemeData theme,
    required bool isFavorited,
    required bool isPending,
    required VoidCallback onFavoriteTap,
  }) {
    return GestureDetector(
      // 始终由星标自身消费点击，避免冒泡到文件选择或进入目录。
      behavior: HitTestBehavior.opaque,
      onTap: isPending ? () {} : onFavoriteTap,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 4),
        child: isPending
            ? const SizedBox(
                width: 16,
                height: 16,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            : Icon(
                isFavorited ? Icons.star_rounded : Icons.star_border_rounded,
                size: 18,
                color: isFavorited
                    ? const Color(0xFFFFC107)
                    : theme.colorScheme.outline,
              ),
      ),
    );
  }

  void _onFileTap(RemoteFileNode node) {
    // 仅目录模式下文件不可作为选择目标：直接忽略点击，
    // 否则可能把已有的选中项（可能是另一个目录）提交出去。
    if (!_controller.canSelectFile) {
      return;
    }
    _controller.toggleSelect(node);
  }

  bool _canPreviewTextFile(RemoteFileNode node) {
    final baseUrl = widget.uploadBaseUrl?.trim() ?? '';
    return !node.isDirectory &&
        baseUrl.isNotEmpty &&
        TextDocumentOpenService.supportsRemoteFile(
          fileName: node.name,
          mimeType: node.mimeType ?? '',
        );
  }

  Future<void> _previewTextFile(RemoteFileNode node) async {
    if (_previewingFileId != null || !_canPreviewTextFile(node)) return;
    final baseUrl = widget.uploadBaseUrl!.trim().replaceFirst(
      RegExp(r'/+$'),
      '',
    );
    setState(() => _previewingFileId = node.id);
    try {
      await TextDocumentOpenService.openRemoteFile(
        url: '$baseUrl/download',
        fileName: node.name,
        mimeType: node.mimeType ?? '',
        queryParameters: {'path': node.id},
        handleSeed: '$baseUrl:${node.id}',
      );
    } catch (error) {
      if (!mounted) return;
      CustomToast.show(
        'remote_picker_preview_failed'.trParams({
          'name': node.name,
          'error': _dioErrorReason(error),
        }),
        isError: true,
      );
    } finally {
      if (mounted) setState(() => _previewingFileId = null);
    }
  }

  void _handleConfirm() {
    final result = _controller.confirm();
    widget.onConfirm?.call(result);
  }

  void _handleUseCurrentDir() {
    final currentDir = _controller.currentDirectoryNode;
    if (currentDir == null) return;
    // 盘符列表/未解析的哨兵根（id 为空或 `::` 前缀）不是可提交的真实目录。
    if (currentDir.id.isEmpty || currentDir.id.startsWith('::')) return;
    widget.onConfirm?.call(RemoteFilePickerResult(selectedFiles: [currentDir]));
  }

  Widget _buildUploadProgress(ThemeData theme) {
    return Container(
      padding: const EdgeInsets.fromLTRB(16, 6, 16, 6),
      color: theme.colorScheme.surfaceContainerLow,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  'remote_file_picker_uploading'.trParams({
                    'current': '$_uploadCurrent',
                    'total': '$_uploadTotal',
                    'name': _uploadCurrentName,
                  }),
                  style: TextStyle(
                    fontSize: 12,
                    color: theme.colorScheme.onSurface.withValues(alpha: 0.7),
                  ),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
            ],
          ),
          const SizedBox(height: 4),
          LinearProgressIndicator(
            value: _uploadCurrentProgress > 0 ? _uploadCurrentProgress : null,
          ),
        ],
      ),
    );
  }

  Widget _buildDownloadProgress(ThemeData theme) {
    return Container(
      padding: const EdgeInsets.fromLTRB(16, 6, 16, 6),
      color: theme.colorScheme.surfaceContainerLow,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  'remote_file_picker_downloading'.trParams({
                    'current': '$_downloadCurrent',
                    'total': '$_downloadTotal',
                    'name': _downloadCurrentName,
                  }),
                  style: TextStyle(
                    fontSize: 12,
                    color: theme.colorScheme.onSurface.withValues(alpha: 0.7),
                  ),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                ),
              ),
              InkWell(
                onTap: _downloadCancelled
                    ? null
                    : () {
                        setState(() => _downloadCancelled = true);
                        // 立即中断正在进行的下载，避免大文件取消要等很久。
                        _downloadCancelToken?.cancel('user_cancelled');
                      },
                child: Padding(
                  padding: const EdgeInsets.symmetric(
                    horizontal: 6,
                    vertical: 2,
                  ),
                  child: Text(
                    'common_cancel'.tr,
                    style: TextStyle(
                      fontSize: 12,
                      color: theme.colorScheme.primary,
                    ),
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 4),
          LinearProgressIndicator(
            value: _downloadCurrentProgress > 0
                ? _downloadCurrentProgress
                : null,
          ),
        ],
      ),
    );
  }

  Future<void> _handleUpload() async {
    // 防重复点击：ping 主机有最长 3 秒等待，期间按钮已置忙碌态，
    // 这里再做一道入口拦截，杜绝并发触发导致文件选择器弹两次。
    if (_isUploadBusy) return;
    final baseUrl = widget.uploadBaseUrl;
    if (baseUrl == null || baseUrl.isEmpty) return;
    final currentDir =
        _controller.currentParentId ?? _controller.serverCurrentPath;
    if (currentDir == null || currentDir.isEmpty) {
      CustomToast.show('remote_picker_upload_need_dir'.tr, isError: true);
      return;
    }

    setState(() => _isUploadBusy = true);
    try {
      if (!await RemoteFileHostConnectivity.isReachable(baseUrl)) {
        if (!mounted) return;
        await showDialog<void>(
          context: context,
          builder: (ctx) => AlertDialog(
            title: Text('remote_file_picker_host_unreachable_title'.tr),
            content: Text('remote_file_picker_host_unreachable_upload'.tr),
            actions: [
              TextButton(
                onPressed: () => Navigator.of(ctx).pop(),
                child: Text('common_got_it'.tr),
              ),
            ],
          ),
        );
        return;
      }

      // Let user choose source: gallery (photos/videos) or file browser.
      final source = await _pickUploadSource();
      if (source == null) return;
      if (!mounted) return;

      final candidates = await _collectUploadCandidates(source);
      if (candidates == null || candidates.isEmpty) return;
      if (!mounted) return;

      setState(() {
        _isUploading = true;
        _uploadTotal = candidates.length;
        _uploadCurrent = 0;
        _uploadCurrentName = '';
        _uploadCurrentProgress = 0;
      });

      int successCount = 0;
      final dio = Dio(
        BaseOptions(
          connectTimeout: const Duration(seconds: 30),
          receiveTimeout: const Duration(seconds: 10),
        ),
      );

      try {
        for (int i = 0; i < candidates.length; i++) {
          final file = candidates[i];
          if (!mounted) break;
          setState(() {
            _uploadCurrent = i + 1;
            _uploadCurrentName = file.name;
            _uploadCurrentProgress = 0;
          });

          try {
            await dio.post(
              '$baseUrl/upload',
              queryParameters: {'dir': currentDir},
              data: file.stream,
              options: Options(
                headers: {
                  'X-Filename': Uri.encodeComponent(file.name),
                  'Content-Type': 'application/octet-stream',
                  if (file.size > 0) 'Content-Length': '${file.size}',
                },
                sendTimeout: const Duration(minutes: 30),
                receiveTimeout: const Duration(seconds: 10),
              ),
              onSendProgress: (sent, total) {
                if (!mounted || total <= 0) return;
                setState(() => _uploadCurrentProgress = sent / total);
              },
            );
            successCount++;
          } on DioException catch (e) {
            if (!mounted) break;
            final msg = e.response?.statusCode == 413
                ? 'remote_picker_upload_too_large'.trParams({'name': file.name})
                : 'remote_picker_upload_failed'.trParams({'name': file.name});
            CustomToast.show(msg, isError: true);
          } catch (_) {
            if (mounted) {
              CustomToast.show(
                'remote_picker_upload_failed'.trParams({'name': file.name}),
                isError: true,
              );
            }
          }
        }
      } finally {
        if (mounted) {
          setState(() {
            _isUploading = false;
            _uploadTotal = 0;
            _uploadCurrent = 0;
            _uploadCurrentName = '';
            _uploadCurrentProgress = 0;
          });
        }
      }

      if (!mounted || successCount == 0) return;
      await _controller.retry();
      CustomToast.show(
        successCount == candidates.length
            ? 'remote_picker_uploaded_all'.trParams({
                'count': '$successCount',
              })
            : 'remote_picker_uploaded_partial'.trParams({
                'success': '$successCount',
                'total': '${candidates.length}',
              }),
      );
    } finally {
      if (mounted) setState(() => _isUploadBusy = false);
    }
  }

  Future<_UploadSource?> _pickUploadSource() {
    return showModalBottomSheet<_UploadSource>(
      context: context,
      showDragHandle: true,
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              leading: const Icon(Icons.photo_library_outlined),
              title: Text('remote_file_picker_pick_gallery'.tr),
              subtitle: Text('remote_file_picker_pick_gallery_desc'.tr),
              onTap: () => Navigator.of(ctx).pop(_UploadSource.gallery),
            ),
            ListTile(
              leading: const Icon(Icons.folder_open_outlined),
              title: Text('remote_file_picker_pick_files'.tr),
              subtitle: Text('remote_file_picker_pick_files_desc'.tr),
              onTap: () => Navigator.of(ctx).pop(_UploadSource.files),
            ),
            const SizedBox(height: 8),
          ],
        ),
      ),
    );
  }

  Future<List<_UploadCandidate>?> _collectUploadCandidates(
    _UploadSource source,
  ) async {
    switch (source) {
      case _UploadSource.gallery:
        try {
          final picker = gallery_picker.ImagePicker();
          final items = await picker.pickMultipleMedia();
          if (items.isEmpty) return const <_UploadCandidate>[];
          final list = <_UploadCandidate>[];
          for (final x in items) {
            final size = await x.length();
            list.add(
              _UploadCandidate(name: x.name, size: size, stream: x.openRead()),
            );
          }
          return list;
        } catch (_) {
          if (mounted) {
            CustomToast.show('remote_picker_open_album_failed'.tr, isError: true);
          }
          return null;
        }
      case _UploadSource.files:
        final result = await FilePicker.platform.pickFiles(
          allowMultiple: true,
          withReadStream: true,
        );
        if (result == null) return const <_UploadCandidate>[];
        final list = <_UploadCandidate>[];
        for (final f in result.files) {
          final stream = f.readStream;
          if (stream == null) {
            if (mounted) {
              CustomToast.show(
                'remote_picker_read_failed'.trParams({'name': f.name}),
                isError: true,
              );
            }
            continue;
          }
          list.add(
            _UploadCandidate(name: f.name, size: f.size, stream: stream),
          );
        }
        return list;
    }
  }

  Future<void> _showHostUnreachableDialog() {
    return showDialog<void>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('remote_file_picker_host_unreachable_title'.tr),
        content: Text('remote_file_picker_host_unreachable_download'.tr),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(),
            child: Text('common_got_it'.tr),
          ),
        ],
      ),
    );
  }

  /// 下载选中的文件/目录到手机本地目录。
  /// 文件直接拉取；目录经 /manifest 递归展开后逐个拉取并在本地重建子目录结构。
  /// 全部落到用户选定的目录（图片/视频也不入相册，统一按目录树存放）。
  Future<void> _handleDownload() async {
    if (kIsWeb) return;
    if (_isDownloadBusy) return;
    final baseUrl = widget.uploadBaseUrl;
    if (baseUrl == null || baseUrl.isEmpty) return;
    final selected = _controller.selectedItems
        .where((n) => n.id.isNotEmpty && !n.id.startsWith('::'))
        .toList();
    if (selected.isEmpty) {
      CustomToast.show('remote_picker_download_need_selection'.tr, isError: true);
      return;
    }

    setState(() => _isDownloadBusy = true);
    try {
      // 连通性检查（3 秒超时）
      if (!await RemoteFileHostConnectivity.isReachable(baseUrl)) {
        if (!mounted) return;
        await _showHostUnreachableDialog();
        return;
      }

      // 选下载落地目录。
      // iOS 沙盒限制：无法写入系统目录选择器返回的受限路径（PathAccessException），
      // 统一落到 App 自己的、在「文件」App 可见的 Documents 子目录。
      // 安卓/桌面：仍由用户选择任意目录直接写入。
      final String destDir;
      final bool isIos = defaultTargetPlatform == TargetPlatform.iOS;
      if (isIos) {
        destDir = await appVisibleDownloadDirectory(
          'remote_file_picker_download_dir_name'.tr,
        );
      } else {
        final picked = await FilePicker.platform.getDirectoryPath();
        if (picked == null || picked.isEmpty) return;
        destDir = picked;
      }
      if (!mounted) return;

      // 展开下载任务：文件直接入列，目录经 /manifest 递归展开。
      final tasks = <({String path, String savePath})>[];
      final emptyDirs = <String>[];
      bool truncated = false;
      int unreadable = 0; // 宿主机侧无权限读取、未列入清单的项数
      final metaDio = Dio(
        BaseOptions(
          connectTimeout: const Duration(seconds: 10),
          receiveTimeout: const Duration(seconds: 30),
        ),
      );
      for (final node in selected) {
        if (!node.isDirectory) {
          tasks.add((path: node.id, savePath: p.join(destDir, node.name)));
          continue;
        }
        try {
          final resp = await metaDio.get(
            '$baseUrl/manifest',
            queryParameters: {'path': node.id},
          );
          final data = resp.data is String
              ? jsonDecode(resp.data as String)
              : resp.data;
          // 清单结构异常不静默跳过，明确告知用户该目录没下成。
          if (data is! Map || data['entries'] is! List) {
            if (mounted) {
              CustomToast.show(
                'remote_picker_dir_listing_skipped'.trParams({
                  'name': node.name,
                }),
                isError: true,
              );
            }
            continue;
          }
          final plan = planDirectoryDownload(
            destDir: destDir,
            rootName: node.name,
            manifest: data,
          );
          if (plan.truncated) truncated = true;
          final u = data['unreadable'];
          if (u is int) unreadable += u;
          emptyDirs.addAll(plan.dirs);
          for (final f in plan.files) {
            tasks.add((path: f.hostPath, savePath: f.savePath));
          }
        } catch (e) {
          if (mounted) {
            CustomToast.show(
              'remote_picker_read_dir_failed'.trParams({
                'name': node.name,
                'error': _dioErrorReason(e),
              }),
              isError: true,
            );
          }
        }
      }

      if (tasks.isEmpty && emptyDirs.isEmpty) {
        if (mounted) {
          CustomToast.show('remote_picker_download_empty'.tr);
        }
        return;
      }

      setState(() {
        _downloadCancelled = false;
        _isDownloading = true;
        _downloadTotal = tasks.length;
        _downloadCurrent = 0;
        _downloadCurrentName = '';
        _downloadCurrentProgress = 0;
      });

      // 先重建空目录，保持原目录结构。失败不静默——计数后汇总提示。
      int dirFailed = 0;
      for (final d in emptyDirs) {
        try {
          await ensureDirectory(d);
        } catch (_) {
          dirFailed++;
        }
      }

      int successCount = 0;
      int failCount = 0;
      final cancelToken = CancelToken();
      _downloadCancelToken = cancelToken;
      final dio = Dio(
        BaseOptions(
          connectTimeout: const Duration(seconds: 30),
          receiveTimeout: const Duration(seconds: 30),
        ),
      );
      try {
        for (int i = 0; i < tasks.length; i++) {
          if (_downloadCancelled || !mounted) break;
          final t = tasks[i];
          final name = p.basename(t.savePath);
          setState(() {
            _downloadCurrent = i + 1;
            _downloadCurrentName = name;
            _downloadCurrentProgress = 0;
          });
          try {
            await ensureDirectory(p.dirname(t.savePath));
            await dio.download(
              '$baseUrl/download',
              t.savePath,
              queryParameters: {'path': t.path},
              cancelToken: cancelToken,
              onReceiveProgress: (received, total) {
                if (!mounted || total <= 0) return;
                setState(() => _downloadCurrentProgress = received / total);
              },
            );
            successCount++;
          } catch (e) {
            // 失败/取消都清掉可能残留的半截文件，避免坏文件留在用户目录。
            await deleteFileQuietly(t.savePath);
            if (e is DioException && CancelToken.isCancel(e)) {
              break; // 用户取消：不计失败、不报错
            }
            failCount++;
            // 逐条失败最多提示前 3 个，避免整批失败时刷屏；
            // 总失败数由结尾汇总提示兜底，数量不隐藏。
            if (mounted && failCount <= 3) {
              CustomToast.show(
                'remote_picker_download_file_failed'.trParams({
                  'name': name,
                  'error': _dioErrorReason(e),
                }),
                isError: true,
              );
            }
          }
        }
      } finally {
        _downloadCancelToken = null;
        if (mounted) {
          setState(() {
            _isDownloading = false;
            _downloadTotal = 0;
            _downloadCurrent = 0;
            _downloadCurrentName = '';
            _downloadCurrentProgress = 0;
          });
        }
      }

      if (!mounted) return;
      final notes = <String>[];
      if (_downloadCancelled) {
        notes.add('remote_file_picker_note_cancelled'.tr);
      }
      if (failCount > 0) {
        notes.add(
          'remote_file_picker_note_failed'.trParams({'count': '$failCount'}),
        );
      }
      if (truncated) notes.add('remote_file_picker_note_truncated'.tr);
      if (unreadable > 0) {
        notes.add(
          'remote_file_picker_note_unreadable'.trParams({
            'count': '$unreadable',
          }),
        );
      }
      if (dirFailed > 0) {
        notes.add(
          'remote_file_picker_note_empty_dirs'.trParams({
            'count': '$dirFailed',
          }),
        );
      }
      final note = notes.isEmpty
          ? ''
          : 'remote_file_picker_notes'.trParams({
              'notes': notes.join('remote_file_picker_note_sep'.tr),
            });
      // iOS 落在沙盒目录，给用户「文件」App 里的可读位置而非冗长系统路径。
      final location = isIos
          ? 'remote_file_picker_ios_location'.tr
          : destDir;
      final summary = 'remote_file_picker_downloaded_summary'.trParams({
        'success': '$successCount',
        'total': '${tasks.length}',
        'location': location,
        'note': note,
      });
      // 能"打开文件夹"的平台：iOS(shareddocuments) 与桌面(file://)；
      // 安卓无统一方案，仍用 toast（用户自选目录，知道位置）。
      final canOpenFolder =
          isIos ||
          defaultTargetPlatform == TargetPlatform.macOS ||
          defaultTargetPlatform == TargetPlatform.linux ||
          defaultTargetPlatform == TargetPlatform.windows;
      if (successCount > 0 && canOpenFolder) {
        await _showDownloadDoneDialog(summary, destDir, isIos);
      } else {
        CustomToast.show(
          summary,
          // 全部失败时标红；部分成功仍正常提示（问题已在 note 与逐条 toast 中体现）。
          isError: tasks.isNotEmpty && successCount == 0,
        );
      }
    } catch (e) {
      // 兜底：getDirectoryPath、jsonDecode 等意外异常不静默、不崩溃。
      if (mounted) {
        CustomToast.show(
          'remote_picker_download_error'.trParams({
            'error': _dioErrorReason(e),
          }),
          isError: true,
        );
      }
    } finally {
      if (mounted) setState(() => _isDownloadBusy = false);
    }
  }

  /// 把下载/清单请求的异常翻译成可读原因，避免只丢一句"失败"。
  String _dioErrorReason(Object e) => remoteFilePickerErrorText(e).tr;

  String _crumbLabel(String name) {
    switch (name) {
      case 'root':
      case 'remote_file_picker_root_label':
        return 'remote_file_picker_root_label'.tr;
      case 'Home':
      case 'remote_file_picker_go_home':
        return 'remote_file_picker_go_home'.tr;
      default:
        return name;
    }
  }

  /// 下载完成提示：在可打开文件夹的平台给「打开文件夹」入口。
  Future<void> _showDownloadDoneDialog(
    String summary,
    String destDir,
    bool isIos,
  ) {
    return showDialog<void>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('remote_file_picker_download_done'.tr),
        content: Text(summary),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(),
            child: Text('common_ok'.tr),
          ),
          FilledButton(
            onPressed: () {
              Navigator.of(ctx).pop();
              _openDownloadFolder(destDir, isIos);
            },
            child: Text('remote_file_picker_open_folder'.tr),
          ),
        ],
      ),
    );
  }

  /// 打开下载落地目录。iOS 用 shareddocuments 跳「文件」App 的 App 目录
  /// （配合 Info.plist 的文件共享键）；桌面用 file:// 在访达/资源管理器打开。
  /// 打不开时回退为提示，不静默。
  Future<void> _openDownloadFolder(String destDir, bool isIos) async {
    try {
      final uri = isIos
          ? Uri.parse('shareddocuments://${Uri.encodeFull(destDir)}')
          : Uri.file(destDir);
      final ok = await launchUrl(uri, mode: LaunchMode.externalApplication);
      if (!ok && mounted) {
        CustomToast.show('remote_picker_open_folder_failed'.tr, isError: true);
      }
    } catch (_) {
      if (mounted) {
        CustomToast.show('remote_picker_open_folder_failed'.tr, isError: true);
      }
    }
  }

  void _showCreateFolderDialog() {
    final controller = TextEditingController();
    showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('remote_file_picker_create_folder'.tr),
        content: TextField(
          controller: controller,
          autofocus: true,
          decoration: InputDecoration(
            hintText: 'remote_file_picker_folder_name_hint'.tr,
          ),
          onSubmitted: (v) => Navigator.of(ctx).pop(v),
        ),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(ctx).pop(),
            child: Text('common_cancel'.tr),
          ),
          FilledButton(
            onPressed: () => Navigator.of(ctx).pop(controller.text),
            child: Text('common_confirm'.tr),
          ),
        ],
      ),
    ).then((name) {
      if (name != null && name.trim().isNotEmpty) {
        _controller.createFolder(name);
      }
    });
  }

  Widget? _fileSubtitle(ThemeData theme, RemoteFileNode node) {
    final parts = <String>[];
    if (node.size != null) parts.add(_formatFileSize(node.size!));
    final modified = _formatModifiedTime(node);
    if (modified != null) parts.add(modified);
    if (parts.isEmpty) return null;
    return Text(parts.join(' · '), style: theme.textTheme.bodySmall);
  }

  /// 目录行副标题：仅在拿到最后更新时间时展示，否则不占位。
  Widget? _dirSubtitle(ThemeData theme, RemoteFileNode node) {
    final modified = _formatModifiedTime(node);
    if (modified == null) return null;
    return Text(modified, style: theme.textTheme.bodySmall);
  }

  /// 将条目的最后更新时间格式化为与全站一致的友好文案（今天/昨天/星期/日期）。
  /// connector 未提供时间时返回 null。
  String? _formatModifiedTime(RemoteFileNode node) {
    final at = node.modifiedAt;
    if (at == null) return null;
    final text = TimeFormatter.formatChatTime(at.millisecondsSinceEpoch);
    return text.isEmpty ? null : text;
  }

  IconData _fileIcon(RemoteFileNode node) {
    final mime = node.mimeType?.toLowerCase();
    if (mime != null) {
      if (mime.startsWith('image/')) return Icons.image_rounded;
      if (mime.startsWith('video/')) return Icons.videocam_rounded;
      if (mime.startsWith('audio/')) return Icons.audiotrack_rounded;
      if (mime.contains('pdf')) return Icons.picture_as_pdf_rounded;
      if (mime.contains('zip') || mime.contains('compressed')) {
        return Icons.archive_rounded;
      }
    }
    final ext = node.name.contains('.')
        ? node.name.split('.').last.toLowerCase()
        : '';
    switch (ext) {
      case 'pdf':
        return Icons.picture_as_pdf_rounded;
      case 'jpg':
      case 'jpeg':
      case 'png':
      case 'gif':
      case 'webp':
      case 'svg':
        return Icons.image_rounded;
      case 'mp4':
      case 'mov':
      case 'avi':
      case 'mkv':
        return Icons.videocam_rounded;
      case 'mp3':
      case 'wav':
      case 'flac':
      case 'aac':
        return Icons.audiotrack_rounded;
      case 'zip':
      case 'rar':
      case '7z':
      case 'tar':
      case 'gz':
        return Icons.archive_rounded;
      case 'doc':
      case 'docx':
        return Icons.description_rounded;
      case 'xls':
      case 'xlsx':
        return Icons.table_chart_rounded;
      case 'ppt':
      case 'pptx':
        return Icons.slideshow_rounded;
      case 'json':
      case 'xml':
      case 'yaml':
      case 'yml':
        return Icons.data_object_rounded;
      case 'md':
      case 'txt':
        return Icons.article_rounded;
      default:
        return Icons.insert_drive_file_rounded;
    }
  }

  Widget _buildActionBar(ThemeData theme) {
    if (_showFavorites) {
      return _buildFavoritesActionBar(theme);
    }
    final count = _controller.selectedItems.length;
    return Padding(
      padding: EdgeInsets.fromLTRB(12, 8, 12, 10 + widget.paddingBottom),
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 600),
        child: Row(
          children: [
            if (count > 0)
              TextButton(
                onPressed: _controller.clearSelection,
                child: Text('${'remote_file_picker_clear'.tr}($count)'),
              ),
            Expanded(
              child: count > 0
                  ? const SizedBox.shrink()
                  : Text(
                      (_controller.canSelectDirectory
                              ? 'remote_file_picker_no_selection_dir'
                              : 'remote_file_picker_no_selection')
                          .tr,
                      style: TextStyle(
                        fontSize: 13,
                        color: theme.colorScheme.secondary.withValues(
                          alpha: 0.7,
                        ),
                      ),
                    ),
            ),
            if (!kIsWeb &&
                widget.uploadBaseUrl != null &&
                widget.uploadBaseUrl!.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(right: 8),
                child: _isDownloadBusy
                    ? const SizedBox(
                        width: 40,
                        height: 40,
                        child: Center(
                          child: SizedBox(
                            width: 16,
                            height: 16,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          ),
                        ),
                      )
                    : OutlinedButton.icon(
                        onPressed: count > 0 ? _handleDownload : null,
                        icon: const Icon(Icons.download_rounded, size: 18),
                        label: Text('common_download'.tr),
                        style: OutlinedButton.styleFrom(
                          foregroundColor: AppTheme.primaryColor,
                          side: const BorderSide(color: AppTheme.primaryColor),
                          minimumSize: const Size(64, 40),
                          padding: const EdgeInsets.symmetric(horizontal: 12),
                        ),
                      ),
              ),
            if (_controller.canSelectDirectory)
              OutlinedButton(
                onPressed: _handleUseCurrentDir,
                style: OutlinedButton.styleFrom(
                  foregroundColor: AppTheme.primaryColor,
                  side: const BorderSide(color: AppTheme.primaryColor),
                  minimumSize: const Size(64, 40),
                ),
                child: Text('remote_file_picker_use_current_dir'.tr),
              ),
            const SizedBox(width: 8),
            FilledButton(
              onPressed: count > 0 ? _handleConfirm : null,
              child: Text('common_confirm'.tr),
            ),
          ],
        ),
      ),
    );
  }

  Widget _buildFavoritesActionBar(ThemeData theme) {
    final count = _selectedFavorites.length;
    return Padding(
      padding: EdgeInsets.fromLTRB(12, 8, 12, 10 + widget.paddingBottom),
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 600),
        child: Row(
          children: [
            if (count > 0)
              TextButton(
                onPressed: () => setState(() => _selectedFavorites.clear()),
                child: Text('${'remote_file_picker_clear'.tr}($count)'),
              ),
            Expanded(
              child: count > 0
                  ? const SizedBox.shrink()
                  : Text(
                      'remote_file_picker_favorites_tap_hint'.tr,
                      style: TextStyle(
                        fontSize: 13,
                        color: theme.colorScheme.secondary.withValues(
                          alpha: 0.7,
                        ),
                      ),
                    ),
            ),
            const SizedBox(width: 8),
            FilledButton(
              onPressed: count > 0 ? _confirmSelectedFavorites : null,
              child: Text('common_confirm'.tr),
            ),
          ],
        ),
      ),
    );
  }
}

String _formatFileSize(int bytes) {
  if (bytes < 1024) return '$bytes B';
  if (bytes < 1024 * 1024) return '${(bytes / 1024).toStringAsFixed(1)} KB';
  if (bytes < 1024 * 1024 * 1024) {
    return '${(bytes / (1024 * 1024)).toStringAsFixed(1)} MB';
  }
  return '${(bytes / (1024 * 1024 * 1024)).toStringAsFixed(1)} GB';
}

class _RemoteFilePickerSheetWrapper extends StatelessWidget {
  const _RemoteFilePickerSheetWrapper({
    required this.listProvider,
    this.createFolderProvider,
    this.favoriteApi,
    this.selectionMode = RemoteFileSelectionMode.multiple,
    this.pickTarget = RemoteFilePickTarget.files,
    this.allowedExtensions,
    this.showHidden = false,
    this.title,
    this.rootLabel = 'remote_file_picker_root_label',
    this.initialPath,
    this.storageKey,
    this.uploadBaseUrl,
  });

  final RemoteFileListProvider listProvider;
  final RemoteCreateFolderProvider? createFolderProvider;
  final UserFavoritePathService? favoriteApi;
  final RemoteFileSelectionMode selectionMode;
  final RemoteFilePickTarget pickTarget;
  final List<String>? allowedExtensions;
  final bool showHidden;
  final String? title;
  final String rootLabel;
  final String? initialPath;
  final String? storageKey;
  final String? uploadBaseUrl;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SizedBox(
      height: MediaQuery.of(context).size.height * 0.78,
      child: Column(
        children: [
          Container(
            width: 36,
            height: 4,
            margin: const EdgeInsets.only(top: 8, bottom: 4),
            decoration: BoxDecoration(
              color: theme.colorScheme.outline.withValues(alpha: 0.3),
              borderRadius: BorderRadius.circular(2),
            ),
          ),
          Expanded(
            child: RemoteFilePicker(
              listProvider: listProvider,
              createFolderProvider: createFolderProvider,
              favoriteApi: favoriteApi,
              selectionMode: selectionMode,
              pickTarget: pickTarget,
              allowedExtensions: allowedExtensions,
              showHidden: showHidden,
              title:
                  title ??
                  (pickTarget == RemoteFilePickTarget.directories
                      ? 'remote_file_picker_title_dir'.tr
                      : 'remote_file_picker_title'.tr),
              rootLabel: rootLabel,
              initialPath: initialPath,
              storageKey: storageKey,
              uploadBaseUrl: uploadBaseUrl,
              paddingBottom: MediaQuery.of(context).padding.bottom,
              onConfirm: (result) => Navigator.of(context).pop(result),
              onCancel: () => Navigator.of(context).pop(),
            ),
          ),
        ],
      ),
    );
  }
}

class _RemoteFilePickerDialogWrapper extends StatelessWidget {
  const _RemoteFilePickerDialogWrapper({
    required this.listProvider,
    this.createFolderProvider,
    this.favoriteApi,
    this.selectionMode = RemoteFileSelectionMode.multiple,
    this.pickTarget = RemoteFilePickTarget.files,
    this.allowedExtensions,
    this.showHidden = false,
    this.title,
    this.rootLabel = 'remote_file_picker_root_label',
    this.initialPath,
    this.storageKey,
    this.uploadBaseUrl,
  });

  final RemoteFileListProvider listProvider;
  final RemoteCreateFolderProvider? createFolderProvider;
  final UserFavoritePathService? favoriteApi;
  final RemoteFileSelectionMode selectionMode;
  final RemoteFilePickTarget pickTarget;
  final List<String>? allowedExtensions;
  final bool showHidden;
  final String? title;
  final String rootLabel;
  final String? initialPath;
  final String? storageKey;
  final String? uploadBaseUrl;

  @override
  Widget build(BuildContext context) {
    return RemoteFilePicker(
      listProvider: listProvider,
      createFolderProvider: createFolderProvider,
      favoriteApi: favoriteApi,
      selectionMode: selectionMode,
      pickTarget: pickTarget,
      allowedExtensions: allowedExtensions,
      showHidden: showHidden,
      title: title ?? 'remote_file_picker_title'.tr,
      rootLabel: rootLabel,
      initialPath: initialPath,
      storageKey: storageKey,
      uploadBaseUrl: uploadBaseUrl,
      onConfirm: (result) => Navigator.of(context).pop(result),
      onCancel: () => Navigator.of(context).pop(),
    );
  }
}

enum _UploadSource { gallery, files }

class _UploadCandidate {
  const _UploadCandidate({
    required this.name,
    required this.size,
    required this.stream,
  });

  final String name;
  final int size;
  final Stream<List<int>> stream;
}
