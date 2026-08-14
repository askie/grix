import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';
import 'package:get/get.dart';
import 'package:url_launcher/url_launcher.dart';

import '../../data/providers/feature_flag_service.dart';
import '../../data/providers/gateway_service.dart';
import '../../shared/utils/toast_util.dart';
import '../../shared/widgets/app_dialog_style.dart';

/// "我的Grix中转"面板：看余额、发起充值、看账单与充值记录。
///
/// 兜底模型/模型映射/Agent 中转开关已统一到移动端「模型设置」页，桌面端
/// 这里只保留资金侧能力：顶部余额卡 + 充值入口，下面是账单、充值记录两个 Tab。
/// 建Key/接入这两步仍在"添加Agent"流程里自动完成，这里不含Key管理。
class GatewayRelayPanelView extends StatefulWidget {
  const GatewayRelayPanelView({super.key, this.isActive = true});

  /// 外层“系统设置”当前是否正显示“大模型设置”页。外层 TabBarView 会提前
  /// 构建并保留本组件，因此需要显式通知真正的页面打开时机。
  final bool isActive;

  @override
  State<GatewayRelayPanelView> createState() => _GatewayRelayPanelViewState();
}

class _GatewayRelayPanelViewState extends State<GatewayRelayPanelView>
    with SingleTickerProviderStateMixin {
  late final TabController _tabController;
  bool _loading = true;
  GatewayWalletModel? _wallet;
  List<GatewayLedgerEntryModel> _ledger = const [];
  List<GatewayTopupRecordModel> _topups = const [];

  GatewayService get _service {
    if (!Get.isRegistered<GatewayService>()) {
      Get.put(GatewayService());
    }
    return Get.find<GatewayService>();
  }

  /// 当前选中的子 tab。内容渲染直接认这个值，不认 TabBarView 自己的翻页位置——
  /// 这个面板是外层系统设置 TabBarView 的第三页，会在应用打开时跟另外两页一起
  /// 提前构建（即使当时还没切到这个 tab），这个时机差会让 TabBarView 内部的
  /// PageView 翻页位置跟 TabController.index 对不上：TabBar 显示选中第一页，
  /// 实际画出来的却是第二页的内容，手动切一次 tab 才会自愈。改用 IndexedStack
  /// 按这个变量直接选内容，彻底不依赖 TabBarView 的翻页状态。
  int _tabIndex = 0;

  @override
  void initState() {
    super.initState();
    _tabController = TabController(length: 2, vsync: this)
      ..addListener(() {
        if (_tabIndex != _tabController.index) {
          final nextIndex = _tabController.index;
          setState(() => _tabIndex = nextIndex);
          _refreshTab(nextIndex);
        }
      });
    _load();
  }

  @override
  void didUpdateWidget(covariant GatewayRelayPanelView oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!oldWidget.isActive && widget.isActive) {
      _refreshWallet();
      if (_tabIndex == 0) _refreshLedger();
      if (_tabIndex == 1) _refreshTopups();
    }
  }

  @override
  void dispose() {
    _tabController.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() => _loading = true);
    final results = await Future.wait([
      _service.getWallet(),
      _service.listLedger(),
      _service.listTopups(),
    ]);
    if (!mounted) return;
    setState(() {
      _wallet = results[0] as GatewayWalletModel?;
      _ledger = results[1] as List<GatewayLedgerEntryModel>;
      _topups = results[2] as List<GatewayTopupRecordModel>;
      _loading = false;
    });
  }

  /// 每次打开子页面都重新读取该页数据。这个面板会被外层 TabBarView 提前构建并
  /// 保留状态，不能把 initState 的首次加载当成用户真正打开页面时的数据刷新。
  void _refreshTab(int index) {
    switch (index) {
      case 0:
        _refreshLedger();
        return;
      case 1:
        _refreshTopups();
        return;
    }
  }

  Future<void> _refreshWallet() async {
    final wallet = await _service.getWallet();
    if (mounted) setState(() => _wallet = wallet);
  }

  Future<void> _refreshLedger() async {
    final ledger = await _service.listLedger();
    if (mounted) setState(() => _ledger = ledger);
  }

  Future<void> _refreshTopups() async {
    final topups = await _service.listTopups();
    if (mounted) setState(() => _topups = topups);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return RefreshIndicator(
      onRefresh: _load,
      child: Column(
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
            child: Container(
              width: double.infinity,
              padding: const EdgeInsets.all(16),
              decoration: BoxDecoration(
                color: theme.colorScheme.primaryContainer,
                borderRadius: BorderRadius.circular(12),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(
                              'gateway_relay_balance_title'.tr,
                              style: TextStyle(
                                fontSize: 13,
                                color: theme.colorScheme.onPrimaryContainer
                                    .withValues(alpha: 0.7),
                              ),
                            ),
                            const SizedBox(height: 4),
                            _loading
                                ? const SizedBox(
                                    height: 28,
                                    width: 28,
                                    child: CircularProgressIndicator(
                                      strokeWidth: 2,
                                    ),
                                  )
                                : Text(
                                    '\$${_wallet?.balance ?? '0'}',
                                    style: TextStyle(
                                      fontSize: 26,
                                      fontWeight: FontWeight.w700,
                                      color:
                                          theme.colorScheme.onPrimaryContainer,
                                    ),
                                  ),
                          ],
                        ),
                      ),
                      FilledButton.icon(
                        onPressed: _showTopupDialog,
                        icon: const Icon(Icons.add_card, size: 18),
                        label: Text('gateway_relay_topup'.tr),
                      ),
                    ],
                  ),
                  const SizedBox(height: 4),
                  Text(
                    'gateway_relay_balance_hint'.tr,
                    style: TextStyle(
                      fontSize: 11,
                      color: theme.colorScheme.onPrimaryContainer.withValues(
                        alpha: 0.6,
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
          TabBar(
            controller: _tabController,
            tabs: [
              Tab(text: 'gateway_relay_tab_ledger'.tr),
              Tab(text: 'gateway_relay_tab_topups'.tr),
            ],
          ),
          Expanded(
            child: IndexedStack(
              index: _tabIndex,
              children: [_buildLedgerList(theme), _buildTopupList(theme)],
            ),
          ),
        ],
      ),
    );
  }

  Future<void> _showTopupDialog() async {
    final order = await showAppDialog<GatewayTopupOrderModel>(
      context: context,
      barrierDismissible: false,
      builder: (_) => _TopupDialog(service: _service),
    );
    if (order != null) {
      await _openPayUrl(order.payUrl);
      // 支付跳出去（外部浏览器/收银台）再回来，余额与充值记录应是最新的。
      await Future.wait([_refreshWallet(), _refreshTopups()]);
    }
  }

  Future<void> _openPayUrl(String url) async {
    final uri = Uri.tryParse(url);
    if (uri == null || !(uri.isScheme('https') || uri.isScheme('http'))) {
      CustomToast.show('gateway_relay_pay_url_invalid'.tr);
      return;
    }
    var ok = false;
    try {
      if (kIsWeb) {
        // Web端到这里已脱离用户点击手势（中间隔了一次下单请求），开新窗口会被
        // 浏览器弹窗拦截且 launchUrl 拿不到真实结果；改当前标签跳收银台，
        // 支付完成后由 return_url（下单时传的当前页面地址）跳回。
        ok = await launchUrl(uri, webOnlyWindowName: '_self');
      } else {
        ok = await launchUrl(uri, mode: LaunchMode.externalApplication);
      }
    } catch (_) {}
    if (!ok) {
      CustomToast.show('gateway_relay_pay_open_failed'.tr);
      return;
    }
    if (!kIsWeb) {
      CustomToast.show('gateway_relay_pay_opened'.tr, isError: false);
    }
  }

  Widget _buildLedgerList(ThemeData theme) {
    if (_ledger.isEmpty) {
      return _buildEmpty('gateway_relay_empty_ledger'.tr);
    }
    return ListView.separated(
      padding: const EdgeInsets.symmetric(vertical: 8),
      itemCount: _ledger.length,
      separatorBuilder: (_, _) => const Divider(height: 1),
      itemBuilder: (context, index) {
        final entry = _ledger[index];
        return ListTile(
          dense: true,
          title: Text('${entry.provider} · ${entry.model}'),
          subtitle: Text(entry.createdAt),
          trailing: Text(
            '-\$${entry.cost}',
            style: TextStyle(
              fontWeight: FontWeight.w600,
              color: entry.status == 'failed'
                  ? theme.colorScheme.error
                  : theme.colorScheme.onSurface,
            ),
          ),
        );
      },
    );
  }

  Widget _buildTopupList(ThemeData theme) {
    if (_topups.isEmpty) {
      return _buildEmpty('gateway_relay_empty_topups'.tr);
    }
    return ListView.separated(
      padding: const EdgeInsets.symmetric(vertical: 8),
      itemCount: _topups.length,
      separatorBuilder: (_, _) => const Divider(height: 1),
      itemBuilder: (context, index) {
        final record = _topups[index];
        return ListTile(
          dense: true,
          title: Text(
            record.paymentChannel.isEmpty
                ? 'gateway_relay_admin_topup'.tr
                : record.paymentChannel,
          ),
          subtitle: Text(record.createdAt),
          trailing: Text(
            '+\$${record.creditedAmount}',
            style: const TextStyle(
              fontWeight: FontWeight.w600,
              color: Colors.green,
            ),
          ),
        );
      },
    );
  }

  Widget _buildEmpty(String text) {
    return LayoutBuilder(
      builder: (context, constraints) => SingleChildScrollView(
        physics: const AlwaysScrollableScrollPhysics(),
        child: SizedBox(
          height: constraints.maxHeight,
          child: Center(
            child: Text(
              text,
              style: TextStyle(color: Theme.of(context).colorScheme.outline),
            ),
          ),
        ),
      ),
    );
  }
}

