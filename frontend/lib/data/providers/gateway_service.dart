import 'package:dio/dio.dart';
import 'package:get/get.dart';

import '../../shared/utils/app_runtime_endpoints.dart';
import 'auth_service.dart';

/// "Grix中转"网关钱包（余额，USD计价，本轮不含充值）。
class GatewayWalletModel {
  const GatewayWalletModel({required this.id, required this.balance});

  final String id;
  final String balance;

  factory GatewayWalletModel.fromJson(Map<String, dynamic> json) {
    return GatewayWalletModel(
      id: json['id']?.toString() ?? '',
      balance: json['balance']?.toString() ?? '0',
    );
  }
}

/// 一条消费流水：用了哪个厂商/模型、token数、扣了多少钱。
class GatewayLedgerEntryModel {
  const GatewayLedgerEntryModel({
    required this.provider,
    required this.model,
    required this.cost,
    required this.status,
    required this.createdAt,
  });

  final String provider;
  final String model;
  final String cost;
  final String status;
  final String createdAt;

  factory GatewayLedgerEntryModel.fromJson(Map<String, dynamic> json) {
    return GatewayLedgerEntryModel(
      provider: json['provider']?.toString() ?? '',
      model: json['model']?.toString() ?? '',
      cost: json['cost']?.toString() ?? '0',
      status: json['status']?.toString() ?? '',
      createdAt: json['created_at']?.toString() ?? '',
    );
  }
}

/// 一条充值到账记录（充值本身目前仍由管理员在塘主后台代充）。
class GatewayTopupRecordModel {
  const GatewayTopupRecordModel({
    required this.creditedAmount,
    required this.paymentChannel,
    required this.createdAt,
  });

  final String creditedAmount;
  final String paymentChannel;
  final String createdAt;

  factory GatewayTopupRecordModel.fromJson(Map<String, dynamic> json) {
    return GatewayTopupRecordModel(
      creditedAmount: json['credited_amount']?.toString() ?? '0',
      paymentChannel: json['payment_channel']?.toString() ?? '',
      createdAt: json['created_at']?.toString() ?? '',
    );
  }
}

/// 一笔自助充值下单结果：充值单号 + 收银台跳转地址。
/// （后端响应里还有 status 字段，前端用不到，不解析。）
class GatewayTopupOrderModel {
  const GatewayTopupOrderModel({
    required this.topupOrderId,
    required this.payUrl,
  });

  final String topupOrderId;
  final String payUrl;

  factory GatewayTopupOrderModel.fromJson(Map<String, dynamic> json) {
    return GatewayTopupOrderModel(
      topupOrderId: json['topup_order_id']?.toString() ?? '',
      payUrl: json['pay_url']?.toString() ?? '',
    );
  }
}

/// 一个后端支持的模型 + 它的单价（USD / 每百万 token）。
/// 单价必须展示给用户：他在 Claude 的壳子里干活、花的却是这个模型的钱，
/// 看不见价格就不可能踏实。
class GatewayModelModel {
  const GatewayModelModel({
    required this.provider,
    required this.model,
    required this.inputPricePerM,
    required this.outputPricePerM,
  });

  final String provider;
  final String model;
  final String inputPricePerM;
  final String outputPricePerM;

  factory GatewayModelModel.fromJson(Map<String, dynamic> json) {
    return GatewayModelModel(
      provider: json['provider']?.toString() ?? '',
      model: json['model']?.toString() ?? '',
      inputPricePerM: json['input_price_per_m']?.toString() ?? '0',
      outputPricePerM: json['output_price_per_m']?.toString() ?? '0',
    );
  }
}

/// "Grix中转"的模型设置：兜底模型 + 模型映射表。
/// 映射与兜底都存在后端、由网关每次请求时解析，所以改完立即生效，不需要重启任何 Agent。
class GatewayRelaySettingsModel {
  const GatewayRelaySettingsModel({
    required this.defaultModel,
    required this.modelMap,
  });

  /// 兜底模型：所有没被映射命中的模型（含 Claude/Codex 将来发布的任何新模型）都走它。
  final String defaultModel;

  /// {客户端侧模型名: 后端支持的模型名}
  final Map<String, String> modelMap;

  factory GatewayRelaySettingsModel.fromJson(Map<String, dynamic> json) {
    final raw = json['model_map'];
    final map = <String, String>{};
    if (raw is Map) {
      raw.forEach((k, v) {
        final key = k?.toString() ?? '';
        final value = v?.toString() ?? '';
        if (key.isNotEmpty && value.isNotEmpty) map[key] = value;
      });
    }
    return GatewayRelaySettingsModel(
      defaultModel: json['default_model']?.toString() ?? '',
      modelMap: map,
    );
  }
}

