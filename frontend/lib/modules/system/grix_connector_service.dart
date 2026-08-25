import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';

import 'package:dio/dio.dart';
import 'package:get/get.dart' hide Response;
import 'package:pub_semver/pub_semver.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../platform/platform_capability.dart';
import '../../shared/utils/toast_util.dart';
import 'node_runtime_installer.dart';

/// grix-connector 本地 Admin API 服务
/// 封装健康检查、agent 列表、版本检测、安装/启动操作
/// 按 agent 开/关 Grix中转 的结果（见 GrixConnectorService.setAgentRelay）
enum GrixRelayToggleResult {
  ok,
  okButBusy,

  /// 连接器本机没有该 agent 的网关虚拟 Key（老版本连接器）。
  /// 新版本（≥3.16.0）会自己经 WS 向服务端闭环要凭证，不再回这个；
  /// 收到它意味着该走旧两段式（HTTP 签发 + 直传凭证）回落。
  needsKey,

  /// 连接器不在线
  offline,

  /// 连接器在线但它到服务端的 WS 不在线，开中转要的凭证要不来
  wsOffline,

  /// 服务端两次都没回应凭证申请
  credentialTimeout,

  /// 等待凭证期间用户又把中转关了，启用被取消
  cancelled,

  /// 服务端版本太旧，不认识 relay_credential_request
  unsupportedServer,

  /// 服务端明确拒绝了签发（余额/模型/权限等，具体原因见 lastError）
  credentialFailed,
  failed,
}

/// 把服务端签发的一次性中转凭证应用到本地连接器的结果
/// （见 GrixConnectorService.applyRelayCredential）。
enum GrixApplyRelayCredentialResult { ok, okButBusy, offline, failed }

/// 升级动作的结果（见 GrixConnectorService.upgrade）。
/// 两条路做的事完全不同，UI 必须据此如实告诉用户到底发生了什么。
enum ConnectorUpgradeOutcome {
  /// 指令已下发，connector 会自己装包、等任务空闲、重启生效
  queued,

  /// connector 没在跑，已直接装好新版本——要等它启动后才生效
  installed,

  /// 没升成，理由见 lastError
  failed,
}

class GrixConnectorService extends GetxService {
  static const int _healthPort = 19579;
  static const int _adminPort = 19580;
  static const String _healthBase = 'http://127.0.0.1:$_healthPort';
  static const String _adminBase = 'http://127.0.0.1:$_adminPort';

  final _dio = Dio(BaseOptions(
    connectTimeout: const Duration(seconds: 3),
    receiveTimeout: const Duration(seconds: 5),
  ));

  @visibleForTesting
  set httpAdapter(HttpClientAdapter adapter) => _dio.httpClientAdapter = adapter;

  /// 私有 Node 运行时（见 NodeRuntimeInstaller）。本机没有可用 Node 时由 ensureReady
  /// 静默装到 ~/.grix 下，之后所有 shell 调用把它前置到 PATH。
  late final NodeRuntimeInstaller nodeRuntime = NodeRuntimeInstaller(
    homeDir: Platform.environment['HOME'] ??
        Platform.environment['USERPROFILE'] ??
        '.',
  );

  /// 运行时安装的注入点（测试替换，避免真的下载）。
  @visibleForTesting
  late Future<bool> Function() installNodeRuntime = _defaultInstallNodeRuntime;

  Future<bool> _defaultInstallNodeRuntime() async {
    _installShellInFlight = true;
    try {
      installLog.value = '${'system_node_runtime_installing'.tr}\n';
      return await nodeRuntime.install(
        onLog: (line) => installLog.value += '$line\n',
      );
    } catch (e) {
      lastError.value = e.toString();
      return false;
    } finally {
      _installShellInFlight = false;
    }
  }

  /// 需要前置到 PATH 的目录：私有 Node 运行时的 bin；Windows 上再补 Node 官方
  /// 安装目录和 npm 全局 bin——GUI 进程继承的是启动时的环境，会话中装上的
  /// Node / 全局包不会自动进它的 PATH。
  @visibleForTesting
  List<String> extraPathDirs() {
    final dirs = <String>[];
    if (nodeRuntime.isInstalled) dirs.add(nodeRuntime.binDir);
    if (Platform.isWindows) {
      final env = Platform.environment;
      for (final candidate in [
        if (env['APPDATA'] != null) '${env['APPDATA']}\\npm',
        if (env['ProgramFiles'] != null) '${env['ProgramFiles']}\\nodejs',
      ]) {
        if (Directory(candidate).existsSync()) dirs.add(candidate);
      }
    }
    return dirs;
  }

  /// 把 [extraPathDirs] 前置到 PATH 后再执行命令。放在命令里而不是 environment
  /// 参数里：login shell 的 profile 会重设 PATH，命令内 export 才能保证生效。
  @visibleForTesting
  String withRuntimePath(String command, {bool? windows}) {
    final dirs = extraPathDirs();
    if (dirs.isEmpty) return command;
    if (windows ?? Platform.isWindows) {
      return 'set "PATH=${dirs.join(';')};%PATH%" && $command';
    }
    final quoted = dirs.map((d) => "'${d.replaceAll("'", "'\\''")}'").join(':');
    return 'export PATH=$quoted:"\$PATH"; $command';
  }

  /// 连接状态
  final isRunning = false.obs;
  final isInstalled = false.obs;
  final installedVersion = ''.obs;
  final latestVersion = ''.obs;
  final agents = <Map<String, dynamic>>[].obs;
  final uptime = 0.obs;
  final pid = 0.obs;
  final lastError = ''.obs;


  // --- Probe 状态 ---
  final probeResults = <AgentProbeResult>[].obs;
  final installedClients = <InstalledClientCommand>[].obs;
  final probeLoading = false.obs;
  final probeSummary = Rx<ProbeSummary?>(null);

  Timer? _pollTimer;

  // --- 保活看门狗 ---
  // connector 是桌面端运行 agent 的必需组件，掉线必须拉起，因此保活是固定的
  // 内部行为，不提供开关。失败时按指数退避持续重试，直到恢复。
  bool _restartInFlight = false;
  int _restartCount = 0;
  int _consecutiveFailures = 0;
  DateTime? _nextRestartAt;
  /// 本次运行是否见过连接器在线。冷启动时的拉起是静默的，只有"在线后掉线"
  /// 才需要提示用户，避免每次开 App 都弹一条重启成功。
  bool _sawRunning = false;
  Future<bool>? _startFuture;

  /// 最后一次从 /healthz 读到的 daemon pid。离线时**不清零**：daemon 挂死（进程活着
  /// 占着单例锁，但 /healthz 探不通）时 start 是无效的，恢复只能靠这个 pid 去杀它。
  int _lastKnownPid = 0;

  /// 本次「离线→在线」跃迁的时刻。在线不足 [stableOnlineWindow] 就又掉线视作
  /// 崩溃循环的一环，退避计数不清零，见 checkHealth / _markOffline。
  DateTime? _onlineSince;
  DateTime? _lastRecoveryToastAt;

  /// 在线满这么久才算「稳定恢复」，退避计数才清零。若一探到在线就清零，
  /// 启动即崩的 daemon 会被无退避地每个轮询周期 spawn 一次。
  static const stableOnlineWindow = Duration(seconds: 60);

  /// 连续拉起失败达到该次数、且握着旧 pid 时，升级为「杀掉疑似挂死的旧进程再拉起」。
  static const killEscalationThreshold = 2;

  /// 掉线恢复成功 toast 的最小间隔。崩溃循环下每次拉起都"成功"过一瞬，
  /// 不节流的话 toast 会随循环刷屏。
  static const recoveryToastInterval = Duration(minutes: 10);

  /// 连续拉起失败达到该次数时回退版本：杀挂死进程（阈值 2）都救不回来，多半是
  /// 装上了起不来的新版本或安装已损坏，重装「上一个稳定在线过的版本」兜底。
  static const rollbackEscalationThreshold = 4;

  /// 上一个稳定在线满 [stableOnlineWindow] 的版本号（跨 App 重启持久化）。
  /// 这是回退的目标：它在这台机器上被证明能跑起来。
  final lastGoodVersion = ''.obs;

  /// 本轮离线期是否已尝试过回退（一轮只回退一次，避免反复 npm install 轰炸）。
  /// 稳定在线后复位。
  bool _rollbackAttempted = false;

  // --- 连接器自报的运行元状态（connector ≥4.2 的 /healthz 顶层字段，老版本没有）---

  /// 已连上服务端 WS 的 agent 数 / 总数。连接器活着不等于 agent 可达，
  /// total>0 且 connected<total 时界面必须显示"运行中但断服"，不能说一切正常。
  final wsConnected = 0.obs;
  final wsTotal = 0.obs;

  /// 连接器是否有一笔升级事务在进行（journal 未终结）。
  final upgradeInProgress = false.obs;
  final upgradePhase = ''.obs;

  /// 最近一次 healthz 报出 in_progress 的时刻。升级事务中 daemon 会按计划
  /// 自杀重启，桌面探到"离线"是预期内的：此刻杀进程/装包/拉起都会跟 guardian
  /// 的原子切包打架，看门狗要停手到事务结束或宽限窗超时。
  DateTime? _upgradeSeenAt;

  /// 升级事务的看门狗停手宽限窗。guardian 自带绝对 deadline 与失败回滚，
  /// 正常事务几分钟内收场；超窗还起不来就当事务失控，看门狗恢复接管。
  static const upgradeStandDownWindow = Duration(minutes: 10);

  /// 升级事务首次被探到停在当前 phase 的时刻；phase 一变就重新计时。
  /// daemon 在线但事务不动，看门狗（只管离线）永远不会介入，得单独盯。
  DateTime? _upgradeStalledSince;
  String _upgradeStalledPhase = '';

  /// 同一 phase 停滞超过这么久就判定事务失控，桌面端接管：重装最新版并 restart。
  /// 连接器自己的激活超时是 10 分钟、验证 2 分钟，30 分钟远在正常事务之外。
  static const upgradeStallTakeoverWindow = Duration(minutes: 30);

  /// 本次 App 会话是否已接管过一次（避免反复 npm install 轰炸）。
  bool _stalledUpgradeTakeoverAttempted = false;
  Future<void>? _stalledUpgradeTakeoverFuture;

  @visibleForTesting
  Future<void>? get stalledUpgradeTakeoverForTest =>
      _stalledUpgradeTakeoverFuture;

  static const _lastGoodVersionKey = 'connector_last_good_version';

