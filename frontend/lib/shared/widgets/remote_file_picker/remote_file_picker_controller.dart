import 'dart:async';
import 'dart:collection';

import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'remote_file_picker_model.dart';

class _PathEntry {
  _PathEntry({this.id, required this.name});

  final String? id;
  final String name;
}

class RemoteFilePickerController extends ChangeNotifier {
  RemoteFilePickerController({
    required this.listProvider,
    this.createFolderProvider,
    this.selectionMode = RemoteFileSelectionMode.multiple,
    this.pickTarget = RemoteFilePickTarget.files,
    bool showHidden = false,
    this.allowedExtensions,
    this.rootLabel = 'root',
    this.initialPath,
    this.storageKey,
  }) : _showHidden = showHidden;

  final RemoteFileListProvider listProvider;
  final RemoteCreateFolderProvider? createFolderProvider;
  final RemoteFileSelectionMode selectionMode;
  final RemoteFilePickTarget pickTarget;
  final List<String>? allowedExtensions;
  final String rootLabel;
  final String? initialPath;
  final String? storageKey;
  bool _showHidden;
  bool get showHidden => _showHidden;
  bool get isMultiSelect => selectionMode == RemoteFileSelectionMode.multiple;
  bool get canSelectDirectory =>
      pickTarget == RemoteFilePickTarget.directories ||
      pickTarget == RemoteFilePickTarget.both;
  bool get canSelectFile =>
      pickTarget == RemoteFilePickTarget.files ||
      pickTarget == RemoteFilePickTarget.both;

  Future<void> toggleShowHidden() async {
    _showHidden = !_showHidden;
    if (_pathStack.isEmpty) {
      await loadRoot();
      return;
    }
    final last = _pathStack.last;
    await _loadDirectory(last.id, last.name, skipPush: true);
  }

  List<RemoteFileNode> _items = [];
  List<RemoteFileNode> get items => UnmodifiableListView(_items);

  final Set<RemoteFileNode> _selectedItems = {};
  Set<RemoteFileNode> get selectedItems => UnmodifiableSetView(_selectedItems);

  final List<_PathEntry> _pathStack = [];
  List<({String? id, String name})> get pathStack =>
      UnmodifiableListView(_pathStack.map((e) => (id: e.id, name: e.name)));

  bool _isLoading = false;
  bool get isLoading => _isLoading;

  String? _error;
  String? get error => _error;

  int _loadVersion = 0;

  /// 服务端最近一次返回的 currentPath（根目录时 id 为 null，用此作为上传目标路径的兜底）。
  String? _serverCurrentPath;
  String? get serverCurrentPath => _serverCurrentPath;

  /// 当前列表所在机器的名字（connector 返回）。收藏时写入收藏项，
  /// 收藏夹默认按此过滤。为空表示尚未取到或 agent 未提供。
  String? _currentMachineName;
  String? get currentMachineName => _currentMachineName;

  bool get canNavigateBack => _pathStack.length > 1;

  /// Whether the current directory has a parent we can navigate to.
  bool get canNavigateUp {
    if (_pathStack.isEmpty) return false;
    final id = _pathStack.last.id;
    if (id == null) return false;
    return _parentPath(id) != null;
  }

  String? get currentParentId => _pathStack.isEmpty ? null : _pathStack.last.id;

  RemoteFileNode? get currentDirectoryNode {
    if (_pathStack.isEmpty) return null;
    final entry = _pathStack.last;
    return RemoteFileNode(
      id: entry.id ?? '',
      name: entry.name,
      isDirectory: true,
    );
  }

