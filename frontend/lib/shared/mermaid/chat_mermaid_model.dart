enum ChatMermaidDiagramType {
  flowchart,
  sequence,
  state,
  gantt,
  classDiagram,
  erDiagram,
  pie,
  mindmap,
  journey,
  gitGraph,
  xyChart,
  timeline,
  quadrant,
  sankey,
  radar,
  kanban,
  treemap,
  block,
  packet,
  requirement,
}

enum ChatMermaidFlowDirection { topDown, bottomTop, leftRight, rightLeft }

enum ChatMermaidNodeShape {
  rectangle,
  rounded,
  diamond,
  circle,
  stadium,
  subroutine,
  cylindrical,
  hexagon,
}

enum ChatMermaidEdgeStyle {
  solidArrow,
  solidLine,
  dashedArrow,
  thickArrow,
  circle,
  cross,
}

abstract class ChatMermaidDiagram {
  const ChatMermaidDiagram();

  ChatMermaidDiagramType get type;
}

class ChatMermaidFlowchart extends ChatMermaidDiagram {
  const ChatMermaidFlowchart({
    required this.direction,
    required this.nodes,
    required this.edges,
    required this.subgraphs,
  });

  final ChatMermaidFlowDirection direction;
  final List<ChatMermaidNode> nodes;
  final List<ChatMermaidEdge> edges;
  final List<ChatMermaidFlowSubgraph> subgraphs;

  @override
  ChatMermaidDiagramType get type => ChatMermaidDiagramType.flowchart;
}

class ChatMermaidFlowSubgraph {
  const ChatMermaidFlowSubgraph({
    required this.id,
    required this.label,
    required this.order,
    required this.depth,
    required this.nodeIds,
  });

  final String id;
  final String label;
  final int order;
  final int depth;
  final List<String> nodeIds;
}

class ChatMermaidNode {
  const ChatMermaidNode({
    required this.id,
    required this.label,
    required this.shape,
    required this.order,
    this.fillColor,
    this.strokeColor,
    this.textColor,
  });

  final String id;
  final String label;
  final ChatMermaidNodeShape shape;
  final int order;

  /// 节点填充色，来自 classDef/style/:::className 语法；ARGB 整数。
  final int? fillColor;

  /// 节点边框色，来自 classDef/style/:::className 语法；ARGB 整数。
  final int? strokeColor;

  /// 节点文字色，来自 classDef/style/:::className 的 color 属性；ARGB 整数。
  final int? textColor;
}

class ChatMermaidEdge {
  const ChatMermaidEdge({
    required this.sourceId,
    required this.targetId,
    required this.style,
    required this.order,
    this.label,
  });

  final String sourceId;
  final String targetId;
  final String? label;
  final ChatMermaidEdgeStyle style;
  final int order;
}

enum ChatMermaidSequenceMessageStyle {
  solidArrow,
  dashedArrow,
  solidLine,
  dashedLine,
}

enum ChatMermaidSequenceNotePosition { over, leftOf, rightOf }

enum ChatMermaidSequenceGroupKind { loop, alt, opt, par, critical, breakBlock }

class ChatMermaidSequenceDiagram extends ChatMermaidDiagram {
  const ChatMermaidSequenceDiagram({
    required this.participants,
    required this.events,
  });

  final List<ChatMermaidSequenceParticipant> participants;
  final List<ChatMermaidSequenceEvent> events;

  @override
  ChatMermaidDiagramType get type => ChatMermaidDiagramType.sequence;
}

enum ChatMermaidStateNodeKind { regular, start, end }

class ChatMermaidStateDiagram extends ChatMermaidDiagram {
  const ChatMermaidStateDiagram({
    required this.nodes,
    required this.transitions,
  });

  final List<ChatMermaidStateNode> nodes;
  final List<ChatMermaidStateTransition> transitions;

  @override
  ChatMermaidDiagramType get type => ChatMermaidDiagramType.state;
}

class ChatMermaidStateNode {
  const ChatMermaidStateNode({
    required this.id,
    required this.label,
    required this.kind,
    required this.order,
  });

  final String id;
  final String label;
  final ChatMermaidStateNodeKind kind;
  final int order;
}

