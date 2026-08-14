import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../data/providers/session_service.dart';
import '../../shared/utils/toast_util.dart';
import '../../shared/widgets/app_dialog_style.dart';
import 'widgets/widget_site_embed_sheet.dart';
import 'widgets/widget_site_form_dialog.dart';

class WidgetSitesView extends StatefulWidget {
  const WidgetSitesView({super.key});

  @override
  State<WidgetSitesView> createState() => _WidgetSitesViewState();
}

class _WidgetSitesViewState extends State<WidgetSitesView> {
  final SessionService _sessionService = Get.find<SessionService>();
  bool _loading = false;
  final List<WidgetSiteModel> _sites = <WidgetSiteModel>[];

  @override
  void initState() {
    super.initState();
    _loadSites();
  }

  Future<void> _loadSites() async {
    setState(() => _loading = true);
    final result = await _sessionService.fetchWidgetSites(limit: 100);
    if (!mounted) return;
    setState(() {
      _loading = false;
      _sites
        ..clear()
        ..addAll(result.items);
    });
    if (!result.success && result.message.isNotEmpty) {
      CustomToast.show(result.message);
    }
  }

  Future<void> _showCreateDialog() async {
    final form = await showAppDialog<WidgetSiteFormResult>(
      context: context,
      builder: (_) => WidgetSiteFormDialog(
        confirmLabel: 'settings_widget_sites_create'.tr,
      ),
    );
    if (form == null) return;

    final result = await _sessionService.createWidgetSite(
      siteName: form.siteName,
      allowedOrigins: form.allowedOrigins,
      displayConfig: form.displayConfig,
    );
    if (!result.success) {
      CustomToast.show(result.message.isNotEmpty
          ? result.message
          : 'settings_widget_sites_create_failed'.tr);
      return;
    }
    CustomToast.show('settings_widget_sites_create_success'.tr, isError: false);
    await _loadSites();
    final site = result.site;
    if (site != null) {
      await _showSiteDetail(site.id);
    }
  }

  Future<void> _showSiteDetail(String siteId) async {
    final detail = await _sessionService.fetchWidgetSiteDetail(siteId);
    if (!detail.success) {
      CustomToast.show(detail.message.isNotEmpty
          ? detail.message
          : 'settings_widget_sites_detail_failed'.tr);
      return;
    }
    final site = detail.site!;
    if (!mounted) return;
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      builder: (ctx) => WidgetSiteEmbedSheet(
        site: site,
        baseEmbedCode: detail.embedCode,
        onEdit: () => _showEditDialog(site),
        onDelete: () => _deleteSite(site),
      ),
    );
  }

  Future<void> _showEditDialog(WidgetSiteModel site) async {
    final form = await showAppDialog<WidgetSiteFormResult>(
      context: context,
      builder: (_) => WidgetSiteFormDialog(
        initial: site,
        confirmLabel: 'settings_widget_sites_edit'.tr,
      ),
    );
    if (form == null) return;

    final result = await _sessionService.updateWidgetSite(
      id: site.id,
      siteName: form.siteName,
      allowedOrigins: form.allowedOrigins,
      status: site.status,
      displayConfig: form.displayConfig,
    );
    if (!result.success) {
      CustomToast.show(result.message.isNotEmpty
          ? result.message
          : 'settings_widget_sites_update_failed'.tr);
      return;
    }
    CustomToast.show('settings_widget_sites_create_success'.tr, isError: false);
    await _loadSites();
  }

  Future<void> _deleteSite(WidgetSiteModel site) async {
    final confirmed = await showAppConfirmDialog(
      context: context,
      title: 'settings_widget_sites_delete_title'.tr,
      message: 'settings_widget_sites_delete_message'.tr,
      confirmText: 'settings_widget_sites_delete'.tr,
      isDestructive: true,
    );
    if (!confirmed) return;

    final result = await _sessionService.deleteWidgetSite(site.id);
    if (!result.success) {
      CustomToast.show(result.message.isNotEmpty
          ? result.message
          : 'settings_widget_sites_delete_failed'.tr);
      return;
    }
    CustomToast.show('settings_widget_sites_deleted'.tr, isError: false);
    await _loadSites();
  }

  Future<void> _toggleSiteStatus(WidgetSiteModel site) async {
    final result = await _sessionService.updateWidgetSite(
      id: site.id,
      siteName: site.siteName,
      allowedOrigins: site.allowedOrigins,
      status: site.isActive ? 2 : 1,
      displayConfig: site.displayConfig,
    );
    if (!result.success) {
      CustomToast.show(result.message.isNotEmpty
          ? result.message
          : 'settings_widget_sites_update_failed'.tr);
      return;
    }
    CustomToast.show(
      site.isActive
          ? 'settings_widget_sites_disabled'.tr
          : 'settings_widget_sites_enabled'.tr,
      isError: false,
    );
    await _loadSites();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text('settings_widget_sites'.tr),
        actions: [
          IconButton(
            onPressed: _showCreateDialog,
            icon: const Icon(Icons.add_rounded),
            tooltip: 'settings_widget_sites_create_tooltip'.tr,
          ),
        ],
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : RefreshIndicator(
              onRefresh: _loadSites,
              child: ListView.builder(
                itemCount: _sites.length,
                itemBuilder: (context, index) {
                  final site = _sites[index];
                  return ListTile(
                    title: Text(site.siteName),
                    subtitle: Text(
                      '${site.allowedOrigins.join(", ")}\nkey: ${site.siteKey}',
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                    ),
                    isThreeLine: true,
                    trailing: Switch(
                      value: site.isActive,
                      onChanged: (_) => _toggleSiteStatus(site),
                    ),
                    onTap: () => _showSiteDetail(site.id),
                  );
                },
              ),
            ),
    );
  }
}