  Future<void> loadRoot() async {
    _pathStack.clear();
    _selectedItems.clear();
    // 优先用传入的 initialPath，其次从 SharedPreferences 读上次记忆路径
    String? startPath = initialPath;
    if ((startPath == null || startPath.isEmpty) && storageKey != null) {
      final prefs = await SharedPreferences.getInstance();
      startPath = prefs.getString(storageKey!);
    }
    if (startPath != null && startPath.isNotEmpty) {
      try {
        await _loadDirectory(
          startPath,
          _pathBasename(startPath) ?? rootLabel,
        ).timeout(const Duration(seconds: 8));
      } on TimeoutException {
        _isLoading = false;
        _error = 'Loading directory timed out';
        notifyListeners();
      }
      // Show the error to the user — do NOT silently fall back to root.
    } else {
      await _loadDirectory(null, rootLabel);
    }
  }

  /// 单选模式下，跳转目录时清掉之前的选中项。
  /// 避免出现“人已进入新目录、选中状态却仍残留着另一个目录”导致提交错对象。
  /// 多选模式保留选中（可能需要跨目录多选），刷新类操作不走这些跳转方法因此不受影响。
  void _clearSelectionForNavigation() {
    if (isMultiSelect || _selectedItems.isEmpty) return;
    _selectedItems.clear();
  }

  Future<void> navigateTo(RemoteFileNode directory) async {
    assert(directory.isDirectory);
    _clearSelectionForNavigation();
    await _loadDirectory(directory.id, directory.name);
  }

  /// Navigate directly to a specific directory path, replacing the current
  /// navigation stack. Used when jumping from favorites to a browsed path.
  Future<void> loadPath(String dirPath, String dirName) async {
    _pathStack.clear();
    _selectedItems.clear();
    await _loadDirectory(dirPath, dirName);
  }

  /// Navigate to the parent of the current directory.
  Future<void> navigateUp() async {
    if (_pathStack.isEmpty) return;
    final currentPath = _pathStack.last.id;
    if (currentPath == null) return;
    final parentPath = _parentPath(currentPath);
    if (parentPath == null) return;
    _clearSelectionForNavigation();
    _pathStack.removeLast();
    await _loadDirectory(
      parentPath,
      _pathBasename(parentPath) ?? '/',
      skipPush: true,
    );
  }

  Future<void> navigateBack() async {
    if (_pathStack.length <= 1) return;
    _clearSelectionForNavigation();
    _pathStack.removeLast();
    final entry = _pathStack.last;
    await _loadDirectory(entry.id, entry.name, skipPush: true);
  }

  Future<void> navigateToIndex(int index) async {
    if (index < 0 || index >= _pathStack.length) return;
    if (index == _pathStack.length - 1) return;
    _clearSelectionForNavigation();
    final target = _pathStack[index];
    _pathStack.removeRange(index + 1, _pathStack.length);
    await _loadDirectory(target.id, target.name, skipPush: true);
  }

  void toggleSelect(RemoteFileNode node) {
    if ((node.isDirectory && !canSelectDirectory) ||
        (!node.isDirectory && !canSelectFile)) {
      return;
    }
    if (_selectedItems.contains(node)) {
      _selectedItems.remove(node);
    } else {
      if (!isMultiSelect) _selectedItems.clear();
      _selectedItems.add(node);
    }
    notifyListeners();
  }

  void clearSelection() {
    if (_selectedItems.isEmpty) return;
    _selectedItems.clear();
    notifyListeners();
  }

  RemoteFilePickerResult confirm() {
    return RemoteFilePickerResult(
      selectedFiles: List.unmodifiable(_selectedItems),
    );
  }

  bool get canCreateFolder => createFolderProvider != null;

  bool _isCreatingFolder = false;
  bool get isCreatingFolder => _isCreatingFolder;

  Future<RemoteFileNode?> createFolder(String name) async {
    if (createFolderProvider == null || _isCreatingFolder) return null;
    final trimmed = name.trim();
    if (trimmed.isEmpty) return null;

    _isCreatingFolder = true;
    notifyListeners();

    try {
      final parentId = _pathStack.isEmpty ? null : _pathStack.last.id;
      final folder = await createFolderProvider!(parentId, trimmed);
      await _loadDirectory(parentId, _pathStack.last.name, skipPush: true);
      final match = _items.where((n) => n.id == folder.id);
      if (match.isNotEmpty && canSelectDirectory) {
        _selectedItems.clear();
        _selectedItems.add(match.first);
      }
      return folder;
    } catch (e) {
      _error = e.toString();
      notifyListeners();
      return null;
    } finally {
      _isCreatingFolder = false;
      notifyListeners();
    }
  }

