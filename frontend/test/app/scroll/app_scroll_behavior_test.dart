import 'package:flutter/gestures.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/scroll/app_scroll_behavior.dart';

void main() {
  test('app scroll behavior enables mouse and trackpad dragging', () {
    const behavior = AppScrollBehavior();

    expect(behavior.dragDevices, contains(PointerDeviceKind.mouse));
    expect(behavior.dragDevices, contains(PointerDeviceKind.trackpad));
    expect(behavior.dragDevices, contains(PointerDeviceKind.touch));
  });
}
