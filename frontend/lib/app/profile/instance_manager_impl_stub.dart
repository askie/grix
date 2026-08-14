import 'profile_instance_info.dart';

/// 网页版：不支持多实例管理。
bool instanceManagerSupported() => false;

String currentProfileName() => 'default';

Future<List<ProfileInstanceInfo>> listInstances() async =>
    const <ProfileInstanceInfo>[];

Future<void> openInstance(String name) async {}

Future<void> launchNewInstance() async {}