/// 我名下某个托管Agent跟"Grix中转"的关系。
class GatewayAgentRelayStateModel {
  const GatewayAgentRelayStateModel({
    required this.agentId,
    required this.agentName,
    required this.clientType,
    required this.supported,
    required this.configured,
    this.relayModel = '',
    this.enabled,
    this.applied,
    this.appliedAt,
    this.stateKnown,
  });

  final String agentId;
  final String agentName;
  final String clientType;

  /// false = 该类型接不了中转（绑定自己账号或BYOK，不支持自定义端点）。
  /// 必须在界面上说明原因，否则用户会疑惑"为什么 Gemini 不扣我的钱"。
  final bool supported;

  /// true = 已签发专属虚拟Key，流量正走中转、正在花 Grix 余额。
  final bool configured;

  /// 启用中转时选定的模型（relay state 开启时是服务端 desired；空=未指定，走网关映射/兜底）。
  /// 列表回显"上次选中的模型"靠它。
  final String relayModel;

  /// —— relay state（migration 111）扩展字段，仅服务端 gateway.relay_state_enabled 开启时
  /// 返回；flag 关闭时整体缺席（null），前端按旧版语义展示（设计 §2.6）。——
  /// 期望开关（desired），是 Switch 的真值来源（M3 起取代连接器本机名单）。
  final bool? enabled;

  /// connector 最近一次有效回执的实际态（actual）。stateKnown=false 时不可信。
  final bool? applied;

  /// 最近一次有效回执时间（ISO 字符串，仅展示用）。
  final String? appliedAt;

  /// 服务端能否确知该 agent 的实际态（当前存在在线、通过权威校验、且能力位声明
  /// 支持 apply_relay_state 的 WS 连接）。false = agent 离线或对端是旧版 connector。
  final bool? stateKnown;

  factory GatewayAgentRelayStateModel.fromJson(Map<String, dynamic> json) {
    return GatewayAgentRelayStateModel(
      agentId: json['agent_id']?.toString() ?? '',
      agentName: json['agent_name']?.toString() ?? '',
      clientType: json['client_type']?.toString() ?? '',
      supported: json['supported'] == true,
      configured: json['configured'] == true,
      relayModel: json['relay_model']?.toString() ?? '',
      enabled: json['enabled'] is bool ? json['enabled'] as bool : null,
      applied: json['applied'] is bool ? json['applied'] as bool : null,
      appliedAt: json['applied_at']?.toString(),
      stateKnown: json['state_known'] is bool ? json['state_known'] as bool : null,
    );
  }
}

/// relay state 写操作（POST /agents/:id/relay）返回的最新 state；409 冲突时同样带回。
class GatewayRelayWriteStateModel {
  const GatewayRelayWriteStateModel({
    required this.agentId,
    required this.enabled,
    required this.relayModel,
    required this.revision,
    required this.applied,
    this.appliedAt,
  });

  final String agentId;
  final bool enabled;
  final String relayModel;

  /// 乐观锁版本号：下次写操作带 expected_revision 用它，不一致服务端返回 409。
  final int revision;
  final bool applied;
  final String? appliedAt;

  factory GatewayRelayWriteStateModel.fromJson(Map<String, dynamic> json) {
    return GatewayRelayWriteStateModel(
      agentId: json['agent_id']?.toString() ?? '',
      enabled: json['enabled'] == true,
      relayModel: json['relay_model']?.toString() ?? '',
      revision: json['revision'] is num ? (json['revision'] as num).toInt() : 0,
      applied: json['applied'] == true,
      appliedAt: json['applied_at']?.toString(),
    );
  }
}

/// [GatewayService.setAgentRelay] 的结果分类。
enum GatewaySetRelayStatus {
  /// 写入成功，state 带回最新 state（含新 revision）。
  ok,

  /// 409：expected_revision 与服务端不一致，state 带回最新 state，前端刷新后重试。
  conflict,

  /// 400：开启 + 原生配置类型缺 model（对应 need_model 文案，引导选模型后重试）。
  needModel,

  /// 503：服务端 feature flag 关闭，GET 同时回落旧语义，前端刷新后走降级路径。
  disabled,

  /// 其他失败（模型不可用、网络错误等）。
  failed,
}

class GatewaySetAgentRelayResult {
  const GatewaySetAgentRelayResult(this.status, [this.state]);

  final GatewaySetRelayStatus status;
  final GatewayRelayWriteStateModel? state;
}