  @visibleForTesting
  int get consecutiveFailuresForTest => _consecutiveFailures;
  @visibleForTesting
  DateTime? get nextRestartAtForTest => _nextRestartAt;
  @visibleForTesting
  int get lastKnownPidForTest => _lastKnownPid;

  /// 测试环境不自举。测试跑在 macOS host 上，isDesktop 为真，onInit 会真的去 shell 探
  /// 命令、打本机 19579/19580、起 10 秒轮询——测试结果就成了"跑测这台机器上有没有活着的
  /// connector"的函数，异步回填的版本号还会盖掉用例自己摆好的状态。
  static final bool _isTestEnv = Platform.environment.containsKey('FLUTTER_TEST');

  @override
  void onInit() {
    super.onInit();
    if (_isTestEnv) return;
    // grix-connector 是桌面端本地代理，移动端没有该服务也无法 spawn 子进程，
    // 即便用户误打开了状态页，也不应启动 Process.run / 10 秒轮询，
    // 否则会持续唤醒系统造成发热。
    if (!PlatformCapability.isDesktop) {
      return;
    }
    loadLastGoodVersion().then((v) {
      if (v != null && v.isNotEmpty) lastGoodVersion.value = v;
    });
    checkAll().then((_) => ensureReady());
    // 每 10 秒轮询一次健康状态
    _pollTimer = Timer.periodic(const Duration(seconds: 10), (_) => checkHealth());
  }

  @override
  void onClose() {
    _pollTimer?.cancel();
    super.onClose();
  }

  /// 综合检查：安装状态 + 运行状态
  ///
  /// 这里不直接探测：启动时 connector 往往还没运行（随后由 ensureReady 拉起），
  /// 此刻探测只会拿到空结果。探测统一由 checkHealth 里的「离线 → 在线」跃迁驱动。
  Future<void> checkAll() async {
    // 必须先探健康：daemon 在跑时 /healthz 就带着版本号，checkInstalled 便无需
    // 再去 shell 调 CLI 读版本（那条路径对运行中的 daemon 有副作用，见 checkInstalled）
    await checkHealth();
    await checkInstalled();
    await checkLatestVersion();
  }

  /// 检查是否已安装 grix-connector
  /// 使用 login shell 确保能发现 nvm/Homebrew 等管理的命令
  ///
  /// 只判断"装没装"，不读版本号。版本号一律由运行中的 daemon 经 /healthz 自报
  /// （见 checkHealth）——这里绝不能 shell 调 `grix-connector --version`：老版 CLI
  /// 不认这个参数，会把它当成"没给命令"直接进守护进程模式，daemon 在跑就抢锁把它
  /// 杀掉（每刷新一次状态页全部 agent 断连重连），daemon 没跑则就地启动一个并挂住。
  Future<void> checkInstalled() async {
    try {
      final result = await _shellRun(connectorPresenceCommand());
      isInstalled.value = result.exitCode == 0;
      if (!isInstalled.value) installedVersion.value = '';
    } catch (_) {
      isInstalled.value = false;
      installedVersion.value = '';
    }
  }

  /// 健康检查（无需鉴权）
  Future<void> checkHealth() async {
    try {
      final resp = await _dio.get('$_healthBase/healthz');
      if (resp.statusCode == 200) {
        final data = resp.data as Map<String, dynamic>;
        final wasRunning = isRunning.value;
        isRunning.value = data['status'] == 'ok';
        uptime.value = data['uptime'] ?? 0;
        pid.value = data['pid'] ?? 0;
        agents.value = List<Map<String, dynamic>>.from(data['agents'] ?? []);
        // 运行中的 daemon 自报版本，这是读版本号唯一无副作用的来源。
        // 无条件覆盖：报不出版本就得清空，否则 daemon 换成不带 version 字段的老版本后，
        // 界面会继续显示上一个 daemon 的版本号，把"跑着老版"显示成"已是最新"。
        installedVersion.value =
            _parseSemver('${data['version'] ?? ''}')?.toString() ?? '';
        lastError.value = '';
        if (pid.value > 0) _lastKnownPid = pid.value;
        // connector ≥4.2 自报的 WS 摘要与升级事务快照；老版本没有这些字段，
        // 一律按"未知即零值/无事务"处理
        final ws = data['ws'];
        wsConnected.value = ws is Map ? (ws['connected'] as num?)?.toInt() ?? 0 : 0;
        wsTotal.value = ws is Map ? (ws['total'] as num?)?.toInt() ?? 0 : 0;
        final upgrade = data['upgrade'];
        final upgradeActive = upgrade is Map && upgrade['in_progress'] == true;
        upgradeInProgress.value = upgradeActive;
        upgradePhase.value = upgradeActive ? '${upgrade['phase'] ?? ''}' : '';
        if (upgradeActive) {
          _upgradeSeenAt = clock();
          if (upgradePhase.value != _upgradeStalledPhase) {
            _upgradeStalledPhase = upgradePhase.value;
            _upgradeStalledSince = clock();
          }
        } else {
          // 只有 daemon 亲口说"没有在途事务"才解除停手；离线期间不清，
          // 否则升级重启的离线窗刚开始看门狗就会扑上去
          _upgradeSeenAt = null;
          _upgradeStalledSince = null;
          _upgradeStalledPhase = '';
        }
        if (isRunning.value) {
          _sawRunning = true;
          if (!wasRunning) _onlineSince = clock();
          if (!upgradeActive) unawaited(modernizeWindowsConnectorIfNeeded());
          if (upgradeActive) unawaited(takeOverStalledUpgradeIfNeeded());
          // 在线满稳定窗口才清零退避：启动即崩的 daemon 会被反复拉起，
          // 一探到在线就清零的话，崩溃循环就退化成无退避的快速 spawn。
          final since = _onlineSince;
          if (since != null && clock().difference(since) >= stableOnlineWindow) {
            _consecutiveFailures = 0;
            _nextRestartAt = null;
            _rollbackAttempted = false;
            // 站稳了的版本记为「已知可用」：它是这台机器上回退的目标
            final stableVersion = installedVersion.value;
            if (stableVersion.isNotEmpty &&
                stableVersion != lastGoodVersion.value) {
              lastGoodVersion.value = stableVersion;
              unawaited(saveLastGoodVersion(stableVersion));
            }
          }
          // 由离线转为在线（首次连上 / 被 ensureReady 拉起 / 看门狗重启后恢复）时，
          // 探测结果要么从未填充、要么已在离线时被清空，且没有任何轮询会回填它，
          // 不在这里补探，Agent 工具栏就会一直是空的。
          if (!wasRunning) {
            unawaited(probeAll());
            // 首次检查时 daemon 往往还没起来，可用版本只能退而问 npm registry（绕开了
            // 灰度规则）。它一上线就以它的判断为准重查一次，否则那个不准的结果会一直挂着。
            unawaited(checkLatestVersion());
          } else if (latestVersion.value.isEmpty) {
            // 上一次问 connector 没问出结果（抖了一下），当时选择了不拿 npm 覆盖它。
            // 这里不补一次，可用版本就会一直空着，升级入口再也不出现。
            unawaited(checkLatestVersion());
          }
        }
      } else {
        _markOffline('HTTP ${resp.statusCode}');
      }
    } catch (e) {
      _markOffline(e is DioException ? 'system_connection_failed'.tr : e.toString());
    }
  }

  /// 启动路径上 checkAll 和 checkHealth 的「离线→在线」跃迁会同时触发查版本，
  /// 合流到同一次在途请求，别让 connector 白白多问一遍后端（它是按 agent 数逐个发的）。
  Future<void>? _latestVersionFuture;

  /// 查可用的新版本。
  ///
  /// daemon 在跑时一律以 connector 自己的判断为准（GET /api/upgrade，它问的是后端的
  /// 已发布版本 + 灰度规则），而不是 npm registry 的 latest。两者必须同源：升级实际由
  /// connector 执行，它按灰度规则决定升不升；npm 上出了新版不等于这台机器就该升。若拿
  /// npm latest 当"有更新"，灰度期内没被圈中的机器会一直显示升级按钮，点了 connector
  /// 却什么都不做——又是一次"点了没反应"。
  ///
  /// 只有 daemon 没在跑（问不到它）时才回落到 npm registry：此时按钮走的也是直接装包
  /// 那条路，本就绕开了灰度。
  Future<void> checkLatestVersion() {
    return _latestVersionFuture ??= _checkLatestVersion()
        .whenComplete(() => _latestVersionFuture = null);
  }

  Future<void> _checkLatestVersion() async {
    if (isRunning.value && await _checkLatestFromConnector()) return;
    // 官方源优先，被墙/不通时退到国内镜像；哪个先答出来用哪个，都不通不影响主流程
    for (final registry in const [
      'https://registry.npmjs.org',
      npmMirrorRegistry,
    ]) {
      try {
        final resp = await _dio.get(
          '$registry/grix-connector/latest',
          options: Options(receiveTimeout: const Duration(seconds: 8)),
        );
        if (resp.statusCode == 200) {
          latestVersion.value =
              (resp.data as Map<String, dynamic>)['version'] ?? '';
          return;
        }
      } catch (_) {
        // 试下一个源
      }
    }
  }

  /// 问 connector：后端现在放给这台机器的版本是什么。
  ///
  /// 返回 false 才回落 npm registry，而回落只允许发生在一种情况：这个 connector 老到
  /// 根本没有该接口。它在跑、只是抖了一下（超时、5xx、答了个看不懂的东西）时绝不能回落
  /// ——npm 的 latest 绕开灰度，会亮出这台机器根本升不了的版本，正是本次要根治的病。
  /// 那种情况下宁可这一轮不更新可用版本，交给下一轮补查（见 checkHealth）。
  Future<bool> _checkLatestFromConnector() async {
    try {
      final resp = await _dio.get(
        '$_adminBase/api/upgrade',
        options: Options(validateStatus: (s) => s != null),
      );
      final code = resp.statusCode ?? 0;
      if (code == 404 || code == 501) return false; // 老 connector：没有这个接口
      if (code != 200) return true;
      final data = resp.data;
      if (data is! Map) return true;
      if (data['available'] == true) {
        final version = '${(data['release'] as Map?)?['version'] ?? ''}';
        if (_parseSemver(version) == null) return true;
        latestVersion.value = version;
        return true;
      }
      // connector 说"没有可升的版本"：即便 npm 上有更新的包，这台机器也不该升
      // （灰度没圈到它）。把可用版本对齐到当前版本，hasUpdate 自然为 false。
      //
      // ⚠ available:false 是个二义信号——connector 的 checkForUpdate() 在"问不到后端"
      // （网络错、5xx）和"没配 agent"时同样返回它，并非只表示"已是最新"。这里仍然选择
      // 不提示更新：那种状态下 connector 自己也升不动（触发升级同样要问后端），亮一个
      // 点了不动的按钮比不亮更糟。
      //
      // 但它要是连自己的版本都报不出来（老到 /healthz 不带 version 字段），这里就不能
      // 抹平——那会把 latestVersion 写成空串，连带废掉 hasUpdate 里"运行中报不出版本
      // 就提示更新"的逃生通道，这台机器在界面上会被当成一切正常，再也没有升级入口。
      if (installedVersion.value.isEmpty) return false;
      latestVersion.value = installedVersion.value;
      return true;
    } on DioException {
      // 连不上/超时：daemon 可能正好在重启。同样不回落 npm（理由见方法注释）。
      return true;
    } catch (_) {
      return true;
    }
  }

