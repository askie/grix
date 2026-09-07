import 'package:flutter_test/flutter_test.dart';
import 'package:grix/data/models/local_search_result.dart';
import 'package:grix/shared/utils/local_contact_matcher.dart';

MatchedContact _contact(
  String peerId, {
  required String displayName,
  String username = '',
  String introduction = '',
  int peerType = 1,
}) {
  return MatchedContact(
    peerId: peerId,
    peerType: peerType,
    displayName: displayName,
    username: username,
    introduction: introduction,
  );
}

void main() {
  test('两个词都命中的联系人排在只命中一个的前面', () {
    final matched = LocalContactMatcher.match([
      _contact('p-partial', displayName: '老王'),
      _contact('p-full', displayName: '老王', introduction: '负责装修'),
      _contact('p-other', displayName: '装修队'),
    ], ['老王', '装修']);

    expect(matched.map((c) => c.peerId).toList(), [
      'p-full',
      'p-partial',
      'p-other',
    ]);
  });

  test('一个词都不命中的联系人被排除', () {
    final matched = LocalContactMatcher.match([
      _contact('p-hit', displayName: '装修队'),
      _contact('p-miss', displayName: '读书会'),
    ], ['装修']);

    expect(matched.map((c) => c.peerId).toList(), ['p-hit']);
  });

  test('用户名与简介参与匹配，且忽略大小写', () {
    final matched = LocalContactMatcher.match([
      _contact('p-username', displayName: '张三', username: 'LaoWang'),
      _contact('p-intro', displayName: '李四', introduction: 'laowang 的同事'),
      _contact('p-miss', displayName: '王五'),
    ], ['laowang']);

    expect(matched.map((c) => c.peerId).toList(), ['p-username', 'p-intro']);
  });

  test('按 limit 截断，并对同一对端去重', () {
    final matched = LocalContactMatcher.match([
      _contact('p-1', displayName: '装修一'),
      _contact('p-1', displayName: '装修一重复'),
      _contact('p-2', displayName: '装修二'),
      _contact('p-3', displayName: '装修三'),
    ], ['装修'], limit: 2);

    expect(matched.map((c) => c.peerId).toList(), ['p-1', 'p-2']);
  });

  test('同一 id 的用户与 Agent 互不遮蔽', () {
    final matched = LocalContactMatcher.match([
      _contact('x', displayName: '装修助手', peerType: 1),
      _contact('x', displayName: '装修助手', peerType: 2),
    ], ['装修']);

    expect(matched.map((c) => c.peerType).toList(), [1, 2]);
  });
}