  /// 跳转到文件系统根（Windows 盘符列表 / Unix `/`）。哨兵 `::root` 由 connector 解析。
  Future<void> goToFilesystemRoot() async {
    _pathStack.clear();
    _selectedItems.clear();
    await _loadDirectory('::root', rootLabel, adoptServerPath: true);
  }

  /// 跳转到当前用户的 home 目录。哨兵 `::home` 由 connector 解析为真实路径。
  Future<void> goToHome() async {
    _pathStack.clear();
    _selectedItems.clear();
    await _loadDirectory('::home', 'Home', adoptServerPath: true);
  }

  Future<void> _loadDirectory(
    String? parentId,
    String name, {
    bool skipPush = false,
    bool adoptServerPath = false,
  }) async {
    final version = ++_loadVersion;
    _isLoading = true;
    _error = null;
    _items = [];
    if (!skipPush || _pathStack.isEmpty) {
      _pathStack.add(_PathEntry(id: parentId, name: name));
    }
    notifyListeners();

    try {
      final result = await listProvider(
        parentId,
        RemoteFileListQuery(
          showHidden: _showHidden,
          allowedExtensions: allowedExtensions,
        ),
      );
      if (version != _loadVersion) return;
      _items = _sortItems(_filterByExtension(result.files));
      // 记录当前机器名（connector 每次列目录都返回同一台机器）。
      if (result.machineName != null && result.machineName!.isNotEmpty) {
        _currentMachineName = result.machineName;
      }
      // currentPath 仅用于修正面包屑显示名称，id 始终以请求时的 parentId 为准。
      // agent 有时会返回父目录路径或其他不一致的值，若用其覆盖 id 会导致
      // "使用当前目录"返回错误目录（偶发，取决于 agent 实现）。
      if (result.currentPath != null && result.currentPath!.isNotEmpty) {
        _serverCurrentPath = result.currentPath;
      }
      if (result.currentPath != null &&
          result.currentPath!.isNotEmpty &&
          _pathStack.isNotEmpty) {
        final correctedName =
            _pathBasename(result.currentPath!) ??
            _pathBasename(parentId) ??
            name;
        // 哨兵跳转（::root/::home）时采用 connector 回传的真实路径作为 id，
        // 否则后续返回上级、"使用当前目录"会拿到哨兵字符串而失效。
        final correctedId = adoptServerPath ? result.currentPath : parentId;
        _pathStack.removeLast();
        _pathStack.add(_PathEntry(id: correctedId, name: correctedName));
      }
      _isLoading = false;
      // 保存当前目录路径供下次打开时恢复
      if (storageKey != null && _pathStack.isNotEmpty) {
        final currentId = _pathStack.last.id;
        if (currentId != null &&
            currentId.isNotEmpty &&
            !currentId.startsWith('::')) {
          SharedPreferences.getInstance().then(
            (prefs) => prefs.setString(storageKey!, currentId),
          );
        }
      }
      notifyListeners();
    } catch (e) {
      if (version != _loadVersion) return;
      _isLoading = false;
      _error = e.toString();
      notifyListeners();
    }
  }

  RemoteFileSortMode _sortMode = RemoteFileSortMode.nameAsc;
  RemoteFileSortMode get sortMode => _sortMode;

  /// 切换排序模式（字母升序 ↔ 时间降序），就地重排已加载项，无需重新请求。
  void cycleSortMode() {
    _sortMode = _sortMode == RemoteFileSortMode.nameAsc
        ? RemoteFileSortMode.timeDesc
        : RemoteFileSortMode.nameAsc;
    _items = _sortItems(_items);
    notifyListeners();
  }

