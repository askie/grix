class ChatAttachmentLimitPolicy {
  const ChatAttachmentLimitPolicy._();

  static const int maxImageBytes = 5 * 1024 * 1024;
  static const int maxVideoBytes = 50 * 1024 * 1024;

  static bool shouldCompressImage(int byteLength) {
    return byteLength > maxImageBytes;
  }

  static bool isImageWithinLimit(int byteLength) {
    return byteLength > 0 && byteLength <= maxImageBytes;
  }

  static bool isVideoWithinLimit(int byteLength) {
    return byteLength > 0 && byteLength <= maxVideoBytes;
  }
}