class ChatMermaidStateTransition {
  const ChatMermaidStateTransition({
    required this.sourceId,
    required this.targetId,
    required this.order,
    this.label,
  });

  final String sourceId;
  final String targetId;
  final String? label;
  final int order;

  bool get isSelfTransition => sourceId == targetId;
}

class ChatMermaidGanttDiagram extends ChatMermaidDiagram {
  const ChatMermaidGanttDiagram({
    required this.title,
    required this.axisFormat,
    required this.rangeStart,
    required this.rangeEndExclusive,
    required this.sections,
  });

  final String title;
  final String axisFormat;
  final DateTime rangeStart;
  final DateTime rangeEndExclusive;
  final List<ChatMermaidGanttSection> sections;

  @override
  ChatMermaidDiagramType get type => ChatMermaidDiagramType.gantt;
}

class ChatMermaidGanttSection {
  const ChatMermaidGanttSection({
    required this.title,
    required this.order,
    required this.tasks,
  });

  final String title;
  final int order;
  final List<ChatMermaidGanttTask> tasks;
}

class ChatMermaidGanttTask {
  const ChatMermaidGanttTask({
    required this.label,
    required this.startDate,
    required this.durationDays,
    required this.order,
    this.id,
  });

  final String? id;
  final String label;
  final DateTime startDate;
  final int durationDays;
  final int order;

  DateTime get endDateExclusive => startDate.add(Duration(days: durationDays));
}

class ChatMermaidSequenceParticipant {
  const ChatMermaidSequenceParticipant({
    required this.id,
    required this.label,
    required this.order,
    this.isActor = false,
  });

  final String id;
  final String label;
  final int order;
  final bool isActor;
}

abstract class ChatMermaidSequenceEvent {
  const ChatMermaidSequenceEvent({required this.order});

  final int order;
}

class ChatMermaidSequenceMessage extends ChatMermaidSequenceEvent {
  const ChatMermaidSequenceMessage({
    required super.order,
    required this.fromId,
    required this.toId,
    required this.label,
    required this.style,
  });

  final String fromId;
  final String toId;
  final String label;
  final ChatMermaidSequenceMessageStyle style;

  bool get isSelfMessage => fromId == toId;
}

class ChatMermaidSequenceNote extends ChatMermaidSequenceEvent {
  const ChatMermaidSequenceNote({
    required super.order,
    required this.position,
    required this.targetIds,
    required this.text,
  });

  final ChatMermaidSequenceNotePosition position;
  final List<String> targetIds;
  final String text;
}

class ChatMermaidSequenceGroupStart extends ChatMermaidSequenceEvent {
  const ChatMermaidSequenceGroupStart({
    required super.order,
    required this.kind,
    required this.label,
  });

  final ChatMermaidSequenceGroupKind kind;
  final String label;
}

class ChatMermaidSequenceGroupDivider extends ChatMermaidSequenceEvent {
  const ChatMermaidSequenceGroupDivider({
    required super.order,
    required this.label,
  });

  final String label;
}

class ChatMermaidSequenceGroupEnd extends ChatMermaidSequenceEvent {
  const ChatMermaidSequenceGroupEnd({required super.order});
}

// ---------------------------------------------------------------------------
// Class Diagram
// ---------------------------------------------------------------------------

class ChatMermaidClassDiagram extends ChatMermaidDiagram {
  const ChatMermaidClassDiagram({
    required this.classes,
    required this.relations,
  });

  final List<ChatMermaidClassItem> classes;
  final List<ChatMermaidClassRelation> relations;

  @override
  ChatMermaidDiagramType get type => ChatMermaidDiagramType.classDiagram;
}

class ChatMermaidClassItem {
  const ChatMermaidClassItem({
    required this.id,
    required this.label,
    required this.members,
    required this.order,
  });

  final String id;
  final String label;
  final List<String> members;
  final int order;
}

enum ChatMermaidClassRelationType {
  inheritance,
  composition,
  aggregation,
  association,
  dependency,
  realization,
}

class ChatMermaidClassRelation {
  const ChatMermaidClassRelation({
    required this.sourceId,
    required this.targetId,
    required this.relationType,
    required this.order,
    this.label,
  });

