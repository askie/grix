import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/widgets/message_bubble.dart';

void main() {
  setUp(() {
    MessageStreamController.resetForTest();
  });

  tearDown(() {
    MessageStreamController.resetForTest();
  });

  testWidgets('addChunk batches rapid chunks into one flush', (
    WidgetTester tester,
  ) async {
    final stream = MessageStreamController.getStream('m1');
    final events = <String>[];
    final sub = stream.listen(events.add);

    MessageStreamController.addChunk('m1', 'a');
    MessageStreamController.addChunk('m1', 'b');
    MessageStreamController.addChunk('m1', 'c');

    await tester.pump(const Duration(milliseconds: 60));
    expect(events.where((e) => e == 'abc'), isEmpty);

    await tester.pump(const Duration(milliseconds: 40));
    expect(events.where((e) => e == 'abc').length, 1);

    sub.cancel();
  });

  testWidgets('finish emits final content and resets stream slot', (
    WidgetTester tester,
  ) async {
    final stream = MessageStreamController.getStream('m2');
    final events = <String>[];
    final sub = stream.listen(events.add);

    MessageStreamController.addChunk('m2', 'partial');
    await tester.pump(const Duration(milliseconds: 10));

    MessageStreamController.finish('m2', 'final_content');
    await tester.pump();

    expect(events.contains('final_content'), isTrue);
    expect(stream.isClosed, isTrue);

    final nextStream = MessageStreamController.getStream('m2');
    expect(identical(stream, nextStream), isFalse);
    expect(nextStream.value, '');

    sub.cancel();
  });

  testWidgets('ordered chunks are reassembled by chunk_seq when out of order', (
    WidgetTester tester,
  ) async {
    final stream = MessageStreamController.getStream('m4');
    final events = <String>[];
    final sub = stream.listen(events.add);

    MessageStreamController.addChunk('m4', 'B', chunkSeq: 2);
    MessageStreamController.addChunk('m4', 'A', chunkSeq: 1);
    MessageStreamController.addChunk('m4', 'C', chunkSeq: 3);

    await tester.pump(const Duration(milliseconds: 120));
    expect(events.where((e) => e == 'ABC').length, 1);

    sub.cancel();
  });

  testWidgets('does not emit out-of-order content before missing head chunk', (
    WidgetTester tester,
  ) async {
    final stream = MessageStreamController.getStream('m5');
    final events = <String>[];
    final sub = stream.listen(events.add);

    MessageStreamController.addChunk('m5', 'B', chunkSeq: 2);
    await tester.pump(const Duration(milliseconds: 120));
    expect(
      events.where((e) => e == 'B' || e == 'AB' || e == 'ABC').isEmpty,
      isTrue,
    );

    MessageStreamController.addChunk('m5', 'A', chunkSeq: 1);
    await tester.pump(const Duration(milliseconds: 120));
    expect(events.where((e) => e == 'AB').length, 1);

    MessageStreamController.addChunk('m5', 'C', chunkSeq: 3);
    await tester.pump(const Duration(milliseconds: 120));
    expect(events.where((e) => e == 'ABC').length, 1);

    sub.cancel();
  });

  testWidgets('peekContent returns buffered text before flush', (
    WidgetTester tester,
  ) async {
    MessageStreamController.addChunk('m6', 'hello');
    expect(MessageStreamController.peekContent('m6'), 'hello');

    MessageStreamController.finish('m6', 'done');
    await tester.pump();
    expect(MessageStreamController.peekContent('m6'), '');
  });

  testWidgets(
    'recoverable snapshot survives finish and stays isolated by msgId',
    (WidgetTester tester) async {
      MessageStreamController.addChunk('m7', 'alpha');
      MessageStreamController.addChunk('m8', 'beta');
      await tester.pump(const Duration(milliseconds: 100));

      expect(MessageStreamController.peekRecoverableContent('m7'), 'alpha');
      expect(MessageStreamController.peekRecoverableContent('m8'), 'beta');

      MessageStreamController.finish('m7', '');
      await tester.pump();

      expect(MessageStreamController.peekContent('m7'), '');
      expect(MessageStreamController.peekRecoverableContent('m7'), 'alpha');
      expect(MessageStreamController.peekRecoverableContent('m8'), 'beta');

      MessageStreamController.discard('m7');
      expect(MessageStreamController.peekRecoverableContent('m7'), '');
      expect(MessageStreamController.peekRecoverableContent('m8'), 'beta');
    },
  );

  testWidgets('transfer moves recoverable snapshot to the new msgId', (
    WidgetTester tester,
  ) async {
    MessageStreamController.addChunk('old-msg', 'migrated content');
    await tester.pump(const Duration(milliseconds: 100));

    expect(
      MessageStreamController.peekRecoverableContent('old-msg'),
      'migrated content',
    );

    MessageStreamController.transfer('old-msg', 'new-msg');

    expect(MessageStreamController.peekRecoverableContent('old-msg'), '');
    expect(
      MessageStreamController.peekRecoverableContent('new-msg'),
      'migrated content',
    );
  });
}