/// 一把一次性明文中转凭证（见 GatewayService.issueAgentRelayCredential）。
/// 只应在内存里短暂存活——拿到手立刻转交本地Connector的凭证应用接口，用完就扔，
/// 绝不能写日志、落盘或缓存在任何持久化结构里。
class GatewayRelayCredentialModel {
  const GatewayRelayCredentialModel({
    required this.virtualKey,
    required this.anthropicBaseUrl,
    required this.openaiBaseUrl,
  });

  final String virtualKey;
  final String anthropicBaseUrl;
  final String openaiBaseUrl;

  factory GatewayRelayCredentialModel.fromJson(Map<dynamic, dynamic> json) {
    return GatewayRelayCredentialModel(
      virtualKey: json['virtual_key']?.toString() ?? '',
      anthropicBaseUrl: json['anthropic_base_url']?.toString() ?? '',
      openaiBaseUrl: json['openai_base_url']?.toString() ?? '',
    );
  }
}

/// "Grix中转"虚拟Key C端自助服务：给托管Agent接入网关虚拟Key、查自己的余额与账单。
/// 认Grix登录态(JWT)，走 /v1/gateway/* 这组接口。
class GatewayService extends GetxService {
  /// 目前"Grix中转"虚拟Key接得通的托管Agent类型，须跟后端
  /// gatewaySupportedAgentClientTypes 保持一致：Claude/Codex走MITM接管，
  /// 其余六类走原生配置写入（connector ≥3.6.0）。Gemini/Cursor/OpenHuman/Kiro/
  /// Copilot等绑定自己账号或BYOK不支持自定义端点，接不了，不在此列表里。
  /// Kimi 也不在列：模型/供应商由 ~/.kimi/config.toml 全局配置决定，
  /// connector 侧没有会话级注入机制，待补上后再登记。
  static const supportedClientTypes = {
    'claude', 'codex',
    'qwen', 'opencode', 'codewhale', 'reasonix', 'pi', 'hermes',
  };

  /// 走"原生配置直连网关"的类型（非 MITM 接管）。这些 CLI 的配置结构里模型名是
  /// 必填字段（connector ≥3.6.0 与服务端签发接口都会强校验），启用中转必须选定模型；
  /// Claude/Codex 走 MITM + 网关模型映射兜底，不需要也不应该在这里选模型。
  static const nativeProviderClientTypes = {
    'qwen', 'opencode', 'codewhale', 'reasonix', 'pi', 'hermes',
  };

  GatewayService({Dio? dio})
    : _dio =
          dio ??
          Dio(
            BaseOptions(
              baseUrl: AppRuntimeEndpoints.apiBaseUrl,
              connectTimeout: const Duration(seconds: 10),
              receiveTimeout: const Duration(seconds: 10),
            ),
          );

  final Dio _dio;

  /// 鉴权拦截器必须挂在 onInit 而不是 init()：本服务的两个调用方（充值面板、
  /// Agent 工具栏）都是用 Get.put(GatewayService()) 懒注册的，从不走 init()，
  /// 挂在 init() 里等于没挂——请求不带 Authorization 头，网关接口一律 401
  /// （充值下单因此永远失败，前端只会兜底提示"请联系管理员"）。
  /// onInit 由 GetX 在 put/putAsync 两种注册方式下都会调用，是唯一可靠的挂载点。
  @override
  void onInit() {
    super.onInit();
    Get.find<AuthService>().attachAuthInterceptor(_dio);
  }

  /// 供 Get.putAsync 使用；拦截器已在 onInit 挂好，此处不重复挂
  /// （attachAuthInterceptor 会 add 一个新拦截器，不幂等）。
  Future<GatewayService> init() async => this;

  /// 给指定托管Agent接入"Grix中转"：自动开一把该Agent专属虚拟Key + 下发配置给
  /// 该Agent所在的grix-connector自动完成路由/原生配置，不用调用方填任何网址和Key。
  /// 幂等——已经配过的Agent再调一次不会重复发Key，返回值本身不区分，调用方不用关心。
  ///
  /// resend=true：仅用于"连接器已经明确说本地没有这把Key"之后的补发重试
  /// （下发Key是一次性推送，不保证送达——上次那把Key很可能推丢了）。
  /// 这种场景下即使服务端库里已经标记发过Key，也要作废重发一把、重新推一次，
  /// 否则会卡死在"服务端说发了、连接器却永远收不到"的死循环里。
  Future<bool> configureAgentProvider(String agentId, {bool resend = false}) async {
    try {
      final resp = await _dio.post(
        '/gateway/agents/$agentId/provider',
        queryParameters: resend ? {'resend': 'true'} : null,
      );
      return resp.statusCode == 200 && resp.data['code'] == 0;
    } catch (_) {
      return false;
    }
  }

