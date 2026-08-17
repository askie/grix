/// web 构建不含 dart:io，测试环境检测恒为 false（web 不跑 flutter test）。
bool get isFlutterTestEnv => false;