  final String sourceId;
  final String targetId;
  final ChatMermaidClassRelationType relationType;
  final String? label;
  final int order;
}

// ---------------------------------------------------------------------------
// ER Diagram
// ---------------------------------------------------------------------------

class ChatMermaidErDiagram extends ChatMermaidDiagram {
  const ChatMermaidErDiagram({required this.entities, required this.relations});

  final List<ChatMermaidErEntity> entities;
  final List<ChatMermaidErRelation> relations;

  @override
  ChatMermaidDiagramType get type => ChatMermaidDiagramType.erDiagram;
}

class ChatMermaidErEntity {
  const ChatMermaidErEntity({required this.id, required this.order});

  final String id;
  final int order;
}

enum ChatMermaidErCardinality { exactlyOne, zeroOrOne, oneOrMore, zeroOrMore }

class ChatMermaidErRelation {
  const ChatMermaidErRelation({
    required this.sourceId,
    required this.targetId,
    required this.sourceCardinality,
    required this.targetCardinality,
    required this.label,
    required this.order,
  });

  final String sourceId;
  final String targetId;
  final ChatMermaidErCardinality sourceCardinality;
  final ChatMermaidErCardinality targetCardinality;
  final String label;
  final int order;
}

// ---------------------------------------------------------------------------
// Pie Diagram
// ---------------------------------------------------------------------------

class ChatMermaidPieDiagram extends ChatMermaidDiagram {
  const ChatMermaidPieDiagram({required this.title, required this.slices});

  final String title;
  final List<ChatMermaidPieSlice> slices;

  double get total => slices.fold(0, (sum, s) => sum + s.value);

  @override
  ChatMermaidDiagramType get type => ChatMermaidDiagramType.pie;
}

class ChatMermaidPieSlice {
  const ChatMermaidPieSlice({
    required this.label,
    required this.value,
    required this.order,
  });

  final String label;
  final double value;
  final int order;
}

// ---------------------------------------------------------------------------
// Mindmap Diagram
// ---------------------------------------------------------------------------

class ChatMermaidMindmapDiagram extends ChatMermaidDiagram {
  const ChatMermaidMindmapDiagram({required this.root});

  final ChatMermaidMindmapNode root;

  @override
  ChatMermaidDiagramType get type => ChatMermaidDiagramType.mindmap;
}

class ChatMermaidMindmapNode {
  const ChatMermaidMindmapNode({
    required this.label,
    required this.shape,
    required this.children,
    required this.order,
  });

  final String label;
  final ChatMermaidNodeShape shape;
  final List<ChatMermaidMindmapNode> children;
  final int order;
}

// ---------------------------------------------------------------------------
// Journey Diagram
// ---------------------------------------------------------------------------

class ChatMermaidJourneyDiagram extends ChatMermaidDiagram {
  const ChatMermaidJourneyDiagram({
    required this.title,
    required this.sections,
  });

  final String title;
  final List<ChatMermaidJourneySection> sections;

  @override
  ChatMermaidDiagramType get type => ChatMermaidDiagramType.journey;
}

class ChatMermaidJourneySection {
  const ChatMermaidJourneySection({
    required this.title,
    required this.tasks,
    required this.order,
  });

  final String title;
  final List<ChatMermaidJourneyTask> tasks;
  final int order;
}

class ChatMermaidJourneyTask {
  const ChatMermaidJourneyTask({
    required this.label,
    required this.score,
    required this.actors,
    required this.order,
  });

  final String label;
  final int score;
  final List<String> actors;
  final int order;
}

// ---------------------------------------------------------------------------
// Git Graph Diagram
// ---------------------------------------------------------------------------

class ChatMermaidGitGraphDiagram extends ChatMermaidDiagram {
  const ChatMermaidGitGraphDiagram({
    required this.commits,
    required this.branches,
  });

  final List<ChatMermaidGitCommit> commits;
  final List<String> branches;

  @override
  ChatMermaidDiagramType get type => ChatMermaidDiagramType.gitGraph;
}

class ChatMermaidGitCommit {
  const ChatMermaidGitCommit({
    required this.id,
    required this.branch,
    required this.order,
    this.tag,
    this.mergeFrom,
  });