  /// 最近一次 [issueAgentRelayCredential] 失败时服务端给的拒绝原因（如"所选模型当前
  /// 不可用"）；成功时清空。只用于给用户看提示，别参与任何逻辑判断。
  String? lastRelayCredentialError;

  /// 直接给指定托管Agent签发一把一次性明文中转凭证（桌面端直连本地Connector改造的新入口）。
  /// 跟 [configureAgentProvider] 不同：这里不经服务端向 connector 广播下发，明文Key和两个
  /// 协议地址直接在这次HTTP响应里带回来，调用方（桌面端）必须自己转手交给本地 Connector 的
  /// 凭证应用接口，用完立刻丢弃——不能保存在内存以外的任何地方，也不能打日志。
  /// 每次调用都会作废该Agent之前的旧Key、发一把全新的，调用方不需要也不应该缓存复用。
  Future<GatewayRelayCredentialModel?> issueAgentRelayCredential(
    String agentId, {
    String? model,
  }) async {
    lastRelayCredentialError = null;
    try {
      final resp = await _dio.post(
        '/gateway/agents/$agentId/relay-credential',
        data: {
          // 原生配置类型必填（服务端会强校验）；MITM 类型不传，沿用网关映射兜底。
          if (model != null && model.isNotEmpty) 'model': model,
        },
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final data = resp.data['data'];
        if (data is Map) {
          return GatewayRelayCredentialModel.fromJson(data);
        }
      }
      final msg = resp.data is Map ? resp.data['msg']?.toString() : null;
      if (msg != null && msg.isNotEmpty) lastRelayCredentialError = msg;
    } on DioException catch (e) {
      final data = e.response?.data;
      final msg = data is Map ? data['msg']?.toString() : null;
      if (msg != null && msg.isNotEmpty) lastRelayCredentialError = msg;
    } catch (_) {}
    return null;
  }