  List<RemoteFileNode> _sortItems(List<RemoteFileNode> items) {
    final cmp = _comparatorForMode();
    final dirs = items.where((n) => n.isDirectory).toList()..sort(cmp);
    final files = items.where((n) => !n.isDirectory).toList()..sort(cmp);
    return [...dirs, ...files];
  }

  Comparator<RemoteFileNode> _comparatorForMode() {
    int byName(RemoteFileNode a, RemoteFileNode b) =>
        a.name.toLowerCase().compareTo(b.name.toLowerCase());
    switch (_sortMode) {
      case RemoteFileSortMode.nameAsc:
        return byName;
      case RemoteFileSortMode.timeDesc:
        return (a, b) {
          final at = a.modifiedAt;
          final bt = b.modifiedAt;
          // 缺失时间的项排在后面；都缺失则回退到名称升序，保证排序稳定可预期。
          if (at == null && bt == null) return byName(a, b);
          if (at == null) return 1;
          if (bt == null) return -1;
          final c = bt.compareTo(at); // 最新在前
          return c != 0 ? c : byName(a, b); // 同一时间戳回退名称，避免顺序抖动
        };
    }
  }

  List<RemoteFileNode> _filterByExtension(List<RemoteFileNode> items) {
    final extSet = _normalizeExtensions(allowedExtensions);
    if (extSet == null) return items;
    return items.where((node) {
      if (node.isDirectory) return true;
      return _matchesExtension(node.name, extSet);
    }).toList();
  }

  static Set<String>? _normalizeExtensions(List<String>? extensions) {
    if (extensions == null || extensions.isEmpty) return null;
    final normalized = extensions
        .map((e) => e.trim().toLowerCase())
        .where((e) => e.isNotEmpty)
        .map((e) => e.startsWith('.') ? e : '.$e')
        .toSet();
    return normalized.isEmpty ? null : normalized;
  }

  static bool _matchesExtension(String fileName, Set<String> extSet) {
    final dotIndex = fileName.lastIndexOf('.');
    if (dotIndex <= 0 || dotIndex >= fileName.length - 1) return false;
    return extSet.contains(fileName.substring(dotIndex).toLowerCase());
  }

  Future<void> retry() async {
    if (_pathStack.isEmpty) {
      await loadRoot();
      return;
    }
    final last = _pathStack.last;
    await _loadDirectory(last.id, last.name, skipPush: true);
  }

  Future<void> goToRoot() async {
    _pathStack.clear();
    _selectedItems.clear();
    // 清除记忆路径，下次打开从根目录开始
    if (storageKey != null) {
      SharedPreferences.getInstance().then(
        (prefs) => prefs.remove(storageKey!),
      );
    }
    await _loadDirectory(null, rootLabel);
  }

  /// Returns the parent path, or null if already at root.
  /// Works cross-platform: Unix `/`, Windows `C:\`.
  static String? _parentPath(String path) {
    // Normalize backslashes to forward slashes for consistent handling.
    final normalized = path.replaceAll('\\', '/');
    // Remove trailing slash (except root).
    final trimmed = normalized.endsWith('/') && normalized.length > 1
        ? normalized.substring(0, normalized.length - 1)
        : normalized;
    if (trimmed.isEmpty) return null;
    // Root on Unix.
    if (trimmed == '/') return null;
    // Root on Windows: C: D: etc.
    if (trimmed.length == 2 && trimmed[1] == ':') return null;
    if (trimmed.length == 3 &&
        trimmed.endsWith(':/') &&
        trimmed[0].toUpperCase() != trimmed[0].toLowerCase()) {
      return null;
    }
    final lastSlash = trimmed.lastIndexOf('/');
    if (lastSlash <= 0) {
      // No more parents (e.g. "C:" on Windows).
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
}