  final String id;
  final String branch;
  final String? tag;
  final String? mergeFrom;
  final int order;
}

// ---------------------------------------------------------------------------
// XyChart Diagram
// ---------------------------------------------------------------------------

class ChatMermaidXyChartDiagram extends ChatMermaidDiagram {
  const ChatMermaidXyChartDiagram({
    required this.title,
    required this.xAxisTitle,
    required this.xAxisLabels,
    required this.yAxisTitle,
    required this.yAxisMax,
    this.yAxisMin = 0,
    this.horizontal = false,
    this.barSeries = const [],
    this.lineSeries = const [],
  });

  final String title;
  final String xAxisTitle;
  final List<String> xAxisLabels;
  final String yAxisTitle;
  final double yAxisMax;
  final double yAxisMin;
  final bool horizontal;
  final List<List<double>> barSeries;
  final List<List<double>> lineSeries;

  @override
  ChatMermaidDiagramType get type => ChatMermaidDiagramType.xyChart;
}

// ---------------------------------------------------------------------------
// Parse Result
// ---------------------------------------------------------------------------

class ChatMermaidParseResult {
  const ChatMermaidParseResult.supported({required this.diagram})
    : error = null;

  ChatMermaidParseResult.unsupported({required this.error}) : diagram = null;

  final ChatMermaidDiagram? diagram;
  final String? error;

  bool get isSupported => diagram != null;
}

/// 时间线图(timeline):由若干分组(section)构成,每个分组包含若干时间段
/// (period),每个时间段下挂若干事件(event)。无显式分组时归入一个无名分组。
class ChatMermaidTimelineDiagram extends ChatMermaidDiagram {
  const ChatMermaidTimelineDiagram({
    required this.title,
    required this.sections,
  });

  final String title;
  final List<ChatMermaidTimelineSection> sections;

  @override
  ChatMermaidDiagramType get type => ChatMermaidDiagramType.timeline;
}

class ChatMermaidTimelineSection {
  const ChatMermaidTimelineSection({
    required this.title,
    required this.periods,
    required this.order,
  });

  /// 无名分组为空字符串。
  final String title;
  final List<ChatMermaidTimelinePeriod> periods;
  final int order;
}

class ChatMermaidTimelinePeriod {
  const ChatMermaidTimelinePeriod({
    required this.label,
    required this.events,
    required this.order,
  });

  /// 时间段标题(如 2002、"2010s")。
  final String label;
  final List<String> events;
  final int order;
}

/// 象限图(quadrantChart):一个二维平面被中线分成四个象限,
/// 点的 x/y 取值范围为 0..1。象限编号遵循 mermaid 约定:
/// quadrant1=右上,quadrant2=左上,quadrant3=左下,quadrant4=右下。
class ChatMermaidQuadrantDiagram extends ChatMermaidDiagram {
  const ChatMermaidQuadrantDiagram({
    required this.title,
    required this.xAxisLeft,
    required this.xAxisRight,
    required this.yAxisBottom,
    required this.yAxisTop,
    required this.quadrant1,
    required this.quadrant2,
    required this.quadrant3,
    required this.quadrant4,
    required this.points,
  });

  final String title;
  final String xAxisLeft;
  final String xAxisRight;
  final String yAxisBottom;
  final String yAxisTop;
  final String quadrant1;
  final String quadrant2;
  final String quadrant3;
  final String quadrant4;
  final List<ChatMermaidQuadrantPoint> points;

  @override
  ChatMermaidDiagramType get type => ChatMermaidDiagramType.quadrant;
}

class ChatMermaidQuadrantPoint {
  const ChatMermaidQuadrantPoint({
    required this.label,
    required this.x,
    required this.y,
    required this.order,
  });

  final String label;

  /// 横坐标,0..1(0=最左,1=最右)。
  final double x;

  /// 纵坐标,0..1(0=最下,1=最上)。
  final double y;
  final int order;
}

/// 桑基流向图(sankey-beta):由 CSV 三列(source,target,value)定义的有向加权流。
/// 节点按首次出现顺序去重,连接保留输入顺序。
class ChatMermaidSankeyDiagram extends ChatMermaidDiagram {
  const ChatMermaidSankeyDiagram({required this.nodes, required this.links});