  /// 后端当前支持的模型清单（含单价）。清单来自塘主后台录入的价目表——
  /// 价目表为空时这里返回空清单，中转即不可用。
  Future<List<GatewayModelModel>> listModels() async {
    try {
      final resp = await _dio.get('/gateway/models');
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final items = resp.data['data']?['items'];
        if (items is List) {
          return items
              .whereType<Map>()
              .map(
                (e) => GatewayModelModel.fromJson(
                  e.map((k, v) => MapEntry(k.toString(), v)),
                ),
              )
              .toList();
        }
      }
    } catch (_) {}
    return [];
  }

  /// 取我的兜底模型 + 模型映射表。
  Future<GatewayRelaySettingsModel?> getRelaySettings() async {
    try {
      final resp = await _dio.get('/gateway/relay-settings');
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final data = resp.data['data'];
        if (data is Map) {
          return GatewayRelaySettingsModel.fromJson(
            data.map((k, v) => MapEntry(k.toString(), v)),
          );
        }
      }
    } catch (_) {}
    return null;
  }

  /// 保存兜底模型 + 模型映射表。保存即生效：网关每次请求都按这份设置解析模型，
  /// 不需要重启 Agent，也不需要给 connector 下发任何东西。
  ///
  /// 后端会校验兜底模型与所有映射目标都是当前支持的模型；不合法返回 false。
  Future<bool> putRelaySettings({
    required String defaultModel,
    required Map<String, String> modelMap,
  }) async {
    try {
      final resp = await _dio.put(
        '/gateway/relay-settings',
        data: {'default_model': defaultModel, 'model_map': modelMap},
      );
      return resp.statusCode == 200 && resp.data['code'] == 0;
    } catch (_) {
      return false;
    }
  }

  /// 我名下托管Agent的中转接入状态（能不能接、接没接）。
  /// 服务端 relay_state_enabled 开启时每项还带 enabled(desired)/applied(actual)/
  /// applied_at/state_known 扩展字段；flag 关闭时这些字段为 null，按旧语义展示。
  Future<List<GatewayAgentRelayStateModel>> listAgents() async {
    try {
      final resp = await _dio.get('/gateway/agents');
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final items = resp.data['data']?['items'];
        if (items is List) {
          return items
              .whereType<Map>()
              .map(
                (e) => GatewayAgentRelayStateModel.fromJson(
                  e.map((k, v) => MapEntry(k.toString(), v)),
                ),
              )
              .toList();
        }
      }
    } catch (_) {}
    return [];
  }

  /// 设置某个托管Agent的中转开关（服务端 desired 期望态，设计 §2.3）。
  /// 成功/409 都带回最新 state（含 revision）；调用方据此做乐观更新、冲突刷新重试。
  /// [expectedRevision] 传上次拿到的 revision 走乐观锁；不传则 last-write-wins。
  Future<GatewaySetAgentRelayResult> setAgentRelay(
    String agentId, {
    required bool enabled,
    String? model,
    int? expectedRevision,
  }) async {
    try {
      final resp = await _dio.post(
        '/gateway/agents/$agentId/relay',
        data: {
          'enabled': enabled,
          if (model != null && model.isNotEmpty) 'model': model,
          if (expectedRevision != null) 'expected_revision': expectedRevision,
        },
        // 4xx/5xx 都要读响应体（409 带最新 state、503 是 flag 关闭），不能让 dio 抛掉。
        options: Options(validateStatus: (s) => s != null),
      );
      final body = resp.data;
      final rawData = body is Map ? body['data'] : null;
      final state = rawData is Map
          ? GatewayRelayWriteStateModel.fromJson(
              rawData.map((k, v) => MapEntry(k.toString(), v)),
            )
          : null;
      if (resp.statusCode == 200 && body is Map && body['code'] == 0) {
        return GatewaySetAgentRelayResult(GatewaySetRelayStatus.ok, state);
      }
      if (resp.statusCode == 409) {
        return GatewaySetAgentRelayResult(GatewaySetRelayStatus.conflict, state);
      }
      if (resp.statusCode == 503) {
        return GatewaySetAgentRelayResult(GatewaySetRelayStatus.disabled, state);
      }
      if (resp.statusCode == 400 && body is Map && body['code'] == 26006) {
        return GatewaySetAgentRelayResult(GatewaySetRelayStatus.needModel, state);
      }
      return GatewaySetAgentRelayResult(GatewaySetRelayStatus.failed, state);
    } catch (_) {
      return const GatewaySetAgentRelayResult(GatewaySetRelayStatus.failed);
    }
  }

  Future<GatewayWalletModel?> getWallet() async {
    try {
      final resp = await _dio.get('/gateway/wallet');
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final wallet = resp.data['data']?['wallet'];
        if (wallet is Map) {
          return GatewayWalletModel.fromJson(
            wallet.map((k, v) => MapEntry(k.toString(), v)),
          );
        }
      }
    } catch (_) {}
    return null;
  }

  Future<List<GatewayLedgerEntryModel>> listLedger({
    int page = 1,
    int pageSize = 20,
  }) async {
    try {
      final resp = await _dio.get(
        '/gateway/wallet/ledger',
        queryParameters: {'page': page, 'page_size': pageSize},
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final items = (resp.data['data']?['items'] as List?) ?? const [];
        return items
            .whereType<Map>()
            .map(
              (e) => GatewayLedgerEntryModel.fromJson(
                e.map((k, v) => MapEntry(k.toString(), v)),
              ),
            )
            .toList();
      }
    } catch (_) {}
    return const [];
  }

  Future<List<GatewayTopupRecordModel>> listTopups({
    int page = 1,
    int pageSize = 20,
  }) async {
    try {
      final resp = await _dio.get(
        '/gateway/wallet/topups',
        queryParameters: {'page': page, 'page_size': pageSize},
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final items = (resp.data['data']?['items'] as List?) ?? const [];
        return items
            .whereType<Map>()
            .map(
              (e) => GatewayTopupRecordModel.fromJson(
                e.map((k, v) => MapEntry(k.toString(), v)),
              ),
            )
            .toList();
      }
    } catch (_) {}
    return const [];
  }

  /// 自助充值下单：建充值单并返回收银台跳转地址，支付成功后由服务端自动入账。
  /// 失败返回 null（含支付通道未开通、服务端异常等），调用方给统一提示即可。
  Future<GatewayTopupOrderModel?> createTopup({
    required String amount,
    required String currency,
    required String channel,
    String returnUrl = '',
  }) async {
    try {
      final resp = await _dio.post(
        '/gateway/wallet/topup',
        data: {
          'amount': amount,
          'currency': currency,
          'channel': channel,
          if (returnUrl.isNotEmpty) 'return_url': returnUrl,
        },
      );
      if (resp.statusCode == 200 && resp.data['code'] == 0) {
        final data = resp.data['data'];
        if (data is Map) {
          return GatewayTopupOrderModel.fromJson(
            data.map((k, v) => MapEntry(k.toString(), v)),
          );
        }
      }
    } catch (_) {}
    return null;
  }
}
