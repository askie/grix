import 'package:file_picker/file_picker.dart';

class ChatAgentOpenSessionDirectoryPicker {
  const ChatAgentOpenSessionDirectoryPicker._();

  static Future<String?> pickDirectory() {
    return FilePicker.platform.getDirectoryPath();
  }
}
