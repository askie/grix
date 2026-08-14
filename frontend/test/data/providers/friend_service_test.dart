import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:get/get.dart' hide Response;
import 'package:grix/data/providers/auth_service.dart';
import 'package:grix/data/providers/friend_service.dart';

class _FakeAuthService extends AuthService {
  @override
  void attachAuthInterceptor(Dio dio) {}
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  setUp(() {
    Get.testMode = true;
    Get.reset();
    Get.put<AuthService>(_FakeAuthService());
  });

  tearDown(() {
    Get.reset();
  });

  test('fetchUserProfile skips invalid non-numeric ids', () async {
    var requestCount = 0;
    final dio = Dio(BaseOptions(baseUrl: 'https://example.com'))
      ..interceptors.add(
        InterceptorsWrapper(
          onRequest: (options, handler) {
            requestCount++;
            handler.resolve(
              Response<dynamic>(
                requestOptions: options,
                statusCode: 200,
                data: {
                  'code': 0,
                  'data': {
                    'nickname': 'Tester',
                    'username': 'tester',
                    'avatar_url': '',
                  },
                },
              ),
            );
          },
        ),
      );

    final service = await FriendService(dio: dio).init();
    final result = await service.fetchUserProfile('session-send-cross-node');

    expect(result, isNull);
    expect(requestCount, 0);
  });

  test(
    'fetchUserProfile caches 404 misses and avoids repeated requests',
    () async {
      var requestCount = 0;
      final dio = Dio(BaseOptions(baseUrl: 'https://example.com'))
        ..interceptors.add(
          InterceptorsWrapper(
            onRequest: (options, handler) {
              requestCount++;
              handler.reject(
                DioException(
                  requestOptions: options,
                  response: Response<dynamic>(
                    requestOptions: options,
                    statusCode: 404,
                    data: {'code': 10004, 'msg': '用户不存在'},
                  ),
                  type: DioExceptionType.badResponse,
                ),
              );
            },
          ),
        );

      final service = await FriendService(dio: dio).init();

      expect(await service.fetchUserProfile('1001'), isNull);
      expect(await service.fetchUserProfile('1001'), isNull);
      expect(requestCount, 1);
    },
  );

  test(
    'fetchUserProfile caches 200 empty-data misses and avoids repeats',
    () async {
      var requestCount = 0;
      final dio = Dio(BaseOptions(baseUrl: 'https://example.com'))
        ..interceptors.add(
          InterceptorsWrapper(
            onRequest: (options, handler) {
              requestCount++;
              handler.resolve(
                Response<dynamic>(
                  requestOptions: options,
                  statusCode: 200,
                  data: {'code': 0, 'data': null},
                ),
              );
            },
          ),
        );

      final service = await FriendService(dio: dio).init();

      expect(await service.fetchUserProfile('3001'), isNull);
      expect(await service.fetchUserProfile('3001'), isNull);
      expect(requestCount, 1);
    },
  );

  test('fetchUserProfile falls back to visitor label when unnamed', () async {
    final dio = Dio(BaseOptions(baseUrl: 'https://example.com'))
      ..interceptors.add(
        InterceptorsWrapper(
          onRequest: (options, handler) {
            handler.resolve(
              Response<dynamic>(
                requestOptions: options,
                statusCode: 200,
                data: {
                  'code': 0,
                  'data': {
                    'nickname': '',
                    'username': '',
                    'avatar_url': '',
                    'is_visitor': true,
                  },
                },
              ),
            );
          },
        ),
      );

    final service = await FriendService(dio: dio).init();
    // 测试环境未加载翻译，'common_visitor'.tr 返回 key 本身，足以验证走了访客兜底分支
    expect(await service.fetchUserProfile('4001'), 'common_visitor');
  });

