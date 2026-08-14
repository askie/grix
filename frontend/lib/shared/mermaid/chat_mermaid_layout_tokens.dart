/// Shared spacing tokens for Mermaid diagram layout engines.
///
/// Keep diagram-level spacing centralized here so compactness adjustments
/// can be managed consistently.
class ChatMermaidLayoutTokens {
  const ChatMermaidLayoutTokens._();

  static const int flowchartLevelSeparation = 72;
  static const int flowchartNodeSeparation = 28;

  /// Subgraph 分组框相对框内成员节点包围盒的外扩量。
  ///
  /// 顶部留得更大是为了容纳分组标题。分组框的「绘制」与「重叠消解」必须共用
  /// 同一组数值，否则消解时算出的框与实际画出的框不一致、又会重叠。
  static const double subgraphPaddingLeft = 18;
  static const double subgraphPaddingRight = 18;
  static const double subgraphPaddingTop = 34;
  static const double subgraphPaddingBottom = 18;

  /// 嵌套分组每多套一层，外层框在基础外扩量之上额外让出的空间。
  ///
  /// 分组框是「成员节点包围盒 + 外扩量」画出来的，而嵌套时外层分组的成员集合
  /// 包含了内层分组的全部成员，两者的包围盒往往同边。若所有层级共用一组外扩量，
  /// 外层框与贴边的子框边界就会完全重合，看上去像是框叠框。按嵌套层数逐级放大
  /// 外扩量，外层才真正「包住」子框。
  ///
  /// 顶部的放大量不在这里写死——它要装下外层自己的标题，必须由标题的实际高度推
  /// 出（字号变大时写死的数值会不够，标题会压到子框的边框上）。见布局引擎里的
  /// `_measureNestedTopStep`。
  static const double subgraphNestedPaddingStep = 16;

  /// 分组标题相对框顶的偏移，以及标题下方与内容之间的间隙。
  /// 标题矩形的算法（[subgraphLabelTopOffset] + 标题高度）与嵌套顶部让位量同源。
  static const double subgraphLabelTopOffset = 10;
  static const double subgraphLabelBottomGap = 8;
}
