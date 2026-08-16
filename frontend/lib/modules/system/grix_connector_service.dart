import 'dart:async';
import 'dart:io';

import 'package:flutter/foundation.dart';

import 'package:dio/dio.dart';
import 'package:get/get.dart' hide Response;
import 'package:pub_semver/pub_semver.dart';

import '../../platform/platform_capability.dart';
import '../../shared/utils/toast_util.dart';

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
        if (isRunning.value) {
          // 连接器在线（自己拉起或外部启动），退避状态清零
          _sawRunning = true;
          _consecutiveFailures = 0;
          _nextRestartAt = null;
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
    try {
      final resp = await _dio.get(
        'https://registry.npmjs.org/grix-connector/latest',
        options: Options(receiveTimeout: const Duration(seconds: 8)),
      );
      if (resp.statusCode == 200) {
        latestVersion.value = (resp.data as Map<String, dynamic>)['version'] ?? '';
      }
    } catch (_) {
      // 网络不通时不影响主流程
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
    return await _runInstallShell(
      'npm install -g grix-connector',
      clientType: '',
      timeoutSeconds: 120,
      friendlyName: 'npm install grix-connector',
      skipVerify: true,
    ).then((ok) async {
      if (ok) await checkInstalled();
      return ok;
    });
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
        await Future.delayed(const Duration(seconds: 2));
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

  /// 桌面启动时自动保障 connector 就绪（静默执行，不阻塞 UI）
  /// 流程：检查运行 → 检查安装 → 检查 npm → 安装依赖 → 安装 connector → 启动
  Future<void> ensureReady() async {
    try {
      if (isRunning.value) return;

      if (!isInstalled.value) {
        // 检查 npm/node 是否可用（复用现有 npm 类型的前置检查逻辑）
        final prereq = await _checkNpmReady();
        if (!prereq.ok) {
          if (prereq.installCommand != null) {
            final ok = await installPrerequisite(prereq.installCommand!);
            if (!ok) return; // 前置安装失败，等用户在状态页手动处理
          } else {
            return; // 无法自动安装，需用户介入
          }
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
        if (Platform.isMacOS && await _hasBrew()) {
          return PrerequisiteResult(
            ok: false,
            message: 'Node.js 版本过低: $version',
            installCommand: 'brew upgrade node',
          );
        }
        return PrerequisiteResult(
          ok: false,
          message: 'Node.js 版本过低: $version',
        );
      }
      final npmResult = await _shellRun('npm --version');
      if (npmResult.exitCode != 0) {
        return const PrerequisiteResult(ok: false, message: 'npm 不可用');
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
          return await _runInstallShell(
            'npm install -g ${info.packageName}',
            clientType: clientType,
            timeoutSeconds: 120,
            friendlyName: 'npm install',
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
  Future<ProcessResult> _shellRun(String command) {
    if (Platform.isWindows) {
      return Process.run('cmd', ['/c', command]).timeout(const Duration(seconds: 5));
    }
    return Process.run('bash', ['-lc', command]).timeout(const Duration(seconds: 5));
  }

  /// 跨平台 shell 安装执行，带实时日志和超时
  Future<bool> _runInstallShell(
    String command, {
    required String clientType,
    required int timeoutSeconds,
    required String friendlyName,
    bool skipVerify = false,
  }) async {
    installLog.value = '${'system_executing'.trParams({'name': friendlyName})}\n';

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
    lastError.value = error;

    // 每次探测到离线都尝试拉起，退避由 _keepAlive 自己把关
    _keepAlive();
  }

  /// 保活：探测到连接器离线就拉起，失败按指数退避持续重试
  Future<void> _keepAlive() async {
    if (!PlatformCapability.isDesktop) return;
    if (!isInstalled.value) return;
    // start() 内部会再跑一次健康检查，重入会把同一次拉起算成多次失败
    if (_restartInFlight) return;

    final nextAt = _nextRestartAt;
    if (nextAt != null && DateTime.now().isBefore(nextAt)) return;

    _restartInFlight = true;
    try {
      _restartCount++;
      // start() 成功后 _sawRunning 会被置真，先快照，区分冷启动拉起与掉线恢复
      final recovering = _sawRunning;
      debugPrint('[ConnectorWatchdog] ${DateTime.now().toIso8601String()} '
          'Connector 离线，第 $_restartCount 次拉起...');

      final success = await start();
      if (success) {
        debugPrint('[ConnectorWatchdog] ${DateTime.now().toIso8601String()} '
            '第 $_restartCount 次拉起成功');
        if (recovering) {
          CustomToast.show('system_auto_restart_success'.tr, isError: false);
        }
      } else {
        _consecutiveFailures++;
        final backoff = connectorRestartBackoff(_consecutiveFailures);
        _nextRestartAt = DateTime.now().add(backoff);
        debugPrint('[ConnectorWatchdog] ${DateTime.now().toIso8601String()} '
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
