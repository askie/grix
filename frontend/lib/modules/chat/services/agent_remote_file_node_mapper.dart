import '../../../shared/widgets/remote_file_picker/remote_file_picker_model.dart';

RemoteFileNode mapAgentRemoteFileNode(Map<String, dynamic> raw) {
  final id =
      _firstNonEmptyString(raw['id']) ??
      _firstNonEmptyString(raw['path']) ??
      _firstNonEmptyString(raw['current_path']) ??
      '';
  return RemoteFileNode(
    id: id,
    name: raw['name']?.toString() ?? '',
    isDirectory: raw['is_directory'] == true,
    size: raw['size'] as int?,
    modifiedAt: raw['modified_at'] != null
        ? DateTime.tryParse(raw['modified_at'].toString())
        : null,
    mimeType: raw['mime_type']?.toString(),
  );
}

String? _firstNonEmptyString(Object? value) {
  final normalized = value?.toString().trim() ?? '';
  if (normalized.isEmpty) {
    return null;
  }
  return normalized;
}
