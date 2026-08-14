import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart';
import 'package:grix/modules/chat/chat_view.dart';
import 'package:grix/shared/models/session_avatar_member.dart';
import 'package:grix/shared/widgets/session_avatar.dart';

void main() {
  testWidgets('forward conversation dialog returns multiline message', (
    tester,
  ) async {
    String? result;

    await tester.pumpWidget(
      GetMaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) => FilledButton(
              onPressed: () async {
                result = await showChatForwardConversationDialog(
                  context: context,
                  sourceTitle: '产品讨论',
                  targetTitle: '研发群',
                  sourceIsGroup: false,
                  sourceAvatarTitle: '产品助手',
                  sourceAvatarUrl: 'https://example.com/agent-9001.png',
                );
              },
              child: const Text('open'),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('open'));
    await tester.pumpAndSettle();

    expect(find.text('产品讨论'), findsOneWidget);
    final avatar = tester.widget<SessionAvatar>(find.byType(SessionAvatar));
    expect(avatar.isGroup, isFalse);
    expect(avatar.avatarTitle, '产品助手');
    expect(avatar.avatarUrl, 'https://example.com/agent-9001.png');
    expect(
      avatar.avatarColor,
      Theme.of(tester.element(find.byType(AlertDialog))).colorScheme.primary,
    );
    expect(avatar.memberFallbackColor, avatar.avatarColor);
    expect(
      find.byKey(const ValueKey('chat_forward_conversation_message_input')),
      findsOneWidget,
    );

    await tester.enterText(
      find.byKey(const ValueKey('chat_forward_conversation_message_input')),
      '先看背景资料。\n然后我们再同步。',
    );
    await tester.tap(
      find.byKey(const ValueKey('chat_forward_conversation_confirm')),
    );
    await tester.pumpAndSettle();

    expect(result, '先看背景资料。\n然后我们再同步。');
  });

  testWidgets('forward conversation dialog renders group member avatar', (
    tester,
  ) async {
    const members = <SessionAvatarMember>[
      SessionAvatarMember(
        memberId: '1001',
        memberType: 1,
        displayName: 'Alice',
        avatarUrl: '',
      ),
      SessionAvatarMember(
        memberId: '9001',
        memberType: 2,
        displayName: 'Agent',
        avatarUrl: '',
      ),
    ];

    await tester.pumpWidget(
      GetMaterialApp(
        home: Scaffold(
          body: Builder(
            builder: (context) => FilledButton(
              onPressed: () {
                showChatForwardConversationDialog(
                  context: context,
                  sourceTitle: '产品讨论群',
                  targetTitle: '研发群',
                  sourceIsGroup: true,
                  sourceAvatarTitle: '产品讨论群',
                  sourceAvatarUrl: 'https://example.com/ignored.png',
                  sourceAvatarMembers: members,
                );
              },
              child: const Text('open group'),
            ),
          ),
        ),
      ),
    );

    await tester.tap(find.text('open group'));
    await tester.pumpAndSettle();

    final avatar = tester.widget<SessionAvatar>(find.byType(SessionAvatar));
    expect(avatar.isGroup, isTrue);
    expect(avatar.avatarUrl, isEmpty);
    expect(avatar.members, same(members));
    expect(avatar.memberFallbackColor, avatar.avatarColor);
  });
}