  /// 是否有可用更新
  bool get hasUpdate {
    final latest = _parseSemver(latestVersion.value);
    if (latest == null) return false;

    final installed = _parseSemver(installedVersion.value);
    if (installed != null) return latest > installed;

    // 版本号读不出来：daemon 在跑却报不出版本，说明它老到 /healthz 还不带 version 字段，
    // 那正是需要这次更新去修的版本，直接提示更新。daemon 没跑时无从判断，不提示。
    // （早先这里做的是"字符串不相等即有更新"，结果把 CLI 打印的一行日志当成了版本号，
    //   装着最新版也一直提示更新——就是这个 bug 的表象。）
    return isRunning.value;
  }

  /// 连接器是否支持"开中转时自己经 WS 向服务端闭环要凭证"（relay_credential_request，
  /// 3.16.0 起）。低于此版本走旧两段式：桌面端先 HTTP 签发、再 applyRelayCredential 直传。
  /// 版本读不出来一律按不支持处理（老到连版本都报不出的 daemon 更不可能支持）。
  bool get supportsWsRelayCredential {
    final v = _parseSemver(installedVersion.value);
    final floor = _parseSemver('3.16.0');
    if (v == null || floor == null) return false;
    return v >= floor;
  }

  /// 连接器是否**确认**不支持中转开关的服务端期望态协议（relay_state_sync_request /
  /// apply_relay_state，connector M2，3.19.0 起）：版本读得出来且 <3.19.0。
  /// 面板以此分流（单一事实来源）：false 时只写服务端 desired，下发/回执由
  /// "服务端 ↔ 连接器"WS 闭环；true 时回退 Admin API 两段式（降级路径）。
  /// 版本读不出来（daemon 不在线/老到不报版本）不算确认——离线时面板仍写服务端
  /// desired，由 connector 上线后的 sync 兜底对齐；若按"未知即旧版"处理，连接器一
  /// 离线开关就被锁回旧路径，违背"离线也照写 desired"的约定（设计 §2.5）。
  bool get relayStateProtocolUnsupported {
    final v = _parseSemver(installedVersion.value);
    final floor = _parseSemver('3.19.0');
    if (v == null || floor == null) return false;
    return v < floor;
  }

  static Version? _parseSemver(String value) {
    final match = RegExp(
      r'v?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?)',
    ).firstMatch(value.trim());
    if (match == null) return null;
    try {
      return Version.parse(match.group(1)!);
    } catch (_) {
      return null;
    }
  }

  @visibleForTesting
  static String connectorPresenceCommand({bool? windows}) {
    return (windows ?? Platform.isWindows)
        ? 'where grix-connector'
        : 'command -v grix-connector';
  }

  /// 安装 grix-connector（通过 npm，使用 login shell 确保 PATH 正确）
  Future<bool> install() async {
    lastError.value = '';
    installLog.value = '';
    final ok = await npmInstall('grix-connector');
    if (ok) await checkInstalled();
    return ok;
  }

  /// 启动 grix-connector daemon。
  /// 启动保障、看门狗、状态页按钮可能同时触发，这里把并发调用合流到同一次拉起，
  /// 避免重复 spawn 进程。
  Future<bool> start() {
    return _startFuture ??=
        _startProcess().whenComplete(() => _startFuture = null);
  }

  Future<bool> _startProcess() async {
    try {
      lastError.value = '';
      final result = await _shellRun('grix-connector start');
      if (result.exitCode == 0) {
        // 等待 daemon 启动
        await Future.delayed(startProbeDelay);
        await checkHealth();
        return isRunning.value;
      }
      lastError.value = (result.stderr as String).trim();
      return false;
    } catch (e) {
      lastError.value = e.toString();
      return false;
    }
  }

  /// 手动重启本机 daemon：杀掉当前进程再拉起。会掐断本机所有 agent 的在途任务，
  /// 只供状态页的显式操作使用；看门狗对挂死进程的自动清理见 _keepAlive。
  Future<bool> restartDaemon() async {
    lastError.value = '';
    final target = pid.value > 0 ? pid.value : _lastKnownPid;
    if (target > 0) {
      await _killDaemonPid(target);
      _lastKnownPid = 0;
    }
    return start();
  }

  /// 杀掉 daemon 进程。先按命令行校验 pid 身份（防 pid 被系统复用后误杀无关进程），
  /// 再 SIGTERM 给它体面退出的机会，赖着不走才 SIGKILL；Windows 上 taskkill /F 直接强杀。
  Future<void> _killDaemonPid(int pid) async {
    if (!await _isConnectorPid(pid)) return;
    try {
      if (Platform.isWindows) {
        // /T 连子进程树一起清：daemon 挂死时它拉的 agent 子进程没人回收，
        // 会一直挂着占资源（Unix 路径靠 daemon 收到 SIGTERM 自己清理）
        await processRun('taskkill', ['/PID', '$pid', '/T', '/F']);
        return;
      }
      await processRun('kill', ['$pid']);
      for (var i = 0; i < 10; i++) {
        await Future.delayed(const Duration(milliseconds: 300));
        if (!await _pidAlive(pid)) return;
      }
      await processRun('kill', ['-9', '$pid']);
    } catch (_) {
      // 杀不掉也不阻塞后续 start：start 失败会走看门狗自己的退避
    }
  }

  /// pid 对应的进程是否还是 grix-connector daemon
  Future<bool> _isConnectorPid(int pid) async {
    try {
      final ProcessResult result;
      if (Platform.isWindows) {
        result = await processRun('powershell', [
          '-NoProfile',
          '-Command',
          '(Get-CimInstance Win32_Process -Filter "ProcessId=$pid").CommandLine',
        ]);
      } else {
        result = await processRun('ps', ['-p', '$pid', '-o', 'command=']);
      }
      return result.exitCode == 0 &&
          (result.stdout as String).contains('grix-connector');
    } catch (_) {
      return false;
    }
  }

  Future<bool> _pidAlive(int pid) async {
    try {
      final result = await processRun('kill', ['-0', '$pid']);
      return result.exitCode == 0;
    } catch (_) {
      return false;
    }
  }

  /// 已下发升级指令的目标版本（进程内状态）。
  /// 记版本而不是记一个 bool：bool 在这次升级完成后不会自己复位，等下一个新版本出来时
  /// 按钮就会一上来显示成"已通知升级"，点都点不了。
  final upgradeQueuedVersion = ''.obs;

  /// 下发时刻，配合 [upgradeQueueTtl] 给等待态留一个出口。
  final upgradeQueuedAt = Rxn<DateTime>();

  /// 下发多久之后允许重新下发。
  ///
  /// 这不是"超时即失败"——connector 要等所有任务空闲才重启，忙起来等上一小时也正常。
  /// 但升级要是装包失败并回滚了，connector 会继续报同一个可用版本，等待态若没有出口
  /// 就成了死态：按钮再也回不来，本次进程内用户没有任何重试入口。
  static const upgradeQueueTtl = Duration(minutes: 15);

  @visibleForTesting
  DateTime Function() clock = DateTime.now;

  /// 进程操作注入点：测试里换成假实现，避免真的 spawn / 杀进程。
  @visibleForTesting
  Future<ProcessResult> Function(String executable, List<String> arguments)
      processRun = _defaultProcessRun;

  // 10 秒而不是 5 秒：Windows 上 PowerShell 冷启动（挂死进程身份校验走它）和
  // 带 nvm 的 login shell 都可能超过 5 秒，探测被掐断会误判成「未安装/不是本进程」。
  static Future<ProcessResult> _defaultProcessRun(
    String executable,
    List<String> arguments,
  ) =>
      Process.run(executable, arguments).timeout(const Duration(seconds: 10));

  /// start() 拉起后等 daemon 起身的时间，测试里置零免得每个用例干等 2 秒。
  @visibleForTesting
  Duration startProbeDelay = const Duration(seconds: 2);

  /// last-good 版本的持久化注入点（测试替换，避免依赖 SharedPreferences 平台通道）
  @visibleForTesting
  Future<String?> Function() loadLastGoodVersion = _defaultLoadLastGood;
  @visibleForTesting
  Future<void> Function(String version) saveLastGoodVersion =
      _defaultSaveLastGood;

  static Future<String?> _defaultLoadLastGood() async =>
      (await SharedPreferences.getInstance()).getString(_lastGoodVersionKey);

  static Future<void> _defaultSaveLastGood(String version) async {
    await (await SharedPreferences.getInstance())
        .setString(_lastGoodVersionKey, version);
  }

  /// 回退安装的注入点：默认走真实 npm 装包（带实时日志），测试替换成记录器。
  @visibleForTesting
  late Future<bool> Function(String version) rollbackInstall =
      _defaultRollbackInstall;

  Future<bool> _defaultRollbackInstall(String version) {
    return npmInstall(
      'grix-connector@$version',
      timeoutSeconds: 180,
      skipVerify: true,
    );
  }

  /// npm 官方源不可达时的兜底镜像。只做兜底不做默认：第一次安装永远走用户自己的
  /// npm 配置（可能已配了企业内源或其他镜像），失败且症状像网络问题才用它重试。
  static const npmMirrorRegistry = 'https://registry.npmmirror.com';

  /// 上一次 _runInstallShell 是否因超时被杀。registry 被墙的典型症状是 npm 静默
  /// 挂住直到超时，输出里未必有网络关键字，必须单独记录这个信号。
  @visibleForTesting
  bool lastInstallTimedOut = false;

  /// 安装 shell 的注入点（测试替换，避免真的跑 npm）。
  @visibleForTesting
  late Future<bool> Function(
    String command, {
    required String clientType,
    required int timeoutSeconds,
    required bool skipVerify,
  }) installShell = _defaultInstallShell;

