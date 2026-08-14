import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';

import 'package:grix/app/translations/app_translations.dart';
import 'package:grix/data/providers/user_favorite_path_service.dart';
import 'package:grix/shared/widgets/remote_file_picker/remote_file_picker.dart';

/// 内存版收藏服务，便于测试文件浏览里的标星交互。
class _FakeFavoriteService extends UserFavoritePathService {
  _FakeFavoriteService();

  final List<FavoritePathItem> items = [];
  int _seq = 0;

  @override
  Future<List<FavoritePathItem>> list() async => List.of(items);

  @override
  Future<FavoritePathItem?> add(
    String path,
    String name,
    bool isDirectory, {
    String machineName = '',
  }) async {
    final item = FavoritePathItem(
      id: 'fav${_seq++}',
      path: path,
      name: name,
      isDirectory: isDirectory,
      machineName: machineName,
      createdAt: '',
    );
    items.add(item);
    return item;
  }

  @override
  Future<bool> delete(String id) async {
    items.removeWhere((e) => e.id == id);
    return true;
  }
}

void main() {
  Future<void> pumpPicker(
    WidgetTester tester, {
    required RemoteFilePickTarget pickTarget,
    required ValueChanged<RemoteFilePickerResult> onConfirm,
  }) async {
    tester.view.physicalSize = const Size(2000, 1400);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: Center(
            child: SizedBox(
              width: 1600,
              height: 900,
              child: RemoteFilePicker(
                selectionMode: RemoteFileSelectionMode.single,
                pickTarget: pickTarget,
                onConfirm: onConfirm,
                listProvider: (parentId, query) async =>
                    const RemoteFileListResult(
                      files: [
                        RemoteFileNode(
                          id: '/dirA',
                          name: 'dirA',
                          isDirectory: true,
                        ),
                        RemoteFileNode(
                          id: '/readme.md',
                          name: 'readme.md',
                          isDirectory: false,
                        ),
                      ],
                    ),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('仅目录模式下点击文件不会触发自动确认提交', (WidgetTester tester) async {
    RemoteFilePickerResult? confirmed;
    await pumpPicker(
      tester,
      pickTarget: RemoteFilePickTarget.directories,
      onConfirm: (result) => confirmed = result,
    );

    // 点击文件行：仅目录模式下应被忽略，绝不能自动确认。
    await tester.tap(find.text('readme.md'));
    await tester.pumpAndSettle();

    expect(confirmed, isNull, reason: '仅目录模式下点击文件不应触发 onConfirm（避免提交到错误目录）');
  });

  testWidgets('选文件模式下单选点击文件不会自动确认', (WidgetTester tester) async {
    RemoteFilePickerResult? confirmed;
    await pumpPicker(
      tester,
      pickTarget: RemoteFilePickTarget.files,
      onConfirm: (result) => confirmed = result,
    );

    await tester.tap(find.text('readme.md'));
    await tester.pumpAndSettle();

    expect(confirmed, isNull, reason: '单选文件也必须显式点击确认，不能点击即提交');

    await tester.tap(find.text('确认'));
    await tester.pumpAndSettle();

    expect(confirmed, isNotNull);
    expect(confirmed!.selectedFiles.single.id, '/readme.md');
  });

  testWidgets('有 tailnet 地址时显示下载按钮，未选中禁用、选中后启用', (WidgetTester tester) async {
    tester.view.physicalSize = const Size(2000, 1400);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: Center(
            child: SizedBox(
              width: 1600,
              height: 900,
              child: RemoteFilePicker(
                selectionMode: RemoteFileSelectionMode.multiple,
                pickTarget: RemoteFilePickTarget.both,
                uploadBaseUrl: 'http://100.64.0.1:12345',
                onConfirm: (_) {},
                listProvider: (parentId, query) async =>
                    const RemoteFileListResult(
                      files: [
                        RemoteFileNode(
                          id: '/dirA',
                          name: 'dirA',
                          isDirectory: true,
                        ),
                        RemoteFileNode(
                          id: '/readme.md',
                          name: 'readme.md',
                          isDirectory: false,
                        ),
                      ],
                    ),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final downloadBtn = find.widgetWithText(OutlinedButton, '下载');
    expect(downloadBtn, findsOneWidget, reason: '配置 tailnet 地址后应出现下载按钮');
    // 未选中任何项时禁用，避免空下载。
    expect(
      tester.widget<OutlinedButton>(downloadBtn).onPressed,
      isNull,
      reason: '未选择文件/目录时下载按钮应禁用',
    );

    // 选中一个文件后，下载按钮启用（不点击，避免触发真实网络）。
    await tester.tap(find.text('readme.md'));
    await tester.pumpAndSettle();
    expect(
      tester.widget<OutlinedButton>(downloadBtn).onPressed,
      isNotNull,
      reason: '选中后下载按钮应启用',
    );
  });

  testWidgets('支持的 Tailnet 文本文件显示查看按钮，非文本文件不显示', (WidgetTester tester) async {
    tester.view.physicalSize = const Size(2000, 1400);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final favoriteService = _FakeFavoriteService();
    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: SizedBox(
            width: 1600,
            height: 900,
            child: RemoteFilePicker(
              selectionMode: RemoteFileSelectionMode.multiple,
              pickTarget: RemoteFilePickTarget.both,
              uploadBaseUrl: 'http://100.64.0.1:12345',
              favoriteApi: favoriteService,
              onConfirm: (_) {},
              listProvider: (parentId, query) async =>
                  const RemoteFileListResult(
                    files: [
                      RemoteFileNode(
                        id: '/readme.md',
                        name: 'readme.md',
                        isDirectory: false,
                        mimeType: 'text/markdown',
                      ),
                      RemoteFileNode(
                        id: '/photo.png',
                        name: 'photo.png',
                        isDirectory: false,
                        mimeType: 'image/png',
                      ),
                    ],
                  ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    final previewButton = find.byTooltip('查看文本内容');
    expect(previewButton, findsOneWidget, reason: '只应为支持的文本文件显示查看按钮');
    final readmeRow = find.widgetWithText(ListTile, 'readme.md');
    final eye = find.descendant(
      of: readmeRow,
      matching: find.byIcon(Icons.visibility_outlined),
    );
    final star = find.descendant(
      of: readmeRow,
      matching: find.byIcon(Icons.star_border_rounded),
    );
    final checkbox = find.descendant(
      of: readmeRow,
      matching: find.byType(Checkbox),
    );
    expect(eye, findsOneWidget);
    expect(star, findsOneWidget);
    expect(checkbox, findsOneWidget);
    expect(tester.getCenter(eye).dx, lessThan(tester.getCenter(star).dx));
    expect(tester.getCenter(star).dx, lessThan(tester.getCenter(checkbox).dx));
    expect(
      find.descendant(
        of: find.widgetWithText(ListTile, 'photo.png'),
        matching: find.byIcon(Icons.visibility_outlined),
      ),
      findsNothing,
    );
  });

  testWidgets('点击目录行的收藏星：收藏/取消都不应进入目录', (WidgetTester tester) async {
    tester.view.physicalSize = const Size(2000, 1400);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final fav = _FakeFavoriteService();
    // 记录文件列表的加载请求：若出现 '/dirA' 说明误进入了该目录。
    final loadedParents = <String?>[];

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: Center(
            child: SizedBox(
              width: 1600,
              height: 900,
              child: RemoteFilePicker(
                selectionMode: RemoteFileSelectionMode.single,
                pickTarget: RemoteFilePickTarget.files,
                onConfirm: (_) {},
                favoriteApi: fav,
                listProvider: (parentId, query) async {
                  loadedParents.add(parentId);
                  return const RemoteFileListResult(
                    files: [
                      RemoteFileNode(
                        id: '/dirA',
                        name: 'dirA',
                        isDirectory: true,
                      ),
                      RemoteFileNode(
                        id: '/readme.md',
                        name: 'readme.md',
                        isDirectory: false,
                      ),
                    ],
                  );
                },
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    Finder dirAStar(IconData icon) => find.descendant(
      of: find.widgetWithText(ListTile, 'dirA'),
      matching: find.byIcon(icon),
    );

    // 初始未收藏：dirA 行显示空心星。
    expect(dirAStar(Icons.star_border_rounded), findsOneWidget);

    // 点击空心星 → 收藏，且不应进入 dirA 目录。
    await tester.tap(dirAStar(Icons.star_border_rounded));
    await tester.pumpAndSettle();
    expect(fav.items.length, 1, reason: '点击空心星应添加收藏');
    expect(fav.items.single.path, '/dirA');
    expect(loadedParents.contains('/dirA'), isFalse, reason: '收藏点击不应触发进入目录');

    // 现在变实心星：再次点击 → 取消收藏，同样不应进入目录。
    expect(dirAStar(Icons.star_rounded), findsOneWidget);
    await tester.tap(dirAStar(Icons.star_rounded));
    await tester.pumpAndSettle();
    expect(fav.items, isEmpty, reason: '点击实心星应取消收藏');
    expect(loadedParents.contains('/dirA'), isFalse, reason: '取消收藏点击不应触发进入目录');
  });

  testWidgets('单选模式下收藏夹用单选钮而非勾选框，且最多选一个', (WidgetTester tester) async {
    tester.view.physicalSize = const Size(2000, 1400);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final fav = _FakeFavoriteService();
    // 预置两条目录收藏（机器名留空，匹配未知机器，确保可见）。
    fav.items.addAll(const [
      FavoritePathItem(
        id: 'f1',
        path: '/favA',
        name: 'favA',
        isDirectory: true,
        machineName: '',
        createdAt: '',
      ),
      FavoritePathItem(
        id: 'f2',
        path: '/favB',
        name: 'favB',
        isDirectory: true,
        machineName: '',
        createdAt: '',
      ),
    ]);

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: Center(
            child: SizedBox(
              width: 1600,
              height: 900,
              child: RemoteFilePicker(
                selectionMode: RemoteFileSelectionMode.single,
                pickTarget: RemoteFilePickTarget.directories,
                title: '选择目录',
                favoriteApi: fav,
                onConfirm: (_) {},
                listProvider: (parentId, query) async =>
                    const RemoteFileListResult(files: []),
              ),
            ),
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    // 标题栏的星标按钮切到收藏夹视图。
    await tester.tap(find.byIcon(Icons.star_rounded));
    await tester.pumpAndSettle();

    // 单选模式：收藏夹不应出现多选勾选框，应是圆圈单选钮。
    expect(find.byType(Checkbox), findsNothing, reason: '单选模式下收藏夹不应用 Checkbox');
    expect(
      find.byIcon(Icons.radio_button_unchecked_rounded),
      findsNWidgets(2),
      reason: '两条收藏应各显示一个空心单选钮',
    );

    Finder favRadio(String name, IconData icon) => find.descendant(
      of: find.widgetWithText(ListTile, name),
      matching: find.byIcon(icon),
    );

    // 选 favA → 仅 favA 被选中。
    await tester.tap(favRadio('favA', Icons.radio_button_unchecked_rounded));
    await tester.pumpAndSettle();
    expect(
      find.byIcon(Icons.check_circle_rounded),
      findsOneWidget,
      reason: '单选模式下应只有一个被选中',
    );
    expect(favRadio('favA', Icons.check_circle_rounded), findsOneWidget);

    // 再选 favB → 选中转移到 favB，favA 复位（仍只有一个被选）。
    await tester.tap(favRadio('favB', Icons.radio_button_unchecked_rounded));
    await tester.pumpAndSettle();
    expect(
      find.byIcon(Icons.check_circle_rounded),
      findsOneWidget,
      reason: '单选模式下选新的应清掉旧的，始终最多一个',
    );
    expect(favRadio('favB', Icons.check_circle_rounded), findsOneWidget);
    expect(
      favRadio('favA', Icons.radio_button_unchecked_rounded),
      findsOneWidget,
    );
  });

  testWidgets('收藏夹默认过滤到当前机器：机器名解析前不抢先展示其它机器的收藏', (WidgetTester tester) async {
    tester.view.physicalSize = const Size(2000, 1400);
    tester.view.devicePixelRatio = 1.0;
    addTearDown(tester.view.resetPhysicalSize);
    addTearDown(tester.view.resetDevicePixelRatio);

    final fav = _FakeFavoriteService();
    // 本机(HostA)与另一台机器(HostB)各有一条收藏。
    fav.items.addAll(const [
      FavoritePathItem(
        id: 'f1',
        path: '/favA',
        name: 'favA',
        isDirectory: true,
        machineName: 'HostA',
        createdAt: '',
      ),
      FavoritePathItem(
        id: 'f2',
        path: '/favB',
        name: 'favB',
        isDirectory: true,
        machineName: 'HostB',
        createdAt: '',
      ),
    ]);

    // 模拟 connector 列目录的慢响应：在 completer 完成前机器名未知。
    final pending = Completer<RemoteFileListResult>();

    await tester.pumpWidget(
      GetMaterialApp(
        translations: AppTranslations(),
        locale: const Locale('zh', 'CN'),
        home: Scaffold(
          body: Center(
            child: SizedBox(
              width: 1600,
              height: 900,
              child: RemoteFilePicker(
                selectionMode: RemoteFileSelectionMode.single,
                pickTarget: RemoteFilePickTarget.directories,
                title: '选择目录',
                favoriteApi: fav,
                onConfirm: (_) {},
                listProvider: (parentId, query) => pending.future,
              ),
            ),
          ),
        ),
      ),
    );
    // 收藏列表加载完成，但目录列表（机器名）仍在路上。
    await tester.pump();
    await tester.pump();

    // 切到收藏夹视图。
    await tester.tap(find.byIcon(Icons.star_rounded));
    await tester.pump();

    // 机器名还没解析出来时，绝不能抢先把其它机器的收藏展示出来。
    expect(find.text('favB'), findsNothing, reason: '机器名未知时不应展示其它机器(HostB)的收藏');
    expect(find.text('favA'), findsNothing, reason: '机器名未知时应等待，而非默认展示全部收藏');

    // connector 返回，机器名 = HostA。
    pending.complete(
      const RemoteFileListResult(files: [], machineName: 'HostA'),
    );
    await tester.pumpAndSettle();

    // 默认过滤到当前机器：只显示 HostA 的收藏。
    expect(find.text('favA'), findsOneWidget, reason: '默认应展示当前机器(HostA)的收藏');
    expect(find.text('favB'), findsNothing, reason: '默认不应展示其它机器(HostB)的收藏');
  });
}
