/// Whitelisted parameter metadata. Editors must opt in to supported types.
class SystemControl {
  const SystemControl({
    required this.key,
    required this.label,
    required this.description,
    required this.valueType,
    required this.value,
  });
  final String key;
  final String label;
  final String description;
  final String valueType;
  final Object? value;

  factory SystemControl.fromJson(Map<String, dynamic> json) {
    final type = json['value_type'] as String;
    final value = json['value'];
    if (type == 'boolean' && value is! bool) {
      throw const FormatException('系统控制返回的布尔值无效');
    }
    return SystemControl(
      key: json['key'] as String,
      label: json['label'] as String,
      description: json['description'] as String,
      valueType: type,
      value: value,
    );
  }
}
