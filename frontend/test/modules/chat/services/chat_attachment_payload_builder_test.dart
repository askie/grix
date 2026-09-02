import 'package:file_picker/file_picker.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/modules/chat/models/chat_attachment_type.dart';
import 'package:grix/modules/chat/services/chat_attachment_payload_builder.dart';

void main() {
  group('filePickerConfig（按平台选文件筛选方式）', () {
    test('Web 端用 FileType.any 且不下发后缀清单（避免静默吞掉 txt）', () {
      final cfg = ChatAttachmentPayloadBuilder.filePickerConfig(isWeb: true);
      expect(cfg.type, FileType.any);
      expect(cfg.allowedExtensions, isNull);
    });

    test('原生端用 FileType.custom 且带完整可上传后缀清单', () {
      final cfg = ChatAttachmentPayloadBuilder.filePickerConfig(isWeb: false);
      expect(cfg.type, FileType.custom);
      expect(
        cfg.allowedExtensions,
        ChatAttachmentPayloadBuilder.uploadableFileExtensions,
      );
      // txt 必须在原生筛选清单里，否则系统弹窗会灰掉 txt。
      expect(cfg.allowedExtensions, contains('txt'));
    });
  });

  group('txt 文件的可上传判定与 contentType', () {
    test('txt 被判定为支持上传', () {
      expect(ChatAttachmentPayloadBuilder.isSupportedFile('note.txt'), isTrue);
      // 大小写 / 带路径也应识别。
      expect(ChatAttachmentPayloadBuilder.isSupportedFile('A.TXT'), isTrue);
    });

    test('txt 的 contentType 解析为 text/plain', () {
      expect(
        ChatAttachmentPayloadBuilder.resolveContentType(
          'note.txt',
          type: ChatAttachmentType.file,
        ),
        'text/plain',
      );
    });

    test('真正不支持的类型才返回 false（用于给用户明确提示）', () {
      expect(
        ChatAttachmentPayloadBuilder.isSupportedFile('virus.exe'),
        isFalse,
      );
      expect(ChatAttachmentPayloadBuilder.isSupportedFile('noext'), isFalse);
    });
  });
}
