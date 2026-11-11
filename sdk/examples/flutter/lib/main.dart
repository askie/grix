import 'package:flutter/material.dart';
import 'package:grix_widget/grix_widget.dart';

const String _defaultSiteKey = String.fromEnvironment(
  'GRIX_SITE_KEY',
  defaultValue: 'wk_test_sdk_site',
);
const String _defaultApiBase = String.fromEnvironment(
  'GRIX_API_BASE',
  defaultValue: 'http://127.0.0.1:27180',
);
const bool _autoPresent = bool.fromEnvironment('GRIX_AUTO_PRESENT');

void main() {
  runApp(const GrixWidgetExampleApp());
}

class GrixWidgetExampleApp extends StatelessWidget {
  const GrixWidgetExampleApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Grix Widget Flutter Demo',
      navigatorObservers: [GrixWidget.routeObserver],
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(seedColor: const Color(0xFF0F766E)),
        useMaterial3: true,
      ),
      home: const DemoHomePage(),
    );
  }
}

class DemoHomePage extends StatefulWidget {
  const DemoHomePage({super.key});

  @override
  State<DemoHomePage> createState() => _DemoHomePageState();
}

class _DemoHomePageState extends State<DemoHomePage> {
  final TextEditingController _siteKeyController = TextEditingController(
    text: _defaultSiteKey,
  );
  final TextEditingController _apiBaseController = TextEditingController(
    text: _defaultApiBase,
  );
  final TextEditingController _appIdController = TextEditingController();
  final GrixWidgetController _embeddedController = GrixWidgetController();

  bool _showEmbedded = false;
  bool _launcherVisible = false;
  String _lastEvent = 'No events yet';

  @override
  void initState() {
    super.initState();
    _configure();
    GrixWidget.onClose = () {
      _recordEvent('onClose');
    };
    GrixWidget.onUnreadChange = (count) {
      _recordEvent('onUnreadChange($count)');
    };
    GrixWidget.onSessionClosed = (reason) {
      _recordEvent('onSessionClosed($reason)');
    };
    GrixWidget.onError = (code, message) {
      _recordEvent('onError($code): $message');
    };
    if (_autoPresent) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _present();
      });
    }
  }

  void _configure() {
    final appIdOverride = _appIdController.text.trim();
    GrixWidget.configure(
      siteKey: _siteKeyController.text.trim(),
      apiBase: _apiBaseController.text.trim().isEmpty
          ? 'https://gb.grix.im'
          : _apiBaseController.text.trim(),
      locale: 'zh-CN',
      themeColor: '#0f766e',
      title: 'Grix Flutter Support',
      user: const GrixUser(
        name: 'Flutter Visitor',
        email: 'flutter@example.com',
      ),
      appIdOverride: appIdOverride.isEmpty ? null : appIdOverride,
    );
  }

  void _recordEvent(String event) {
    if (mounted == false) return;
    setState(() => _lastEvent = event);
  }

  Future<void> _present() async {
    _configure();
    await GrixWidget.present(context);
  }

  @override
  void dispose() {
    GrixWidget.setLauncherVisible(context, false);
    _siteKeyController.dispose();
    _apiBaseController.dispose();
    _appIdController.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Grix Widget Flutter Demo'),
        actions: [
          IconButton(
            tooltip: 'Show launcher',
            onPressed: () {
              _configure();
              final next = _launcherVisible == false;
              setState(() => _launcherVisible = next);
              GrixWidget.setLauncherVisible(context, next);
            },
            icon: Icon(
              _launcherVisible
                  ? Icons.chat_bubble
                  : Icons.chat_bubble_outline,
            ),
          ),
        ],
      ),
      body: SafeArea(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              TextField(
                controller: _siteKeyController,
                decoration: const InputDecoration(
                  labelText: 'Site Key',
                  border: OutlineInputBorder(),
                  isDense: true,
                ),
              ),
              const SizedBox(height: 8),
              TextField(
                controller: _apiBaseController,
                decoration: const InputDecoration(
                  labelText: 'API Base URL',
                  border: OutlineInputBorder(),
                  isDense: true,
                ),
              ),
              const SizedBox(height: 8),
              TextField(
                controller: _appIdController,
                decoration: const InputDecoration(
                  labelText: 'App ID override (blank = bundle id / package)',
                  border: OutlineInputBorder(),
                  isDense: true,
                ),
              ),
              const SizedBox(height: 12),
              Wrap(
                spacing: 8,
                runSpacing: 8,
                children: [
                  FilledButton.icon(
                    onPressed: _present,
                    icon: const Icon(Icons.open_in_full),
                    label: const Text('present()'),
                  ),
                  OutlinedButton.icon(
                    onPressed: () {
                      _configure();
                      setState(() => _showEmbedded = true);
                    },
                    icon: const Icon(Icons.view_agenda_outlined),
                    label: const Text('makeView()'),
                  ),
                  OutlinedButton.icon(
                    onPressed: () {
                      _configure();
                      _embeddedController.setVisible(true);
                    },
                    icon: const Icon(Icons.visibility),
                    label: const Text('setVisible(true)'),
                  ),
                ],
              ),
              const SizedBox(height: 10),
              Text(
                'Last event: $_lastEvent',
                style: Theme.of(context).textTheme.bodySmall,
              ),
              const SizedBox(height: 10),
              Expanded(
                child: _showEmbedded
                    ? ClipRRect(
                        borderRadius: BorderRadius.circular(12),
                        child: ColoredBox(
                          color: Colors.white,
                          child: GrixWidget.makeView(
                            controller: _embeddedController,
                          ),
                        ),
                      )
                    : Center(
                        child: Text(
                          'Tap present() for full screen, or makeView() to embed.',
                          textAlign: TextAlign.center,
                          style: Theme.of(context).textTheme.bodyLarge,
                        ),
                      ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