  Future<bool> _defaultInstallShell(
    String command, {
    required String clientType,
    required int timeoutSeconds,
    required bool skipVerify,
  }) {
    return _runInstallShell(
      command,
      clientType: clientType,
      timeoutSeconds: timeoutSeconds,
      friendlyName: command,
      skipVerify: skipVerify,
    );
  }

  /// 所有 npm 全局装包的统一入口：先走用户默认 registry，失败且症状像网络不通
  /// （被墙、DNS 失败、超时挂死）时用国内镜像重试一次。
  @visibleForTesting
  Future<bool> npmInstall(
    String packageSpec, {
    String clientType = '',
    int timeoutSeconds = 120,
    bool skipVerify = true,
  }) async {
    final ok = await installShell(
      'npm install -g $packageSpec',
      clientType: clientType,
      timeoutSeconds: timeoutSeconds,
      skipVerify: skipVerify,
    );
    if (ok) return true;
    final symptom = '${installLog.value}\n${lastError.value}';
    if (!lastInstallTimedOut && !looksLikeNetworkFailure(symptom)) {
      return false; // 权限、磁盘满等本地问题，换源解决不了
    }
    installLog.value += '\n${'system_npm_mirror_retry'.tr}\n';
    return installShell(
      'npm install -g $packageSpec --registry=$npmMirrorRegistry',
      clientType: clientType,
      timeoutSeconds: timeoutSeconds,
      skipVerify: skipVerify,
    );
  }

  /// 输出是否像网络故障（据此决定要不要换镜像重试）
  @visibleForTesting
  static bool looksLikeNetworkFailure(String output) {
    final lower = output.toLowerCase();
    const markers = [
      'enotfound',
      'etimedout',
      'econnreset',
      'econnrefused',
      'eai_again',
      'network',
      'timeout',
      'timed out',
      'fetch failed',
      'socket hang up',
    ];
    return markers.any(lower.contains);
  }

  /// 当前这个可用版本是否已经下发过升级指令（下发过就别再让用户重复点）
  bool get upgradeQueued {
    if (upgradeQueuedVersion.value.isEmpty) return false;
    if (upgradeQueuedVersion.value != latestVersion.value) return false;
    final at = upgradeQueuedAt.value;
    if (at == null) return true;
    return clock().difference(at) < upgradeQueueTtl;
  }

  /// 升级 grix-connector。
  ///
  /// 这里**不能**直接 `npm install -g`：包装上了也没用——跑着的 daemon 仍是旧进程、
  /// 旧代码，/healthz 继续自报旧版本，界面按钮原地不动，用户看到的就是"点了没反应"。
  /// 真正的升级只能由 connector 自己那条链路完成（装包 → guardian 看护 → 等所有任务
  /// 空闲后自杀重启 → 新版本被拉起 → 失败自动回滚）。这里只负责把"现在就去升级"这条
  /// 指令发过去，之后不再等待，也不由客户端重启 daemon——重启会掐断本机所有 agent。
  Future<ConnectorUpgradeOutcome> upgrade() async {
    lastError.value = '';
    // daemon 没在跑：那条链路无从触发（也没有"等空闲"的顾虑），只能直接装包。装上的
    // 新版本要等连接器启动后才生效——这跟"已下发、会自动生效"是两回事，得分开告诉用户。
    if (!isRunning.value) {
      return await install()
          ? ConnectorUpgradeOutcome.installed
          : ConnectorUpgradeOutcome.failed;
    }
    try {
      final resp = await _dio.post(
        '$_adminBase/api/upgrade',
        // 连接器答了就不算"连不上"：4xx/5xx 要如实报出来。若放任 dio 把非 2xx 抛成
        // DioException，就会被下面的 catch 统一改写成"连接失败"，把它明确的拒绝理由
        // （已有升级在跑、没有可发布版本……）丢掉，用户只会去排查网络。
        options: Options(validateStatus: (s) => s != null),
      );
      if (resp.statusCode == 200) {
        upgradeQueuedVersion.value = latestVersion.value;
        upgradeQueuedAt.value = clock();
        return ConnectorUpgradeOutcome.queued;
      }
      lastError.value = 'system_upgrade_rejected'.trParams({
        'code': '${resp.statusCode}',
      });
      return ConnectorUpgradeOutcome.failed;
    } catch (e) {
      lastError.value =
          e is DioException ? 'system_connection_failed'.tr : e.toString();
      return ConnectorUpgradeOutcome.failed;
    }
  }

  // --- Windows 存量 3.x 连接器「上车」---
  // 3.x 的 Windows 服务从 <root>/runtime 副本启动，npm 全局包更新不会刷新它，
  // 连接器自升级到任何 4.x 都是「装上 → 旧 runtime 起来 → 判回滚」，线上曾每
  // 5 分钟循环一次。服务端已用 min_version 挡住 3.x，因此这些机器永远升不上去。
  // 4.x CLI 的 `restart` 会重铺 runtime 并重写 wrapper（不需要管理员权限），
  // 桌面端在这里替用户跑一次：装 latest → 用新 CLI restart → healthz 报新版本。

  /// 低于此版本的 Windows 连接器需要桌面端出手升级。
  static const windowsModernFloor = '4.0.0';

  /// 平台判断注入点（测试里模拟 Windows）。
  @visibleForTesting
  bool Function() isWindowsPlatform = () => Platform.isWindows;

  /// 本次运行只试一次：失败多半是网络或安装损坏，看门狗/用户手动处理，
  /// 不要每个轮询周期都 npm install 一遍。
  bool _modernizeAttempted = false;
  @visibleForTesting
  bool get modernizeAttemptedForTest => _modernizeAttempted;

  Future<void> modernizeWindowsConnectorIfNeeded() async {
    if (_modernizeAttempted || !isWindowsPlatform()) return;
    final installed = _parseSemver(installedVersion.value);
    final floor = _parseSemver(windowsModernFloor);
    if (installed == null || floor == null || installed >= floor) return;
    if (_installShellInFlight || _restartInFlight) return;
    _modernizeAttempted = true;
    debugPrint('[ConnectorModernize] Windows 连接器 $installed < $floor，'
        '桌面端接管升级');
    CustomToast.show('system_windows_modernize'.tr, isError: false);
    lastError.value = '';
    final ok = await npmInstall('grix-connector@latest', timeoutSeconds: 300);
    if (!ok) {
      debugPrint('[ConnectorModernize] npm install 失败: ${lastError.value}');
      return;
    }
    // 必须是 restart 而不是 start：daemon 在跑时 start 直接短路，不会重铺 runtime
    final restarted = await installShell(
      'grix-connector restart',
      clientType: '',
      timeoutSeconds: 180,
      skipVerify: true,
    );
    debugPrint('[ConnectorModernize] restart ${restarted ? '成功' : '失败'}: '
        '${lastError.value}');
    await checkHealth();
  }

  /// 升级事务在同一 phase 停滞超过 [upgradeStallTakeoverWindow] 时由桌面端接管。
  ///
  /// 典型场景：guardian 被杀软/断电干掉，pending 停在 handoff_ready/activating，
  /// daemon 在线却永远等 guardian；连接器自己的 check() 又被 pending 短路，
  /// 自动升级不再触发，用户手工 npm 重装 + stop/start 也上不了新版。
  /// 这里装最新版（≥4.2.6 启动即会把停滞事务收口）并 restart 重铺 runtime，
  /// 让机器不靠人工就走出卡死状态。每次 App 会话只做一次。
  Future<void> takeOverStalledUpgradeIfNeeded() {
    if (_stalledUpgradeTakeoverAttempted) return Future.value();
    if (_installShellInFlight || _restartInFlight) return Future.value();
    final since = _upgradeStalledSince;
    if (since == null ||
        clock().difference(since) < upgradeStallTakeoverWindow) {
      return Future.value();
    }
    _stalledUpgradeTakeoverAttempted = true;
    return _stalledUpgradeTakeoverFuture ??= _takeOverStalledUpgrade();
  }

  Future<void> _takeOverStalledUpgrade() async {
    debugPrint('[ConnectorTakeover] 升级事务停滞在 $_upgradeStalledPhase '
        '超过 ${upgradeStallTakeoverWindow.inMinutes} 分钟，桌面端接管');
    CustomToast.show('system_upgrade_stalled_takeover'.tr, isError: false);
    lastError.value = '';
    final ok = await npmInstall('grix-connector@latest', timeoutSeconds: 300);
    if (!ok) {
      debugPrint('[ConnectorTakeover] npm install 失败: ${lastError.value}');
      return;
    }
    // 必须是 restart 而不是 start：daemon 在跑时 start 直接短路，不会重铺 runtime
    final restarted = await installShell(
      'grix-connector restart',
      clientType: '',
      timeoutSeconds: 180,
      skipVerify: true,
    );
    debugPrint('[ConnectorTakeover] restart ${restarted ? '成功' : '失败'}: '
        '${lastError.value}');
    await checkHealth();
  }

  /// 桌面启动时自动保障 connector 就绪（静默执行，不阻塞 UI）
  /// 流程：检查运行 → 检查安装 → 检查 npm → 安装依赖 → 安装 connector → 启动
  Future<void> ensureReady() async {
    try {
      if (isRunning.value) return;

      if (!isInstalled.value) {
        // 本机 Node 缺失或太老：不走 brew/winget（要 sudo/UAC、脚本和包都在
        // GitHub 上，国内拉不动），直接装私有运行时，官方源不通自动切镜像。
        if (!(await _checkNpmReady()).ok) {
          if (!await installNodeRuntime()) return; // 等用户在状态页手动处理
          if (!(await _checkNpmReady()).ok) return;
        }
        // 安装 connector
        final installed = await install();
        if (!installed) return;
      }

      // 启动
      await start();
    } catch (e) {
      debugPrint('[ensureReady] 自动保障失败: $e');
      lastError.value = e.toString();
    }
  }

  /// 检查 npm 是否可用（专供 ensureReady 使用）
  Future<PrerequisiteResult> _checkNpmReady() async {
    try {
      final nodeResult = await _shellRun('node --version');
      if (nodeResult.exitCode != 0) return await _nodeNotFound();
      final version = (nodeResult.stdout as String).trim();
      final major = int.tryParse(
            version.replaceFirst('v', '').split('.').first,
          ) ??
          0;
      if (major < 18) {
        final message = 'system_node_version_low'.trParams({
          'version': version,
        });
        if (Platform.isMacOS && await _hasBrew()) {
          return PrerequisiteResult(
            ok: false,
            message: message,
            installCommand: 'brew upgrade node',
          );
        }
        return PrerequisiteResult(ok: false, message: message);
      }
      final npmResult = await _shellRun('npm --version');
      if (npmResult.exitCode != 0) {
        return PrerequisiteResult(
          ok: false,
          message: 'system_npm_not_found'.tr,
        );
      }
      return const PrerequisiteResult(ok: true);
    } catch (_) {
      return await _nodeNotFound();
    }
  }

