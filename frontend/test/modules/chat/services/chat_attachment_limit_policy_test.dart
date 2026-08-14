import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/services/chat_attachment_limit_policy.dart';

void main() {
  test('image limit policy uses 5MB threshold', () {
    expect(
      ChatAttachmentLimitPolicy.isImageWithinLimit(
        ChatAttachmentLimitPolicy.maxImageBytes,
      ),
      isTrue,
    );
    expect(
      ChatAttachmentLimitPolicy.shouldCompressImage(
        ChatAttachmentLimitPolicy.maxImageBytes,
      ),
      isFalse,
    );
    expect(
      ChatAttachmentLimitPolicy.shouldCompressImage(
        ChatAttachmentLimitPolicy.maxImageBytes + 1,
      ),
      isTrue,
    );
  });

  test('video limit policy uses 50MB threshold', () {
    expect(
      ChatAttachmentLimitPolicy.isVideoWithinLimit(
        ChatAttachmentLimitPolicy.maxVideoBytes,
      ),
      isTrue,
    );
    expect(
      ChatAttachmentLimitPolicy.isVideoWithinLimit(
        ChatAttachmentLimitPolicy.maxVideoBytes + 1,
      ),
      isFalse,
    );
  });
}
