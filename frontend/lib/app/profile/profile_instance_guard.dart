import 'dart:async';
import 'dart:convert';
import 'dart:ffi';
import 'dart:io';

import 'package:flutter/foundation.dart' show debugPrint;
import 'package:path/path.dart' as p;
import 'package:window_manager/window_manager.dart';

import 'profile_paths.dart';

// ASFW_ANY：允许「任意进程」在下一次前台切换里抢占前台。
const int _asfwAny = 0xFFFFFFFF;

/// 桌面端 profile 运行锁 + 实例激活 IPC（仅 io 平台使用）。
///
/// 同一 profile 只允许一个进程运行：Windows/Linux 上双击图标天然会起
/// 第二个进程，此时不弹错误，而是通知已运行实例把窗口带到前台，
/// 新进程静默退出（与 Chrome/微信行为一致）。
///
/// - 锁：`profile.lock` 上的独占文件锁（POSIX 锁 / LockFileEx，
///   进程退出含崩溃时由 OS 自动释放，不会死锁残留）。
/// - IPC：持锁进程监听 localhost 回环端口（写入 `ipc_port` 文件），
///   收到 `activate` 即前台化窗口。三平台同一套实现。
class ProfileInstanceGuard {
  static const String lockFileName = 'profile.lock';
  static const String portFileName = 'ipc_port';
  static const String _activateCommand = 'activate';
  static const Duration _connectTimeout = Duration(seconds: 1);

  static RandomAccessFile? _lockHandle;
  static ServerSocket? _server;

  /// 持锁进程的激活监听端口（未启动时为 null）。
  static int? get activationPort => _server?.port;

  /// 尝试独占当前 profile。成功返回 true 并开始监听激活请求；
  /// 锁已被其他进程持有返回 false。
  static Future<bool> acquire() async {
    if (_lockHandle != null) return true;
    final dir = await ProfilePaths.currentDir();
    final lockFile = File(p.join(dir.path, lockFileName));
    final handle = await lockFile.open(mode: FileMode.append);
    try {
      await handle.lock(FileLock.exclusive);
    } on FileSystemException {
      await handle.close();
      return false;
    }
    _lockHandle = handle;
    // 锁文件内容仅供人工排查（NFS 等文件锁不可靠场景的兜底线索）。
    try {
      await handle.truncate(0);
      await handle.setPosition(0);
      await handle.writeString('$pid\n');
      await handle.flush();
    } catch (_) {}
    await _startActivationServer(dir);
    return true;
  }

  /// 通知已运行的同 profile 实例前台化。失败仅记录，不阻塞退出。
  static Future<void> activateExisting() async {
    try {
      final dir = await ProfilePaths.currentDir();
      final portFile = File(p.join(dir.path, portFileName));
      if (!await portFile.exists()) return;
      final port = int.tryParse((await portFile.readAsString()).trim());
      if (port == null || port <= 0) return;
      final socket = await Socket.connect(
        InternetAddress.loopbackIPv4,
        port,
        timeout: _connectTimeout,
      );
      socket.write('$_activateCommand\n');
      await socket.flush();
      await socket.close();
    } catch (e) {
      debugPrint('⚠️ Activate existing instance failed: $e');
    }
  }

  /// 探测某个 profile 是否有实例在运行（供实例列表使用）：
  /// 能连上其 IPC 端口即视为在运行，并顺带把它带到前台（可选）。
  static Future<bool> pingProfile(
    String profileName, {
    bool activate = false,
  }) async {
    try {
      final dir = await ProfilePaths.dirOf(profileName);
      final portFile = File(p.join(dir.path, portFileName));
      if (!await portFile.exists()) return false;
      final port = int.tryParse((await portFile.readAsString()).trim());
      if (port == null || port <= 0) return false;
      final socket = await Socket.connect(
        InternetAddress.loopbackIPv4,
        port,
        timeout: _connectTimeout,
      );
      if (activate) {
        socket.write('$_activateCommand\n');
        await socket.flush();
      }
      await socket.close();
      return true;
    } catch (_) {
      return false;
    }
  }

  static Future<void> _startActivationServer(Directory profileDir) async {
    try {
      final server = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
      _server = server;
      final portFile = File(p.join(profileDir.path, portFileName));
      await portFile.writeAsString('${server.port}\n', flush: true);
      server.listen((socket) {
        utf8.decoder
            .bind(socket)
            .transform(const LineSplitter())
            .listen(
              (line) {
                if (line.trim() == _activateCommand) {
                  _bringWindowToFront();
                }
              },
              onError: (_) {},
              cancelOnError: true,
            );
      });
    } catch (e) {
      // IPC 起不来不影响主流程：重复启动时降级为静默退出。
      debugPrint('⚠️ Activation server unavailable: $e');
    }
  }

  static Future<void> _bringWindowToFront() async {
    try {
      await windowManager.ensureInitialized();
      // 目标窗口可能被「关闭时最小化到托盘」藏起来了：先还原再显示，否则 focus 无从谈起。
      if (await windowManager.isMinimized()) {
        await windowManager.restore();
      }
      await windowManager.show();
      await windowManager.focus();
    } catch (e) {
      debugPrint('⚠️ Bring window to front failed: $e');
    }
  }

  /// Windows 前台交接：由「当前处于前台的调用方进程」在通知目标实例激活之前调用，
  /// 授予其它进程抢占前台的权限。
  ///
  /// 为什么需要它：Windows 默认禁止非前台进程调用 SetForegroundWindow 抢焦点
  /// （防止程序乱弹窗），后台的目标实例就算收到激活通知、执行 show()/focus()，也只会
  /// 让任务栏图标闪一下、窗口不真正弹到最前——表现就是「点了另一个账号切不过去」。
  /// 调用方此刻正是前台进程，有权用 AllowSetForegroundWindow(ASFW_ANY) 把这次前台切换
  /// 的许可让渡出去，目标实例的 focus() 随后才能真正生效。仅 Windows 需要，其它平台空操作。
  static void allowForegroundHandoff() {
    if (!Platform.isWindows) return;
    try {
      final user32 = DynamicLibrary.open('user32.dll');
      final allowSetForegroundWindow = user32.lookupFunction<
          Int32 Function(Uint32),
          int Function(int)>('AllowSetForegroundWindow');
      allowSetForegroundWindow(_asfwAny);
    } catch (e) {
      // 拿不到该接口不致命：退回原行为（目标窗口可能只任务栏闪烁）。
      debugPrint('⚠️ AllowSetForegroundWindow unavailable: $e');
    }
  }
}