/// 充值弹窗：选通道、填金额、下单。下单成功以订单为结果 pop，
/// 由调用方负责打开收银台；失败留在弹窗内提示重试。
class _TopupDialog extends StatefulWidget {
  const _TopupDialog({required this.service});

  final GatewayService service;

  @override
  State<_TopupDialog> createState() => _TopupDialogState();
}

class _TopupDialogState extends State<_TopupDialog> {
  static const _paypalFeatureKey = 'gateway_topup_paypal';

  /// 自助充值可选通道，须与支付系统已接入的适配器一致：
  /// alipay 只收 CNY，paypal 以 USD 计价。(code, 显示名, 币种, 币符)
  static const _alipayChannel = _TopupChannel(
    code: 'alipay',
    labelKey: 'gateway_relay_channel_alipay',
    currency: 'CNY',
    symbol: '¥',
  );
  static const _paypalChannel = _TopupChannel(
    code: 'paypal',
    labelKey: 'gateway_relay_channel_paypal',
    currency: 'USD',
    symbol: '\$',
  );

  static final _amountPattern = RegExp(r'^\d+(\.\d{1,2})?$');

  /// 单笔上限（源币种），只是前端护栏，真正的限额以支付通道为准。
  static const _maxAmount = 100000;

  final _amountController = TextEditingController();
  var _channelIndex = 0;
  var _submitting = false;