  /// client_type 对应的本地命令名
  static const Map<String, String> clientTypeCommands = {
    'claude': 'claude',
    'codex': 'codex',
    'gemini': 'gemini',
    'qwen': 'qwen',
    'pi': 'pi',
    'cursor': 'agent',
    'reasonix': 'reasonix',
    'codewhale': 'codewhale',
    'openhuman': 'openhuman',
    'kiro': 'kiro-cli',
    'opencode': 'opencode',
  };

  /// 各 agent 的安装配置
  static const Map<String, AgentInstallInfo> installInfos = {
    'claude': AgentInstallInfo(
      command: 'claude',
      method: InstallMethod.npm,
      packageName: '@anthropic-ai/claude-code',
      prerequisite: 'Node.js >= 18',
    ),
    'codex': AgentInstallInfo(
      command: 'codex',
      method: InstallMethod.npm,
      packageName: '@openai/codex',
      prerequisite: 'Node.js >= 18',
    ),
    'gemini': AgentInstallInfo(
      command: 'gemini',
      method: InstallMethod.npm,
      packageName: '@google/gemini-cli',
      prerequisite: 'Node.js >= 18',
    ),
    'qwen': AgentInstallInfo(
      command: 'qwen',
      method: InstallMethod.npm,
      packageName: '@qwen-code/qwen-code',
      prerequisite: 'Node.js >= 18',
    ),
    'pi': AgentInstallInfo(
      command: 'pi',
      method: InstallMethod.npm,
      packageName: '@earendil-works/pi-coding-agent',
      prerequisite: 'Node.js >= 18',
    ),
    'cursor': AgentInstallInfo(
      command: 'agent',
      method: InstallMethod.manual,
      installHint: 'system_cursor_cli_hint',
    ),
    'reasonix': AgentInstallInfo(
      command: 'reasonix',
      method: InstallMethod.npm,
      packageName: 'reasonix',
      prerequisite: 'Node.js >= 18',
    ),
    'codewhale': AgentInstallInfo(
      command: 'codewhale',
      method: InstallMethod.npm,
      packageName: 'codewhale',
      prerequisite: 'Node.js >= 18',
    ),
    'openhuman': AgentInstallInfo(
      command: 'openhuman',
      method: InstallMethod.script,
      installScript:
          'curl -fsSL https://raw.githubusercontent.com/tinyhumansai/openhuman/main/scripts/install.sh | bash',
      installScriptWindows:
          "powershell -Command \"irm https://raw.githubusercontent.com/tinyhumansai/openhuman/main/scripts/install.ps1 | iex\"",
      installHint: 'system_official_script_install',
    ),
    'kiro': AgentInstallInfo(
      command: 'kiro-cli',
      method: InstallMethod.script,
      installScript: 'curl -fsSL https://cli.kiro.dev/install | bash',
      installScriptWindows:
          "powershell -Command \"irm 'https://cli.kiro.dev/install.ps1' | iex\"",
      installHint: 'system_official_script_install',
    ),
    'opencode': AgentInstallInfo(
      command: 'opencode',
      method: InstallMethod.npm,
      packageName: 'opencode-ai',
      prerequisite: 'Node.js >= 18',
    ),
  };

  /// 支持的 client_type 列表
  static List<String> get supportedClientTypes => clientTypeCommands.keys.toList();

  /// 检查指定 client_type 对应的命令是否存在，返回实际路径
  Future<String?> resolveCommandPath(String clientType) async {
    final cmd = clientTypeCommands[clientType];
    if (cmd == null) return null;
    try {
      final ProcessResult result;
      if (Platform.isWindows) {
        result = await Process.run('where', [cmd])
            .timeout(const Duration(seconds: 5));
      } else {
        // login shell 确保加载 nvm/homebrew 等 PATH
        result = await Process.run('bash', ['-lc', 'command -v $cmd'])
            .timeout(const Duration(seconds: 5));
      }
      if (result.exitCode == 0) {
        final path = (result.stdout as String).trim().split('\n').first.trim();
        if (path.isNotEmpty) return path;
      }
    } catch (_) {}
    return null;
  }

  /// 检查指定 client_type 对应的命令是否存在
  Future<bool> isCommandAvailable(String clientType) async {
    return (await resolveCommandPath(clientType)) != null;
  }

  /// 检查前置依赖是否满足
  Future<PrerequisiteResult> checkPrerequisite(String clientType) async {
    final info = installInfos[clientType];
    if (info == null) return const PrerequisiteResult(ok: true);
    switch (info.method) {
      case InstallMethod.npm:
        try {
          final nodeResult = await _shellRun('node --version');
          if (nodeResult.exitCode != 0) {
            return _nodeNotFound();
          }
          final version = (nodeResult.stdout as String).trim();
          final major = int.tryParse(version.replaceFirst('v', '').split('.').first) ?? 0;
          if (major < 18) {
            if (Platform.isMacOS && await _hasBrew()) {
              return PrerequisiteResult(ok: false, message: 'system_node_version_low'.trParams({'version': version}), installCommand: 'brew upgrade node');
            }
            return PrerequisiteResult(ok: false, message: 'system_node_version_low'.trParams({'version': version}), installHint: 'system_node_upgrade_hint'.tr);
          }
          // 检查 npm
          final npmResult = await _shellRun('npm --version');
          if (npmResult.exitCode != 0) {
            return PrerequisiteResult(
              ok: false,
              message: 'system_npm_not_found'.tr,
              installHint: 'system_npm_install_hint'.tr,
            );
          }
          return const PrerequisiteResult(ok: true);
        } catch (_) {
          return _nodeNotFound();
        }
      case InstallMethod.goInstall:
        try {
          final goResult = await _shellRun('go version');
          if (goResult.exitCode != 0) {
            return _goNotFound();
          }
          return const PrerequisiteResult(ok: true);
        } catch (_) {
          return _goNotFound();
        }
      case InstallMethod.script:
        if (Platform.isWindows) {
          // Windows 用 PowerShell，检查 powershell 可用
          try {
            final psResult = await Process.run('powershell', ['-Command', 'echo ok'])
                .timeout(const Duration(seconds: 5));
            if (psResult.exitCode != 0) {
              return PrerequisiteResult(ok: false, message: 'system_powershell_not_found'.tr);
            }
            return const PrerequisiteResult(ok: true);
          } catch (_) {
            return PrerequisiteResult(ok: false, message: 'system_powershell_not_found'.tr);
          }
        }
        // macOS/Linux 检查 curl
        try {
          final curlResult = await _shellRun('curl --version');
          if (curlResult.exitCode != 0) {
            return PrerequisiteResult(
              ok: false,
              message: 'system_curl_not_found'.tr,
              installCommand: Platform.isMacOS ? 'brew install curl' : null,
              installHint: Platform.isMacOS ? null : 'system_curl_install_hint'.tr,
            );
          }
        } catch (_) {
          return PrerequisiteResult(ok: false, message: 'system_curl_not_found'.tr);
        }
        return const PrerequisiteResult(ok: true);
      case InstallMethod.manual:
        return const PrerequisiteResult(ok: true);
    }
  }

  Future<PrerequisiteResult> _nodeNotFound() async {
    if (Platform.isMacOS) {
      if (await _hasBrew()) {
        return PrerequisiteResult(ok: false, message: 'system_node_not_found'.tr, installCommand: 'brew install node');
      }
      return PrerequisiteResult(
        ok: false,
        message: 'system_node_no_brew'.tr,
        installCommand: '/bin/bash -c "\$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)" && brew install node',
        installHint: 'system_install_brew_then_node'.tr,
      );
    }
    if (Platform.isWindows) {
      return PrerequisiteResult(ok: false, message: 'system_node_not_found'.tr, installHint: 'system_node_download_hint'.tr);
    }
    return PrerequisiteResult(
      ok: false,
      message: 'system_node_not_found'.tr,
      installCommand: 'curl -fsSL https://fnm.vercel.app/install | bash && source ~/.bashrc && fnm install 22',
      installHint: 'system_node_fnm_hint'.tr,
    );
  }

  Future<PrerequisiteResult> _goNotFound() async {
    if (Platform.isMacOS) {
      if (await _hasBrew()) {
        return PrerequisiteResult(ok: false, message: 'system_go_not_found'.tr, installCommand: 'brew install go');
      }
      return PrerequisiteResult(
        ok: false,
        message: 'system_go_no_brew'.tr,
        installCommand: '/bin/bash -c "\$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)" && brew install go',
        installHint: 'system_install_brew_then_go'.tr,
      );
    }
    if (Platform.isWindows) {
      return PrerequisiteResult(ok: false, message: 'system_go_not_found'.tr, installHint: 'system_go_download_hint'.tr);
    }
    return PrerequisiteResult(ok: false, message: 'system_go_not_found'.tr, installHint: 'system_go_install_hint'.tr);
  }

  Future<bool> _hasBrew() async {
    try {
      final result = await Process.run('which', ['brew']).timeout(const Duration(seconds: 3));
      return result.exitCode == 0;
    } catch (_) {
      return false;
    }
  }

  /// 安装前置依赖
  Future<bool> installPrerequisite(String command) async {
    lastError.value = '';
    installLog.value = '';
    return await _runInstallShell(
      command,
      clientType: '', // 不需要校验
      timeoutSeconds: 180,
      friendlyName: 'system_install_dependency'.tr,
      skipVerify: true,
    );
  }

  /// 安装日志（供 UI 实时显示）
  final installLog = ''.obs;

  /// 安装指定 client_type 对应的命令
  Future<bool> installAgentCommand(String clientType) async {
    final info = installInfos[clientType];
    if (info == null) return false;

    lastError.value = '';
    installLog.value = '';

    try {
      switch (info.method) {
        case InstallMethod.npm:
          return await npmInstall(
            '${info.packageName}',
            clientType: clientType,
            skipVerify: false,
          );

        case InstallMethod.goInstall:
          return await _runInstallShell(
            'go install ${info.packageName}',
            clientType: clientType,
            timeoutSeconds: 180,
            friendlyName: 'go install',
          );

        case InstallMethod.script:
          // 按平台选择安装脚本：Windows 用 PowerShell 脚本，其余用 shell 脚本。
          final script =
              Platform.isWindows ? info.installScriptWindows : info.installScript;
          if (script == null || script.trim().isEmpty) {
            lastError.value =
                (info.installHint ?? 'system_manual_install_required').tr;
            return false;
          }
          return await _runInstallShell(
            script,
            clientType: clientType,
            timeoutSeconds: 120,
            friendlyName: 'system_install_script'.tr,
          );

        case InstallMethod.manual:
          lastError.value =
              (info.installHint ?? 'system_manual_install_required').tr;
          return false;
      }
    } catch (e) {
      if (e is TimeoutException) {
        lastError.value = 'system_install_timeout'.tr;
      } else {
        lastError.value = 'system_install_error'.trParams({'error': e.toString().split('\n').first});
      }
      return false;
    }
  }

