import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/widgets/remote_file_picker/remote_file_picker_controller.dart';
import 'package:grix/shared/widgets/remote_file_picker/remote_file_picker_model.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  group('RemoteFilePickerController', () {
    test('show hidden toggle updates query and reloads directory', () async {
      final queries = <RemoteFileListQuery>[];
      final parentIds = <String?>[];
      final controller = RemoteFilePickerController(
        listProvider: (parentId, query) async {
          parentIds.add(parentId);
          queries.add(query);
          return const RemoteFileListResult(files: []);
        },
      );

      await controller.loadRoot();
      expect(queries.length, 1);
      expect(queries.first.showHidden, false);

      await controller.toggleShowHidden();
      expect(queries.length, 2);
      expect(queries.last.showHidden, true);
      expect(parentIds.last, isNull);

      await controller.toggleShowHidden();
      expect(queries.length, 3);
      expect(queries.last.showHidden, false);
      expect(parentIds.last, isNull);
    });

    test('show hidden toggle keeps current directory identity', () async {
      const dirA = RemoteFileNode(id: '/dirA', name: 'dirA', isDirectory: true);
      final parentIds = <String?>[];
      final controller = RemoteFilePickerController(
        listProvider: (parentId, query) async {
          parentIds.add(parentId);
          return const RemoteFileListResult(files: [dirA]);
        },
      );

      await controller.loadRoot();
      await controller.navigateTo(dirA);
      expect(controller.currentDirectoryNode?.id, '/dirA');

      await controller.toggleShowHidden();

      expect(parentIds.last, '/dirA');
      expect(controller.currentDirectoryNode?.id, '/dirA');
      expect(controller.pathStack.map((entry) => entry.id), [null, '/dirA']);
    });

    test('retry keeps current directory identity', () async {
      const dirA = RemoteFileNode(id: '/dirA', name: 'dirA', isDirectory: true);
      final parentIds = <String?>[];
      final controller = RemoteFilePickerController(
        listProvider: (parentId, query) async {
          parentIds.add(parentId);
          return const RemoteFileListResult(files: [dirA]);
        },
      );

      await controller.loadRoot();
      await controller.navigateTo(dirA);
      expect(controller.currentDirectoryNode?.id, '/dirA');

      await controller.retry();

      expect(parentIds.last, '/dirA');
      expect(controller.currentDirectoryNode?.id, '/dirA');
      expect(controller.pathStack.map((entry) => entry.id), [null, '/dirA']);
    });

    test('allowedExtensions filter is case-insensitive', () async {
      final controller = RemoteFilePickerController(
        allowedExtensions: const ['MD', '.TxT'],
        listProvider: (parentId, query) async {
          return const RemoteFileListResult(
            files: [
              RemoteFileNode(id: '/a.md', name: 'a.md', isDirectory: false),
              RemoteFileNode(id: '/b.MD', name: 'b.MD', isDirectory: false),
              RemoteFileNode(id: '/c.txt', name: 'c.txt', isDirectory: false),
              RemoteFileNode(id: '/d.csv', name: 'd.csv', isDirectory: false),
              RemoteFileNode(id: '/dir', name: 'dir', isDirectory: true),
            ],
          );
        },
      );

      await controller.loadRoot();
      final names = controller.items.map((e) => e.name).toList();
      expect(names, containsAll(['a.md', 'b.MD', 'c.txt', 'dir']));
      expect(names, isNot(contains('d.csv')));
    });

    test(
      'single + directories mode only keeps one selected directory',
      () async {
        const dirA = RemoteFileNode(
          id: '/dirA',
          name: 'dirA',
          isDirectory: true,
        );
        const dirB = RemoteFileNode(
          id: '/dirB',
          name: 'dirB',
          isDirectory: true,
        );
        const fileA = RemoteFileNode(
          id: '/a.md',
          name: 'a.md',
          isDirectory: false,
        );

        final controller = RemoteFilePickerController(
          selectionMode: RemoteFileSelectionMode.single,
          pickTarget: RemoteFilePickTarget.directories,
          listProvider: (parentId, query) async =>
              const RemoteFileListResult(files: [dirA, dirB, fileA]),
        );

        await controller.loadRoot();
        controller.toggleSelect(fileA);
        expect(controller.selectedItems, isEmpty);

        controller.toggleSelect(dirA);
        expect(controller.selectedItems.length, 1);
        expect(controller.selectedItems.first.id, dirA.id);

        controller.toggleSelect(dirB);
        expect(controller.selectedItems.length, 1);
        expect(controller.selectedItems.first.id, dirB.id);
      },
    );

    test('单选模式下进入新目录会清掉之前的选中项', () async {
      const dirA = RemoteFileNode(id: '/dirA', name: 'dirA', isDirectory: true);
      const dirB = RemoteFileNode(id: '/dirB', name: 'dirB', isDirectory: true);

      final controller = RemoteFilePickerController(
        selectionMode: RemoteFileSelectionMode.single,
        pickTarget: RemoteFilePickTarget.directories,
        listProvider: (parentId, query) async =>
            const RemoteFileListResult(files: [dirA, dirB]),
      );

      await controller.loadRoot();
      controller.toggleSelect(dirA);
      expect(controller.selectedItems.first.id, dirA.id);

      // 进入另一个目录后，残留的选中项必须被清空，避免提交到错误目录。
      await controller.navigateTo(dirB);
      expect(controller.selectedItems, isEmpty);
    });

    test('单选模式下返回上级会清掉之前的选中项', () async {
      const dirA = RemoteFileNode(id: '/dirA', name: 'dirA', isDirectory: true);
      const sub = RemoteFileNode(
        id: '/dirA/sub',
        name: 'sub',
        isDirectory: true,
      );

      final controller = RemoteFilePickerController(
        selectionMode: RemoteFileSelectionMode.single,
        pickTarget: RemoteFilePickTarget.directories,
        listProvider: (parentId, query) async =>
            const RemoteFileListResult(files: [dirA, sub]),
      );

      await controller.loadRoot();
      await controller.navigateTo(dirA);
      controller.toggleSelect(sub);
      expect(controller.selectedItems.first.id, sub.id);

      await controller.navigateBack();
      expect(controller.selectedItems, isEmpty);
    });

    test('agent 返回错误 currentPath 时 id 不被覆盖（进入目录和面包屑后退两种场景）', () async {
      // 回归测试：无论是正常进入目录还是面包屑后退，
      // 只要 agent 的 current_path 与请求路径不一致，前端都不应采用它作为 id。
      const dirA = RemoteFileNode(id: '/a', name: 'a', isDirectory: true);
      const dirB = RemoteFileNode(id: '/a/b', name: 'b', isDirectory: true);
      const dirC = RemoteFileNode(id: '/a/b/c', name: 'c', isDirectory: true);

      final controller = RemoteFilePickerController(
        pickTarget: RemoteFilePickTarget.directories,
        listProvider: (parentId, query) async {
          // agent bug：每次都把 current_path 返回为父目录路径
          String? wrongPath;
          if (parentId == '/a/b') wrongPath = '/a';
          if (parentId == '/a/b/c') wrongPath = '/a/b';
          return RemoteFileListResult(files: const [], currentPath: wrongPath);
        },
      );

      await controller.loadRoot();

      // 场景1：正常进入目录时 id 不被错误 currentPath 覆盖
      await controller.navigateTo(dirA);
      await controller.navigateTo(dirB);
      expect(
        controller.currentDirectoryNode?.id,
        '/a/b',
        reason: '进入 /a/b 后，id 不应被 agent 返回的错误 currentPath(/a) 覆盖',
      );

      // 场景2：面包屑后退时同样受保护
      await controller.navigateTo(dirC);
      await controller.navigateToIndex(2); // 面包屑回到 /a/b
      expect(
        controller.currentDirectoryNode?.id,
        '/a/b',
        reason: '面包屑后退到 /a/b 后，id 不应被 agent 返回的错误 currentPath 覆盖',
      );
    });

    test('currentMachineName 跟随列表返回的机器名，空值不覆盖已有值', () async {
      var machine = 'mac-studio';
      final controller = RemoteFilePickerController(
        listProvider: (parentId, query) async =>
            RemoteFileListResult(files: const [], machineName: machine),
      );

      // 初始未加载时为空
      expect(controller.currentMachineName, isNull);

      await controller.loadRoot();
      expect(controller.currentMachineName, 'mac-studio');

      // 后续返回空机器名（如 agent 未提供）不应抹掉已知机器名
      machine = '';
      await controller.retry();
      expect(controller.currentMachineName, 'mac-studio');

      // 返回新机器名则更新
      machine = 'linux-box';
      await controller.retry();
      expect(controller.currentMachineName, 'linux-box');
    });

    test('多选模式下跳转目录保留选中（不回归跨目录多选）', () async {
      const dirA = RemoteFileNode(id: '/dirA', name: 'dirA', isDirectory: true);
      const dirB = RemoteFileNode(id: '/dirB', name: 'dirB', isDirectory: true);

      final controller = RemoteFilePickerController(
        selectionMode: RemoteFileSelectionMode.multiple,
        pickTarget: RemoteFilePickTarget.directories,
        listProvider: (parentId, query) async =>
            const RemoteFileListResult(files: [dirA, dirB]),
      );

      await controller.loadRoot();
      controller.toggleSelect(dirA);
      expect(controller.selectedItems.length, 1);

      await controller.navigateTo(dirB);
      expect(controller.selectedItems.length, 1);
      expect(controller.selectedItems.first.id, dirA.id);
    });

    test('记忆路径按 storageKey 隔离：不同 agent 的 key 不串用彼此上次路径', () async {
      // 回归：固定 storageKey 会让不同机器的 agent 共用同一记忆路径——
      // 先操作 A 机器 agent 留下其路径，切到 B 机器 agent 时被起始加载到不
      // 存在的 A 路径，列目录失败/取不到机器名，收藏夹便无法默认过滤到 B。
      // 修复后 key 含 agentId，各机器独立记忆，互不串台。
      TestWidgetsFlutterBinding.ensureInitialized();
      SharedPreferences.setMockInitialValues({
        'remote_file_picker_last_path_attach_agentA': '/home/userA/projects',
      });

      // 用旧的固定式 key（即等同 A 的 key）会读到 A 的路径——证明本测试能抓到 bug。
      final leaked = <String?>[];
      final shared = RemoteFilePickerController(
        storageKey: 'remote_file_picker_last_path_attach_agentA',
        listProvider: (parentId, query) async {
          leaked.add(parentId);
          return const RemoteFileListResult(files: [], machineName: 'HostA');
        },
      );
      await shared.loadRoot();
      expect(
        leaked.first,
        '/home/userA/projects',
        reason: '共用同一 key 时会串台到上一台机器的路径（修复前行为）',
      );

      // 修复后：B 机器 agent 用含自身 agentId 的独立 key，从根开始，不串 A 的路径。
      final isolated = <String?>[];
      final ctrlB = RemoteFilePickerController(
        storageKey: 'remote_file_picker_last_path_attach_agentB',
        listProvider: (parentId, query) async {
          isolated.add(parentId);
          return const RemoteFileListResult(files: [], machineName: 'HostB');
        },
      );
      await ctrlB.loadRoot();
      expect(
        isolated.first,
        isNull,
        reason: '按 agent 区分 key 后，B 从根目录开始，不串用 A 的路径',
      );
      expect(ctrlB.currentMachineName, 'HostB');
    });

    test(
      'list and create errors drop Exception prefix and skip Dio dumps',
      () async {
        final controller = RemoteFilePickerController(
          createFolderProvider: (parentId, name) async {
            throw Exception('im_create_folder_timeout');
          },
          listProvider: (parentId, query) async {
            throw Exception('im_file_list_timeout');
          },
        );

        await controller.loadRoot();
        expect(controller.error, 'im_file_list_timeout');

        await controller.createFolder('docs');
        expect(controller.error, 'im_create_folder_timeout');
      },
    );

    test('goToHome uses i18n key until server path arrives', () async {
      final controller = RemoteFilePickerController(
        listProvider: (parentId, query) async {
          if (parentId == '::home') {
            return const RemoteFileListResult(files: []);
          }
          return const RemoteFileListResult(files: []);
        },
      );

      await controller.goToHome();
      expect(controller.pathStack.last.name, 'remote_file_picker_go_home');
    });
  });

  group('remoteFilePickerErrorText', () {
    test('maps Dio and strips Exception prefix', () {
      expect(
        remoteFilePickerErrorText(Exception('im_file_list_timeout')),
        'im_file_list_timeout',
      );
      expect(
        remoteFilePickerErrorText(
          DioException(
            requestOptions: RequestOptions(path: '/'),
            type: DioExceptionType.connectionTimeout,
          ),
        ),
        'remote_file_picker_err_timeout',
      );
      expect(
        remoteFilePickerErrorText(StateError('boom')),
        'remote_file_picker_err_network',
      );
    });
  });
}
