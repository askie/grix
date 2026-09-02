import 'chat_message_card_data.dart';
import 'chat_message_card_type.dart';

/// 进度条卡片的数据模型。
///
/// 展示一行文字 [label] 与一个百分比进度。当 [percent] 为 null 时表示
/// 「不确定态」——进度未知，前端渲染为循环动画的进度条。
class ChatProgressCardData extends ChatMessageCardData {
  const ChatProgressCardData({required this.label, this.percent})
    : super(type: ChatMessageCardType.progress);

  /// 进度条上方展示的一行说明文字。
  final String label;

  /// 进度百分比，取值 0–100。为 null 表示不确定态（进度未知）。
  final int? percent;

  String get displayLabel => label.trim();

  /// 是否为不确定态（无具体百分比）。
  bool get isIndeterminate => percent == null;

  /// 归一化后的百分比（0–100）。不确定态返回 null。
  int? get clampedPercent {
    final value = percent;
    if (value == null) {
      return null;
    }
    if (value < 0) {
      return 0;
    }
    if (value > 100) {
      return 100;
    }
    return value;
  }

  /// 进度条控件使用的 0.0–1.0 比例值。不确定态返回 null。
  double? get fraction {
    final value = clampedPercent;
    if (value == null) {
      return null;
    }
    return value / 100.0;
  }

  @override
  Map<String, dynamic> toPayload() {
    return <String, dynamic>{'label': label, 'percent': percent};
  }
}
