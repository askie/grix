import 'package:flutter/material.dart';

import '../utils/app_version_info.dart';

class AppVersionText extends StatelessWidget {
  const AppVersionText({
    super.key,
    this.style,
    this.placeholder = '--',
  });

  final TextStyle? style;
  final String placeholder;

  static final Future<String> _displayVersionFuture =
      AppVersionInfo.loadDisplayVersion();

  @override
  Widget build(BuildContext context) {
    return FutureBuilder<String>(
      future: _displayVersionFuture,
      builder: (context, snapshot) {
        final displayVersion = snapshot.data ?? placeholder;
        return Text(
          displayVersion,
          style: style,
        );
      },
    );
  }
}