  @override
  void dispose() {
    _amountController.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    final channels = _availableChannels;
    final channel = channels[_selectedChannelIndex(channels)];
    final amount = _amountController.text.trim();
    // 正则已排除负号/非数字，parse 必成功；<= 0 只为挡住 0 / 0.00。
    if (!_amountPattern.hasMatch(amount) || double.parse(amount) <= 0) {
      CustomToast.show('gateway_relay_invalid_amount'.tr);
      return;
    }
    if (double.parse(amount) > _maxAmount) {
      CustomToast.show(
        'gateway_relay_amount_too_large'.trParams({
          'max': '$_maxAmount',
          'currency': channel.currency,
        }),
      );
      return;
    }
    setState(() => _submitting = true);
    final order = await widget.service.createTopup(
      amount: amount,
      currency: channel.currency,
      channel: channel.code,
      returnUrl: kIsWeb ? Uri.base.toString() : '',
    );
    if (!mounted) return;
    if (order == null || order.payUrl.isEmpty) {
      setState(() => _submitting = false);
      CustomToast.show('gateway_relay_topup_failed'.tr);
      return;
    }
    Navigator.of(context).pop(order);
  }

  @override
  Widget build(BuildContext context) {
    final flags = Get.isRegistered<FeatureFlagService>()
        ? Get.find<FeatureFlagService>()
        : null;
    if (flags == null) {
      return _buildDialog(context, _availableChannels);
    }
    return Obx(() => _buildDialog(context, _availableChannels));
  }