  /// 跨平台 shell 执行（单条命令，用于检测）
  Future<ProcessResult> _shellRun(String rawCommand) {
    final command = withRuntimePath(rawCommand);
    if (Platform.isWindows) {
      return processRun('cmd', ['/c', command]);
    }
    return processRun('bash', ['-lc', command]);
  }

  /// 跨平台 shell 安装执行，带实时日志和超时
  /// 是否有安装类 shell 正在跑（connector/前置依赖/agent 安装、回退装包）。
  /// 看门狗以此互斥：npm 写文件写到一半时，安装重检可能已经探得到二进制，
  /// 抢跑 start 会拉起残缺安装。
  bool _installShellInFlight = false;

  Future<bool> _runInstallShell(
    String command, {
    required String clientType,
    required int timeoutSeconds,
    required String friendlyName,
    bool skipVerify = false,
  }) async {
    _installShellInFlight = true;
    try {
      return await _runInstallShellInner(
        command,
        clientType: clientType,
        timeoutSeconds: timeoutSeconds,
        friendlyName: friendlyName,
        skipVerify: skipVerify,
      );
    } finally {
      _installShellInFlight = false;
    }
  }

  Future<bool> _runInstallShellInner(
    String rawCommand, {
    required String clientType,
    required int timeoutSeconds,
    required String friendlyName,
    bool skipVerify = false,
  }) async {
    lastInstallTimedOut = false;
    installLog.value = '${'system_executing'.trParams({'name': friendlyName})}\n';
    final command = withRuntimePath(rawCommand);

    final String executable;
    final List<String> args;
    if (Platform.isWindows) {
      executable = 'cmd';
      args = ['/c', command];
    } else {
      executable = 'bash';
      args = ['-lc', command];
    }

    final process = await Process.start(executable, args, environment: Platform.environment);

    final logBuffer = StringBuffer();

    process.stdout.transform(const SystemEncoding().decoder).listen((data) {
      logBuffer.write(data);
      installLog.value = logBuffer.toString();
    });
    process.stderr.transform(const SystemEncoding().decoder).listen((data) {
      logBuffer.write(data);
      installLog.value = logBuffer.toString();
    });

    final exitCode = await process.exitCode.timeout(
      Duration(seconds: timeoutSeconds),
      onTimeout: () {
        process.kill();
        return -1;
      },
    );

    if (exitCode == -1) {
      lastInstallTimedOut = true;
      lastError.value = 'system_install_timeout_seconds'.trParams({'seconds': '$timeoutSeconds'});
      return false;
    }

    if (exitCode == 0) {
      installLog.value += '\n${'system_install_done_verifying'.tr}\n';
      if (skipVerify) return true;
      return await _verifyInstall(clientType);
    }

    final output = logBuffer.toString();
    lastError.value = _friendlyInstallError(output, friendlyName);
    return false;
  }

  /// 将安装错误转为用户友好提示
  String _friendlyInstallError(String output, String friendlyName) {
    final lower = output.toLowerCase();
    if (lower.contains('eacces') || lower.contains('permission denied')) {
      return 'system_permission_denied'.trParams({'cmd': friendlyName});
    }
    if (lower.contains('enotfound') || lower.contains('network') || lower.contains('timeout')) {
      return 'system_network_failed'.tr;
    }
    if (lower.contains('404') || lower.contains('not found')) {
      return 'system_package_not_found'.tr;
    }
    if (lower.contains('enospc')) {
      return 'system_disk_full'.tr;
    }
    // 取最后一行有意义的错误
    final lines = output.trim().split('\n').where((l) => l.trim().isNotEmpty).toList();
    final lastLine = lines.isNotEmpty ? lines.last.trim() : '';
    return lastLine.length > 100 ? '${lastLine.substring(0, 100)}...' : (lastLine.isNotEmpty ? lastLine : 'system_operation_failed'.trParams({'name': friendlyName}));
  }

  /// 安装后校验命令是否可用
  Future<bool> _verifyInstall(String clientType) async {
    // 多次尝试，给文件系统和 PATH 刷新时间
    for (var i = 0; i < 3; i++) {
      await Future.delayed(const Duration(milliseconds: 800));
      final path = await resolveCommandPath(clientType);
      if (path != null) {
        installLog.value += '${'system_verified'.trParams({'path': path})}\n';
        return true;
      }
    }
    lastError.value = 'system_installed_cmd_not_found'.tr;
    return false;
  }

  /// 获取本机连接器当前配置的 agent 列表（通过 Admin API）。
  /// 这是"某个 agent 是否在本机"的唯一真值来源——backend 的 Agent 表按账号存，
  /// 同一账号的 agent 可能分布在多台设备的连接器上，backend 侧看不出归属哪台机器。
  /// null = 连接器不在线/请求失败（未知，调用方不能当成"本机没有 agent"来过滤）。
  Future<List<Map<String, dynamic>>?> getAgents() async {
    try {
      final resp = await _dio.get('$_adminBase/api/agents');
      if (resp.statusCode == 200 && resp.data is List) {
        return List<Map<String, dynamic>>.from(resp.data);
      }
    } catch (_) {}
    return null;
  }

  /// 当前已开启"Grix中转"的 agent 名单（连接器本地状态，按 agent）。
  /// 中转是连接器侧的进程级接管：只有名单内的 agent 起进程时才走 Grix 网关，
  /// 其余 agent 直连自己的官方账号。
  /// **连接器不在线时返回 null**（"未知"，不是"全关"）——不能把未知显示成关，
  /// 否则用户会以为中转已停、实际不知道。
  Future<Set<String>?> getRelayAgents() async {
    try {
      final resp = await _dio.get('$_adminBase/api/proxy');
      if (resp.statusCode == 200) {
        final list = resp.data['relayAgents'];
        if (list is List) return list.map((e) => e.toString()).toSet();
      }
    } catch (_) {}
    return null;
  }

  /// 适配器还带着**旧代理 env** 的 agent：它此刻实际走的还是旧账号，要到下一条消息
  /// 派发前被回收才真正切过去。
  /// 注意这不是"正在忙"——忙会随任务结束消失，而这个状态会一直挂到适配器真被换掉。
  /// UI 必须据此如实提示，不能一亮开关就说已经在走中转。
  Future<Set<String>> getStaleRelayAgents() async {
    try {
      final resp = await _dio.get('$_adminBase/api/proxy');
      if (resp.statusCode == 200) {
        final list = resp.data['staleRelayAgents'];
        if (list is List) return list.map((e) => e.toString()).toSet();
      }
    } catch (_) {}
    return <String>{};
  }

  /// 为单个 agent 开/关"Grix中转"的结果。
  /// - ok：成功（连接器已顺带重启该 agent 让开关生效；busy=true 表示 agent 正忙、
  ///   没硬杀它，等它下次重启才生效）
  /// - needsKey：连接器本机没有这个 agent 的网关虚拟 Key（换机/重装/数据丢失），
  ///   开不了中转——必须回落去服务端重新发一把 Key，绝不能让它"以为在走中转、
  ///   实际直连自己的账号"。新版本连接器（≥3.16.0）会自己经 WS 闭环要凭证，不回这个。
  /// - offline：连接器不在线
  /// - failed：其他失败
  /// [model]：原生配置类型（qwen/pi/hermes 等）开中转必填；MITM 类型（claude/codex）不传。
  Future<GrixRelayToggleResult> setAgentRelay(String name, bool enabled,
      {String? model}) async {
    try {
      final resp = await _dio.put(
        '$_adminBase/api/proxy/agents/${Uri.encodeComponent(name)}/enabled',
        data: {
          'enabled': enabled,
          if (model != null && model.isNotEmpty) 'model': model,
        },
        // 连接器答了就不算"不在线"：5xx 也要如实报失败，不能误导成连接器离线。
        options: Options(validateStatus: (s) => s != null),
      );
      if (resp.statusCode == 200) {
        final busy = resp.data is Map && resp.data['busy'] == true;
        return busy ? GrixRelayToggleResult.okButBusy : GrixRelayToggleResult.ok;
      }
      // 新版本连接器对"开"会走 relay_credential_request WS 闭环，失败按 code 细分，
      // UI 据此如实提示（WS 不在线 / 超时 / 被取消 / 服务端太旧 / 服务端拒绝）。
      final code = resp.data is Map ? '${resp.data['code'] ?? ''}' : '';
      final errMsg = resp.data is Map ? '${resp.data['error'] ?? ''}' : '';
      if (resp.statusCode == 409 && code == 'RELAY_TOGGLE_CANCELLED') {
        return GrixRelayToggleResult.cancelled;
      }
      if (resp.statusCode == 409) return GrixRelayToggleResult.needsKey;
      if (resp.statusCode == 503 && code == 'RELAY_WS_OFFLINE') {
        return GrixRelayToggleResult.wsOffline;
      }
      if (resp.statusCode == 504 && code == 'RELAY_CREDENTIAL_TIMEOUT') {
        return GrixRelayToggleResult.credentialTimeout;
      }
      if (resp.statusCode == 400 && code == 'RELAY_UNSUPPORTED') {
        return GrixRelayToggleResult.unsupportedServer;
      }
      if (resp.statusCode == 502 && code == 'RELAY_CREDENTIAL_FAILED') {
        if (errMsg.isNotEmpty) lastError.value = errMsg;
        return GrixRelayToggleResult.credentialFailed;
      }
      lastError.value = 'HTTP ${resp.statusCode}';
      return GrixRelayToggleResult.failed;
    } on DioException catch (e) {
      lastError.value = '$e';
      // 只有真的连不上（无响应）才算离线
      return e.response == null
          ? GrixRelayToggleResult.offline
          : GrixRelayToggleResult.failed;
    } catch (e) {
      lastError.value = '$e';
      return GrixRelayToggleResult.failed;
    }
  }