  final List<ChatMermaidSankeyNode> nodes;
  final List<ChatMermaidSankeyLink> links;

  @override
  ChatMermaidDiagramType get type => ChatMermaidDiagramType.sankey;
}

class ChatMermaidSankeyNode {
  const ChatMermaidSankeyNode({required this.id, required this.order});

  final String id;
  final int order;
}

class ChatMermaidSankeyLink {
  const ChatMermaidSankeyLink({
    required this.sourceId,
    required this.targetId,
    required this.value,
    required this.order,
  });

  final String sourceId;
  final String targetId;
  final double value;
  final int order;
}

/// 雷达图格网形状。
enum ChatMermaidRadarGraticule { circle, polygon }

/// 雷达图(radar-beta):多条曲线在一组共享轴上的取值,以蛛网/星形展示。
class ChatMermaidRadarDiagram extends ChatMermaidDiagram {
  const ChatMermaidRadarDiagram({
    required this.title,
    required this.axes,
    required this.curves,
    required this.minValue,
    required this.maxValue,
    required this.ticks,
    required this.graticule,
    required this.showLegend,
  });

  final String title;
  final List<ChatMermaidRadarAxis> axes;
  final List<ChatMermaidRadarCurve> curves;

  /// 标尺最小值(默认 0)。
  final double minValue;

  /// 标尺最大值(未显式给出时取数据最大值)。
  final double maxValue;

  /// 同心格网层数(默认 5)。
  final int ticks;
  final ChatMermaidRadarGraticule graticule;
  final bool showLegend;

  @override
  ChatMermaidDiagramType get type => ChatMermaidDiagramType.radar;
}

class ChatMermaidRadarAxis {
  const ChatMermaidRadarAxis({
    required this.id,
    required this.label,
    required this.order,
  });

  final String id;
  final String label;
  final int order;
}

class ChatMermaidRadarCurve {
  const ChatMermaidRadarCurve({
    required this.id,
    required this.label,
    required this.values,
    required this.order,
  });

  final String id;
  final String label;

  /// 与 [ChatMermaidRadarDiagram.axes] 顺序对齐的取值。
  final List<double> values;
  final int order;
}

/// 看板图(kanban):由若干列(工作流阶段)构成,每列含若干任务卡;
/// 任务可携带 assigned/ticket/priority 等元数据。
class ChatMermaidKanbanDiagram extends ChatMermaidDiagram {
  const ChatMermaidKanbanDiagram({required this.columns});

  final List<ChatMermaidKanbanColumn> columns;

  @override
  ChatMermaidDiagramType get type => ChatMermaidDiagramType.kanban;
}

class ChatMermaidKanbanColumn {
  const ChatMermaidKanbanColumn({
    required this.id,
    required this.title,
    required this.items,
    required this.order,
  });

  final String id;
  final String title;
  final List<ChatMermaidKanbanItem> items;
  final int order;
}

class ChatMermaidKanbanItem {
  const ChatMermaidKanbanItem({
    required this.id,
    required this.text,
    required this.order,
    this.assigned,
    this.ticket,
    this.priority,
  });

  final String id;
  final String text;
  final int order;
  final String? assigned;
  final String? ticket;

  /// 优先级:'Very High' / 'High' / 'Low' / 'Very Low' 等(原样保留)。
  final String? priority;
}

/// 矩形树图(treemap-beta):以嵌套矩形展示层级数据,矩形面积正比于取值。
/// 叶节点带显式取值,分组(section)取值为其后代叶节点之和。
class ChatMermaidTreemapDiagram extends ChatMermaidDiagram {
  const ChatMermaidTreemapDiagram({required this.roots});

  final List<ChatMermaidTreemapNode> roots;

  @override
  ChatMermaidDiagramType get type => ChatMermaidDiagramType.treemap;
}

class ChatMermaidTreemapNode {
  const ChatMermaidTreemapNode({
    required this.label,
    required this.value,
    required this.isLeaf,
    required this.children,
    required this.order,
  });

  final String label;

  /// 叶节点为显式取值;分组为后代叶节点取值之和。
  final double value;
  final bool isLeaf;
  final List<ChatMermaidTreemapNode> children;
  final int order;
}