  Widget _buildDialog(BuildContext context, List<_TopupChannel> channels) {
    final selectedIndex = _selectedChannelIndex(channels);
    final channel = channels[selectedIndex];
    return AlertDialog(
      title: Text('gateway_relay_topup'.tr),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SegmentedButton<int>(
            segments: [
              for (var i = 0; i < channels.length; i++)
                ButtonSegment(value: i, label: Text(channels[i].labelKey.tr)),
            ],
            selected: {selectedIndex},
            onSelectionChanged: _submitting
                ? null
                : (selection) =>
                      setState(() => _channelIndex = selection.first),
          ),
          const SizedBox(height: 16),
          TextField(
            controller: _amountController,
            enabled: !_submitting,
            autofocus: true,
            keyboardType: const TextInputType.numberWithOptions(decimal: true),
            decoration: InputDecoration(
              labelText: 'gateway_relay_amount_label'.trParams({
                'currency': channel.currency,
              }),
              prefixText: channel.symbol,
              border: const OutlineInputBorder(),
            ),
            onSubmitted: (_) => _submit(),
          ),
          const SizedBox(height: 8),
          Text(
            channel.currency == 'USD'
                ? 'gateway_relay_amount_usd_hint'.tr
                : 'gateway_relay_amount_fx_hint'.trParams({
                    'currency': channel.currency,
                  }),
            style: TextStyle(
              fontSize: 11,
              color: Theme.of(context).colorScheme.outline,
            ),
          ),
        ],
      ),
      actions: [
        TextButton(
          onPressed: _submitting ? null : () => Navigator.of(context).pop(),
          child: Text('common_cancel'.tr),
        ),
        FilledButton(
          onPressed: _submitting ? null : _submit,
          child: _submitting
              ? const SizedBox(
                  width: 18,
                  height: 18,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : Text('gateway_relay_go_pay'.tr),
        ),
      ],
    );
  }

  List<_TopupChannel> get _availableChannels {
    final channels = <_TopupChannel>[_alipayChannel];
    final paypalEnabled =
        Get.isRegistered<FeatureFlagService>() &&
        Get.find<FeatureFlagService>().isEnabled(_paypalFeatureKey);
    if (paypalEnabled) {
      channels.add(_paypalChannel);
    }
    return channels;
  }

  int _selectedChannelIndex(List<_TopupChannel> channels) {
    if (_channelIndex >= 0 && _channelIndex < channels.length) {
      return _channelIndex;
    }
    return 0;
  }
}

class _TopupChannel {
  const _TopupChannel({
    required this.code,
    required this.labelKey,
    required this.currency,
    required this.symbol,
  });

  final String code;
  final String labelKey;
  final String currency;
  final String symbol;
}