  /// 把服务端刚签发的一次性明文中转凭证应用到本地连接器（桌面端直连本地Connector改造的
  /// 新入口，配套服务端的 GatewayService.issueAgentRelayCredential）。跟旧的
  /// [setAgentRelay] 不同：这里不依赖服务端事先通过 Redis/WS 广播把Key推给 connector，
  /// 凭证由调用方直接在这次PUT请求体里带过来，用完立刻从内存里释放——调用方不得把
  /// virtualKey 落盘或打日志，这里同样不打印请求体。
  /// 200 时连接器会照旧应用配置并按需重启该 agent 让开关生效，busy=true 表示 agent
  /// 正忙没被硬杀、等它下次重启才生效，语义与 [setAgentRelay] 一致。
  Future<GrixApplyRelayCredentialResult> applyRelayCredential(
    String name, {
    required String agentId,
    required String virtualKey,
    required String anthropicBaseUrl,
    required String openaiBaseUrl,
    String? model,
  }) async {
    try {
      final resp = await _dio.put(
        '$_adminBase/api/proxy/agents/${Uri.encodeComponent(name)}/relay-credential',
        data: {
          'agent_id': agentId,
          'virtual_key': virtualKey,
          'anthropic_base_url': anthropicBaseUrl,
          'openai_base_url': openaiBaseUrl,
          // 原生配置类型（qwen/pi/hermes等）必填：connector ≥3.6.0 会把它写进该
          // agent CLI 的原生配置；MITM 类型（claude/codex）不传，沿用网关映射兜底。
          if (model != null && model.isNotEmpty) 'model': model,
        },
        // 连接器答了就不算"不在线"：5xx 也要如实报失败，不能误导成连接器离线。
        options: Options(validateStatus: (s) => s != null),
      );
      if (resp.statusCode == 200) {
        final busy = resp.data is Map && resp.data['busy'] == true;
        return busy ? GrixApplyRelayCredentialResult.okButBusy : GrixApplyRelayCredentialResult.ok;
      }
      lastError.value = 'HTTP ${resp.statusCode}';
      return GrixApplyRelayCredentialResult.failed;
    } on DioException catch (e) {
      lastError.value = '${e.type}';
      // 只有真的连不上（无响应）才算离线；错误信息不带 e 本身，避免 DioException 里
      // 附带的请求体（含明文Key）被间接打进 lastError 展示到界面或日志。
      return e.response == null
          ? GrixApplyRelayCredentialResult.offline
          : GrixApplyRelayCredentialResult.failed;
    } catch (e) {
      lastError.value = e.runtimeType.toString();
      return GrixApplyRelayCredentialResult.failed;
    }
  }

  /// 重启指定 agent
  Future<bool> restartAgent(String name) async {
    try {
      final resp = await _dio.post(
        '$_adminBase/api/agents/$name/restart',

      );
      return resp.statusCode == 200;
    } catch (_) {
      return false;
    }
  }

  /// 添加 agent
  Future<bool> addAgent(Map<String, dynamic> config) async {
    try {
      final resp = await _dio.post(
        '$_adminBase/api/agents',
        data: config,
        options: Options(receiveTimeout: const Duration(seconds: 60)),
      );
      if (resp.statusCode == 201) {
        await checkHealth();
        return true;
      }
      lastError.value = resp.data?['error'] ?? 'system_add_failed'.tr;
      return false;
    } catch (e) {
      lastError.value = e is DioException
          ? (e.response?.data?['error'] ?? 'system_add_failed'.tr)
          : e.toString();
      return false;
    }
  }

  /// 移除指定 agent
  Future<bool> removeAgent(String name) async {
    try {
      final resp = await _dio.delete(
        '$_adminBase/api/agents/$name',

      );
      if (resp.statusCode == 204) {
        await checkHealth();
        return true;
      }
      return false;
    } catch (_) {
      return false;
    }
  }


  // ==================== Install API ====================

  /// GET /api/install → 获取可安装 agent 列表
  Future<List<InstallableAgent>> listInstallable() async {
    try {
      final resp = await _dio.get(
        '$_adminBase/api/install',
        options: Options(receiveTimeout: const Duration(seconds: 10)),
      );
      if (resp.statusCode == 200) {
        final list = resp.data as List? ?? [];
        return list
            .map((a) => InstallableAgent.fromJson(a as Map<String, dynamic>))
            .toList();
      }
    } catch (e) {
      debugPrint('[listInstallable] error: $e');
    }
    return [];
  }

  /// POST /api/install → 触发安装
  Future<bool> installAgentViaApi(String agentType) async {
    try {
      lastError.value = '';
      final resp = await _dio.post(
        '$_adminBase/api/install',
        data: {'agentType': agentType},
        options: Options(receiveTimeout: const Duration(seconds: 120)),
      );
      if (resp.statusCode == 200 || resp.statusCode == 201) {
        return true;
      }
      lastError.value =
          _extractErrorMessage(resp.data) ?? 'agent_installer_request_failed'.tr;
      return false;
    } catch (e) {
      lastError.value = e is DioException
          ? (_extractErrorMessage(e.response?.data) ??
              'agent_installer_request_failed'.tr)
          : e.toString();
      return false;
    }
  }

  /// 从 connector 安装 API 的响应中提取错误信息字符串
  /// 响应中 error 字段可能是 String 或 Map<String, dynamic>
  static String? _extractErrorMessage(dynamic data) {
    if (data == null) return null;
    if (data is String) return data;
    if (data is Map<String, dynamic>) {
      final error = data['error'];
      if (error is String) return error;
      if (error is Map<String, dynamic>) {
        return error['message'] as String? ?? error['code']?.toString();
      }
    }
    return null;
  }

  /// GET /api/install/:agentType → 查询安装进度
  Future<InstallProgress> getInstallProgress(String agentType) async {
    try {
      final resp = await _dio.get(
        '$_adminBase/api/install/$agentType',
        options: Options(receiveTimeout: const Duration(seconds: 10)),
      );
      if (resp.statusCode == 200) {
        return InstallProgress.fromJson(resp.data as Map<String, dynamic>);
      }
    } catch (e) {
      debugPrint('[getInstallProgress] error: $e');
    }
    return const InstallProgress(status: 'unknown');
  }

  // ==================== Probe API ====================

  /// 探测所有 agent 状态（读操作免鉴权）
  Future<void> probeAll({bool fresh = false, bool conversation = false}) async {
    if (!isRunning.value) {
      probeResults.clear();
      installedClients.clear();
      probeSummary.value = null;
      return;
    }

    probeLoading.value = true;
    try {
      final query = <String, dynamic>{};
      if (fresh) query['fresh'] = 'true';
      if (conversation) query['conversation'] = 'true';
      query['timeoutMs'] = '15000';

      final resp = await _dio.get(
        '$_adminBase/api/probe',
        queryParameters: query,
        options: Options(receiveTimeout: const Duration(seconds: 20)),
      );
      if (resp.statusCode == 200) {
        // 探测期间 connector 可能已经掉线（结果已被清空），此时不要把这批
        // 陈旧结果写回去，否则离线态会残留一份看似正常的工具栏。
        if (!isRunning.value) return;
        final data = resp.data as Map<String, dynamic>;
        probeSummary.value = ProbeSummary(
          total: data['total'] ?? 0,
          healthy: data['healthy'] ?? 0,
          degraded: data['degraded'] ?? 0,
          unavailable: data['unavailable'] ?? 0,
          probedAt: data['probed_at'] ?? 0,
          durationMs: data['duration_ms'] ?? 0,
        );
        final agentList = data['agents'] as List? ?? [];
        probeResults.value = agentList
            .map((a) => AgentProbeResult.fromJson(a as Map<String, dynamic>))
            .toList();
        final clientList = data['installed_clients'] as List? ?? [];
        installedClients.value = clientList
            .map((a) => InstalledClientCommand.fromJson(a as Map<String, dynamic>))
            .toList();
        lastError.value = '';
      }
    } catch (e) {
      debugPrint('[probeAll] error: $e');
      lastError.value = 'system_probe_failed'.trParams({'error': '$e'});
    } finally {
      probeLoading.value = false;
    }
  }

  /// 探测单个 agent（读操作免鉴权）
  Future<AgentProbeResult?> probeSingle(String name, {bool fresh = false}) async {
    if (!isRunning.value) return null;

    try {
      final query = <String, dynamic>{};
      if (fresh) query['fresh'] = 'true';

      final resp = await _dio.get(
        '$_adminBase/api/probe/$name',
        queryParameters: query,
        options: Options(receiveTimeout: const Duration(seconds: 15)),
      );
      if (resp.statusCode == 200) {
        return AgentProbeResult.fromJson(resp.data as Map<String, dynamic>);
      }
    } catch (_) {}
    return null;
  }

  void _markOffline(String error) {
    isRunning.value = false;
    agents.clear();
    uptime.value = 0;
    pid.value = 0;
    probeResults.clear();
    probeSummary.value = null;
    wsConnected.value = 0;
    wsTotal.value = 0;
    lastError.value = error;

    // 在线没撑满稳定窗口就掉线：按一次失败计入退避。崩溃循环（起来几秒就崩）
    // 的拉起节奏随失败次数拉长，而不是每个轮询周期立刻 spawn 一次。
    final online = _onlineSince;
    _onlineSince = null;
    if (online != null && clock().difference(online) < stableOnlineWindow) {
      _consecutiveFailures++;
      _nextRestartAt = clock().add(connectorRestartBackoff(_consecutiveFailures));
    }

    // 每次探测到离线都尝试拉起，退避由 _keepAlive 自己把关
    _keepAlive();
  }

