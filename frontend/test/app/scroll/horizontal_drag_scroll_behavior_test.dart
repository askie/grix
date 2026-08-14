import 'package:flutter/gestures.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/app/scroll/horizontal_drag_scroll_behavior.dart';

void main() {
  test('horizontal drag scroll behavior enables mouse and trackpad', () {
    const behavior = HorizontalDragScrollBehavior();

    expect(behavior.dragDevices, contains(PointerDeviceKind.mouse));
    expect(behavior.dragDevices, contains(PointerDeviceKind.trackpad));
    expect(behavior.dragDevices, contains(PointerDeviceKind.touch));
  });
}