  test('fetchUserProfile uses visitor name when provided', () async {
    final dio = Dio(BaseOptions(baseUrl: 'https://example.com'))
      ..interceptors.add(
        InterceptorsWrapper(
          onRequest: (options, handler) {
            handler.resolve(
              Response<dynamic>(
                requestOptions: options,
                statusCode: 200,
                data: {
                  'code': 0,
                  'data': {
                    'nickname': '访客阿强',
                    'username': '',
                    'avatar_url': '',
                    'is_visitor': true,
                  },
                },
              ),
            );
          },
        ),
      );

    final service = await FriendService(dio: dio).init();
    expect(await service.fetchUserProfile('4002'), '访客阿强');
  });

  test('fetchUserProfile cache is cleared by resetForAccountSwitch', () async {
    var requestCount = 0;
    final dio = Dio(BaseOptions(baseUrl: 'https://example.com'))
      ..interceptors.add(
        InterceptorsWrapper(
          onRequest: (options, handler) {
            requestCount++;
            handler.resolve(
              Response<dynamic>(
                requestOptions: options,
                statusCode: 200,
                data: {
                  'code': 0,
                  'data': {
                    'nickname': 'Tester',
                    'username': 'tester',
                    'avatar_url': 'https://example.com/avatar.png',
                  },
                },
              ),
            );
          },
        ),
      );

    final service = await FriendService(dio: dio).init();

    expect(await service.fetchUserProfile('2001'), 'Tester');
    expect(await service.fetchUserProfile('2001'), 'Tester');
    expect(requestCount, 1);

    service.resetForAccountSwitch();

    expect(await service.fetchUserProfile('2001'), 'Tester');
    expect(requestCount, 2);
  });

  test('blockUser removes friend locally after success', () async {
    final dio = Dio(BaseOptions(baseUrl: 'https://example.com'))
      ..interceptors.add(
        InterceptorsWrapper(
          onRequest: (options, handler) {
            handler.resolve(
              Response<dynamic>(
                requestOptions: options,
                statusCode: 200,
                data: {'code': 0, 'data': null},
              ),
            );
          },
        ),
      );

    final service = await FriendService(dio: dio).init();
    service.friendList.assignAll([
      FriendItem(
        id: '1',
        userId: '2001',
        username: 'alice',
        nickname: 'Alice',
        remarkName: '',
        avatarUrl: '',
      ),
    ]);

    final result = await service.blockUser('2001');

    expect(result, isTrue);
    expect(service.friendList, isEmpty);
  });

  test('applyRealtimeEvent mutates friend requests and friend list', () async {
    final service = await FriendService(
      dio: Dio(BaseOptions(baseUrl: 'https://example.com')),
    ).init();

    service.applyRealtimeEvent(<String, dynamic>{
      'event': 'friend_request_received',
      'request': <String, dynamic>{
        'id': '9001',
        'from_user_id': '2001',
        'username': 'alice',
        'nickname': 'Alice',
        'avatar_url': '',
        'message': 'hi',
        'status': 0,
        'created_at': '2026-03-19 10:00:00',
      },
    });

    expect(service.friendRequests, hasLength(1));
    expect(service.friendRequests.first.status, 0);

    service.applyRealtimeEvent(<String, dynamic>{
      'event': 'friend_request_handled',
      'request_id': '9001',
      'status': 1,
    });
    expect(service.friendRequests.first.status, 1);

    service.applyRealtimeEvent(<String, dynamic>{
      'event': 'friend_added',
      'friend': <String, dynamic>{
        'id': '3001',
        'user_id': '2001',
        'username': 'alice',
        'nickname': 'Alice',
        'remark_name': 'Ali',
        'avatar_url': '',
      },
    });
    expect(service.friendList, hasLength(1));
    expect(service.friendList.first.userId, '2001');
    expect(service.friendList.first.remarkName, 'Ali');

    service.applyRealtimeEvent(<String, dynamic>{
      'event': 'friend_deleted',
      'friend_user_id': '2001',
    });
    expect(service.friendList, isEmpty);
  });
}