/// 块图(block-beta):作者完全掌控布局的网格式图。块按 columns 列从左到右
/// 排布、可跨多列(width),可用 space 占位;复合块(block:id..end)含子网格;
/// 块之间可用箭头连接。
class ChatMermaidBlockDiagram extends ChatMermaidDiagram {
  const ChatMermaidBlockDiagram({
    required this.columns,
    required this.items,
    required this.edges,
  });

  /// 顶层列数。
  final int columns;
  final List<ChatMermaidBlockItem> items;
  final List<ChatMermaidBlockEdge> edges;

  @override
  ChatMermaidDiagramType get type => ChatMermaidDiagramType.block;
}

class ChatMermaidBlockItem {
  const ChatMermaidBlockItem({
    required this.id,
    required this.label,
    required this.shape,
    required this.width,
    required this.isSpace,
    required this.isComposite,
    required this.compositeColumns,
    required this.children,
    required this.order,
  });

  final String id;
  final String label;
  final ChatMermaidNodeShape shape;

  /// 跨列数(列跨度),默认 1。
  final int width;

  /// 是否为占位空块。
  final bool isSpace;

  /// 是否为复合块(含子网格)。
  final bool isComposite;
  final int compositeColumns;
  final List<ChatMermaidBlockItem> children;
  final int order;
}

class ChatMermaidBlockEdge {
  const ChatMermaidBlockEdge({
    required this.sourceId,
    required this.targetId,
    required this.label,
    required this.order,
  });

  final String sourceId;
  final String targetId;
  final String? label;
  final int order;
}

/// 数据包图(packet):按位展示网络包结构,每个字段占据一段连续比特位。
class ChatMermaidPacketDiagram extends ChatMermaidDiagram {
  const ChatMermaidPacketDiagram({
    required this.title,
    required this.fields,
    required this.bitsPerRow,
  });

  final String title;
  final List<ChatMermaidPacketField> fields;

  /// 每行显示的比特数(布局用)。
  final int bitsPerRow;

  @override
  ChatMermaidDiagramType get type => ChatMermaidDiagramType.packet;
}

class ChatMermaidPacketField {
  const ChatMermaidPacketField({
    required this.start,
    required this.end,
    required this.label,
    required this.order,
  });

  /// 起始比特位(含)。
  final int start;

  /// 结束比特位(含)。
  final int end;
  final String label;
  final int order;

  int get bitCount => end - start + 1;
}

/// 需求图(requirementDiagram)的需求类型(SysML v1.6)。
enum ChatMermaidRequirementKind {
  requirement,
  functionalRequirement,
  interfaceRequirement,
  performanceRequirement,
  physicalRequirement,
  designConstraint,
}

/// 需求图:由需求节点、元素节点与它们之间的可追溯关系构成。
class ChatMermaidRequirementDiagram extends ChatMermaidDiagram {
  const ChatMermaidRequirementDiagram({
    required this.requirements,
    required this.elements,
    required this.relations,
  });

  final List<ChatMermaidRequirementNode> requirements;
  final List<ChatMermaidRequirementElement> elements;
  final List<ChatMermaidRequirementRelation> relations;

  @override
  ChatMermaidDiagramType get type => ChatMermaidDiagramType.requirement;
}

class ChatMermaidRequirementNode {
  const ChatMermaidRequirementNode({
    required this.name,
    required this.kind,
    required this.id,
    required this.text,
    required this.risk,
    required this.verifyMethod,
    required this.order,
  });

  final String name;
  final ChatMermaidRequirementKind kind;
  final String id;
  final String text;
  final String? risk;
  final String? verifyMethod;
  final int order;
}

class ChatMermaidRequirementElement {
  const ChatMermaidRequirementElement({
    required this.name,
    required this.elementType,
    required this.docref,
    required this.order,
  });

  final String name;
  final String? elementType;
  final String? docref;
  final int order;
}

class ChatMermaidRequirementRelation {
  const ChatMermaidRequirementRelation({
    required this.sourceName,
    required this.targetName,
    required this.type,
    required this.order,
  });

  final String sourceName;
  final String targetName;

  /// contains/copies/derives/satisfies/verifies/refines/traces。
  final String type;
  final int order;
}
