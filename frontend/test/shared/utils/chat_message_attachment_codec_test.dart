import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/models/chat_message_attachment.dart';
import 'package:grix/shared/utils/chat_message_attachment_codec.dart';

void main() {
  test('builds attachments extra envelope', () {
    const attachment = ChatMessageAttachment(
      url: 'https://cdn.example.com/demo.png',
      type: 'image',
      fileName: 'demo.png',
      contentType: 'image/png',
    );

    final extra = ChatMessageAttachmentCodec.buildExtra(<ChatMessageAttachment>[
      attachment,
    ]);

    expect(extra, <String, dynamic>{
      'attachments': <Map<String, dynamic>>[
        <String, dynamic>{
          'media_url': 'https://cdn.example.com/demo.png',
          'attachment_type': 'image',
          'file_name': 'demo.png',
          'content_type': 'image/png',
        },
      ],
    });
  });

  test('reads attachments only from attachments array', () {
    final attachments = ChatMessageAttachmentCodec.readFromExtra(
      <String, dynamic>{
        'attachments': <Map<String, dynamic>>[
          <String, dynamic>{
            'media_url': 'https://cdn.example.com/demo.png',
            'attachment_type': 'image',
            'file_name': 'demo.png',
            'content_type': 'image/png',
          },
        ],
        'media_url': 'https://cdn.example.com/legacy.png',
        'attachment_type': 'image',
      },
    );

    expect(attachments, hasLength(1));
    expect(attachments.first.url, 'https://cdn.example.com/demo.png');
    expect(attachments.first.fileName, 'demo.png');
  });

  test('strips generated attachment markdown content', () {
    final attachments = <ChatMessageAttachment>[
      const ChatMessageAttachment(
        url: 'https://cdn.example.com/demo.png',
        type: 'image',
        fileName: 'demo.png',
        contentType: 'image/png',
      ),
    ];

    final content = ChatMessageAttachmentCodec.buildContent(attachments);

    expect(
      ChatMessageAttachmentCodec.stripGeneratedAttachmentContent(
        content,
        attachments,
      ),
      isEmpty,
    );
  });

  test('strips generated attachment lines but keeps user-entered text', () {
    final attachments = <ChatMessageAttachment>[
      const ChatMessageAttachment(
        url: 'https://cdn.example.com/demo.png',
        type: 'image',
        fileName: 'demo.png',
        contentType: 'image/png',
      ),
      const ChatMessageAttachment(
        url: 'https://cdn.example.com/spec.pdf',
        type: 'file',
        fileName: 'spec.pdf',
        contentType: 'application/pdf',
      ),
    ];

    final content = [
      '请看附件',
      ChatMessageAttachmentCodec.buildContent(attachments),
      '收到后回我',
    ].join('\n');

    expect(
      ChatMessageAttachmentCodec.stripGeneratedAttachmentContent(
        content,
        attachments,
      ),
      '请看附件\n收到后回我',
    );
  });
}
