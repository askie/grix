import 'package:sqflite/sqflite.dart';

import 'local_db_factory_initializer_stub.dart'
    if (dart.library.js_interop) 'local_db_factory_initializer_web.dart'
    if (dart.library.io) 'local_db_factory_initializer_io.dart'
    as impl;

Future<DatabaseFactory> initLocalDatabaseFactory() =>
    impl.initLocalDatabaseFactory();