  /// 保活：探测到连接器离线就拉起，失败按指数退避持续重试
  Future<void> _keepAlive() async {
    if (!PlatformCapability.isDesktop) return;
    // start() 内部会再跑一次健康检查，重入会把同一次拉起算成多次失败
    if (_restartInFlight) return;
    // 有安装类 shell 在跑（ensureReady 自举、用户手动安装、回退装包）时不抢跑：
    // npm 写到一半就被探到二进制、拉起残缺安装，比多等一个轮询周期糟得多
    if (_installShellInFlight) return;
    // 连接器有升级事务在途：daemon 按计划自杀重启，离线是预期内的。
    // guardian 正在原子切包/激活/验证，此刻杀进程、装包、拉起全会跟它打架，
    // 停手到事务收场（healthz 亲口说没事务）或宽限窗超时（事务失控才接管）。
    final upgradeSeen = _upgradeSeenAt;
    if (upgradeSeen != null &&
        clock().difference(upgradeSeen) < upgradeStandDownWindow) {
      return;
    }

    final nextAt = _nextRestartAt;
    if (nextAt != null && clock().isBefore(nextAt)) return;

    _restartInFlight = true;
    try {
      // 安装态不能只信启动时那一次探测：shell 偶发超时会误判成未装，把看门狗
      // 永久锁死；用户会话中途手动装上也要能被接住。离线周期里持续重检，
      // 检不到按一次失败计入退避，避免每个轮询周期都 spawn shell。
      if (!isInstalled.value) {
        await checkInstalled();
        if (!isInstalled.value) {
          _consecutiveFailures++;
          _nextRestartAt =
              clock().add(connectorRestartBackoff(_consecutiveFailures));
          return;
        }
      }

      _restartCount++;
      // start() 成功后 _sawRunning 会被置真，先快照，区分冷启动拉起与掉线恢复
      final recovering = _sawRunning;
      debugPrint('[ConnectorWatchdog] ${clock().toIso8601String()} '
          'Connector 离线，第 $_restartCount 次拉起...');

      // 连拉不起来且手里还有旧 pid：daemon 大概率挂死了——进程活着占着单例锁，
      // /healthz 探不通，再多次 start 也无效。杀掉它再拉起（杀之前按命令行校验
      // 身份，防 pid 被复用后误杀无关进程）。
      if (_consecutiveFailures >= killEscalationThreshold && _lastKnownPid > 0) {
        debugPrint('[ConnectorWatchdog] ${clock().toIso8601String()} '
            '连续 $_consecutiveFailures 次拉起无效，尝试清理疑似挂死的旧进程 '
            'pid=$_lastKnownPid');
        await _killDaemonPid(_lastKnownPid);
        _lastKnownPid = 0;
      }

      // 杀进程都救不回来：多半是升级装上了起不来的版本，或安装本身已损坏。
      // 重装这台机器上最后一个稳定在线过的版本兜底。一轮离线期只试一次，
      // 装包失败也不重试——看门狗对 start 的无限重试仍在继续。
      if (!_rollbackAttempted &&
          _consecutiveFailures >= rollbackEscalationThreshold) {
        // 版本号必须是本服务自己存的规范 semver；解析一遍再上 shell，杜绝注入。
        // 还没有任何稳定在线过的版本（首装即起不来）：没有可退的目标，那就按
        // 「安装损坏」处理，重装 latest 修复。
        final lastGood = _parseSemver(lastGoodVersion.value)?.toString();
        final target = lastGood ?? 'latest';
        _rollbackAttempted = true;
        debugPrint('[ConnectorWatchdog] ${clock().toIso8601String()} '
            '连续 $_consecutiveFailures 次拉起失败，回退/重装 $target');
        CustomToast.show(
          lastGood != null
              ? 'system_rollback_attempt'.trParams({'version': lastGood})
              : 'system_repair_attempt'.tr,
          isError: true,
        );
        await rollbackInstall(target);
      }

      final success = await start();
      if (success) {
        debugPrint('[ConnectorWatchdog] ${clock().toIso8601String()} '
            '第 $_restartCount 次拉起成功');
        // 崩溃循环下每次拉起都"成功"过一瞬，不节流的话 toast 会随循环刷屏
        final lastToast = _lastRecoveryToastAt;
        if (recovering &&
            (lastToast == null ||
                clock().difference(lastToast) >= recoveryToastInterval)) {
          _lastRecoveryToastAt = clock();
          CustomToast.show('system_auto_restart_success'.tr, isError: false);
        }
      } else {
        _consecutiveFailures++;
        final backoff = connectorRestartBackoff(_consecutiveFailures);
        _nextRestartAt = clock().add(backoff);
        debugPrint('[ConnectorWatchdog] ${clock().toIso8601String()} '
            '第 $_restartCount 次拉起失败（连续 $_consecutiveFailures 次）: '
            '${lastError.value}，${backoff.inSeconds}s 后重试');
        // 无限重试，只在掉线恢复失败的首次提示，避免 toast 刷屏
        if (recovering && _consecutiveFailures == 1) {
          CustomToast.show('system_auto_restart_failed'.tr, isError: true);
        }
      }
    } finally {
      _restartInFlight = false;
    }
  }
}

/// 看门狗重试间隔：连续失败越多退得越久，10s 起步、5 分钟封顶。
/// 封顶是为了长期不可恢复时（如 connector 被卸载）不至于一直 spawn 进程。
Duration connectorRestartBackoff(int consecutiveFailures) {
  const min = Duration(seconds: 10);
  const max = Duration(minutes: 5);
  if (consecutiveFailures <= 1) return min;
  final shift = (consecutiveFailures - 1).clamp(0, 8);
  final delay = min * (1 << shift);
  return delay > max ? max : delay;
}

/// 安装方式
enum InstallMethod { npm, goInstall, script, manual }

/// Agent 安装信息
class AgentInstallInfo {
  final String command;
  final InstallMethod method;
  final String? packageName;
  final String? prerequisite;
  /// macOS / Linux 下的安装脚本（method 为 script 时使用）
  final String? installScript;
  /// Windows 下的安装脚本（method 为 script 时使用，通常是 PowerShell 命令）
  final String? installScriptWindows;
  final String? installHint;

  const AgentInstallInfo({
    required this.command,
    required this.method,
    this.packageName,
    this.prerequisite,
    this.installScript,
    this.installScriptWindows,
    this.installHint,
  });
}


// ==================== Probe 数据模型 ====================

class InstalledClientCommand {
  final String clientType;
  final String command;
  final bool installed;
  final String path;
  final String version;

  const InstalledClientCommand({
    required this.clientType,
    this.command = '',
    this.installed = false,
    this.path = '',
    this.version = '',
  });

  factory InstalledClientCommand.fromJson(Map<String, dynamic> json) => InstalledClientCommand(
        clientType: json['client_type'] ?? json['clientType'] ?? '',
        command: json['command'] ?? '',
        installed: json['installed'] ?? false,
        path: json['path'] ?? '',
        version: json['version'] ?? '',
      );
}

class ProbeSummary {
  final int total;
  final int healthy;
  final int degraded;
  final int unavailable;
  final int probedAt;
  final int durationMs;

  const ProbeSummary({
    this.total = 0,
    this.healthy = 0,
    this.degraded = 0,
    this.unavailable = 0,
    this.probedAt = 0,
    this.durationMs = 0,
  });
}

class ProbeCliInfo {
  final String command;
  final bool installed;
  final String path;
  final String version;

  const ProbeCliInfo({
    this.command = '',
    this.installed = false,
    this.path = '',
    this.version = '',
  });

  factory ProbeCliInfo.fromJson(Map<String, dynamic> json) => ProbeCliInfo(
        command: json['command'] ?? '',
        installed: json['installed'] ?? false,
        path: json['path'] ?? '',
        version: json['version'] ?? '',
      );
}

class ProbeConversationInfo {
  final bool attempted;
  final bool ok;
  final int? latencyMs;

  const ProbeConversationInfo({
    this.attempted = false,
    this.ok = false,
    this.latencyMs,
  });

  factory ProbeConversationInfo.fromJson(Map<String, dynamic> json) =>
      ProbeConversationInfo(
        attempted: json['attempted'] ?? false,
        ok: json['ok'] ?? false,
        latencyMs: json['latency_ms'],
      );
}

class ProbeProcessInfo {
  final bool started;
  final bool alive;
  final bool busy;

  const ProbeProcessInfo({
    this.started = false,
    this.alive = false,
    this.busy = false,
  });

  factory ProbeProcessInfo.fromJson(Map<String, dynamic> json) => ProbeProcessInfo(
        started: json['started'] ?? false,
        alive: json['alive'] ?? false,
        busy: json['busy'] ?? false,
      );
}

class AgentProbeResult {
  final String agentName;
  final String clientType;
  final String adapterType;
  final bool ok;
  final String status; // healthy, degraded, unavailable, error
  final bool cached;
  final int probedAt;
  final int durationMs;
  final ProbeCliInfo? cli;
  final ProbeConversationInfo? conversation;
  final ProbeProcessInfo? process;

  const AgentProbeResult({
    required this.agentName,
    this.clientType = '',
    this.adapterType = '',
    this.ok = false,
    this.status = 'unavailable',
    this.cached = false,
    this.probedAt = 0,
    this.durationMs = 0,
    this.cli,
    this.conversation,
    this.process,
  });

  factory AgentProbeResult.fromJson(Map<String, dynamic> json) => AgentProbeResult(
        agentName: json['agent_name'] ?? '',
        clientType: json['client_type'] ?? '',
        adapterType: json['adapter_type'] ?? '',
        ok: json['ok'] ?? false,
        status: json['status'] ?? 'unavailable',
        cached: json['cached'] ?? false,
        probedAt: json['probed_at'] ?? 0,
        durationMs: json['duration_ms'] ?? 0,
        cli: json['cli'] != null
            ? ProbeCliInfo.fromJson(json['cli'] as Map<String, dynamic>)
            : null,
        conversation: json['conversation'] != null
            ? ProbeConversationInfo.fromJson(json['conversation'] as Map<String, dynamic>)
            : null,
        process: json['process'] != null
            ? ProbeProcessInfo.fromJson(json['process'] as Map<String, dynamic>)
            : null,
      );
}

/// 前置依赖检查结果
class PrerequisiteResult {
  final bool ok;
  final String? message;
  /// 可自动安装的命令（为 null 表示需手动安装）
  final String? installCommand;
  /// 安装提示（当无法自动安装时）
  final String? installHint;
  const PrerequisiteResult({required this.ok, this.message, this.installCommand, this.installHint});
}

// ==================== Install 数据模型 ====================

/// 可安装的 agent 信息
class InstallableAgent {
  final String agentType;
  final String label;
  final String? description;
  final String? version;

  const InstallableAgent({
    required this.agentType,
    required this.label,
    this.description,
    this.version,
  });

  factory InstallableAgent.fromJson(Map<String, dynamic> json) => InstallableAgent(
        agentType: json['agentType'] ?? json['agent_type'] ?? '',
        label: json['label'] ?? '',
        description: json['description'],
        version: json['version'],
      );
}

/// 安装进度
class InstallProgress {
  /// pending / downloading / installing / done / error
  final String status;
  final double? progress;
  final String? message;
  final String? error;

  const InstallProgress({
    required this.status,
    this.progress,
    this.message,
    this.error,
  });

  bool get isDone => status == 'done';
  bool get isError => status == 'error' || status == 'unknown';
  bool get isActive => !isDone && !isError;

  factory InstallProgress.fromJson(Map<String, dynamic> json) => InstallProgress(
        status: json['status'] ?? 'unknown',
        progress: (json['progress'] as num?)?.toDouble(),
        message: json['message'],
        error: json['error'],
      );
}
