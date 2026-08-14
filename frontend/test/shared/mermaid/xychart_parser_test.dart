import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/mermaid/chat_mermaid_parser.dart';
import 'package:grix/shared/mermaid/chat_mermaid_model.dart';

void main() {
  const parser = ChatMermaidParser();

  test('parses xychart-beta with bar data', () {
    const source = '''
xychart-beta
    title "子类目7天销量"
    x-axis "类目" [美白牙贴, 牙膏, 漱口水, 其他, 牙线/牙间刷, 口腔喷雾, 牙刷/电动牙刷, 水牙线]
    y-axis "销量" 50000
    bar [45226, 21119, 18785, 12541, 4963, 4661, 4023, 471]
''';
    final result = parser.parse(source);
    expect(result.isSupported, isTrue);
    expect(result.diagram, isA<ChatMermaidXyChartDiagram>());
    final d = result.diagram as ChatMermaidXyChartDiagram;
    expect(d.title, '子类目7天销量');
    expect(d.xAxisTitle, '类目');
    expect(d.xAxisLabels.length, 8);
    expect(d.xAxisLabels[0], '美白牙贴');
    expect(d.yAxisTitle, '销量');
    expect(d.yAxisMax, 50000);
    expect(d.barSeries, hasLength(1));
    expect(d.barSeries.first, hasLength(8));
    expect(d.barSeries.first[0], 45226);
  });

  test('parses xychart-beta with percentage bars', () {
    const source = '''
xychart-beta
    title "类目占比"
    x-axis "类目" [美白牙贴, 牙膏, 漱口水]
    y-axis "占比%" 100
    bar [40, 30, 30]
''';
    final result = parser.parse(source);
    expect(result.isSupported, isTrue);
    final d = result.diagram as ChatMermaidXyChartDiagram;
    expect(d.title, '类目占比');
    expect(d.barSeries.first, [40, 30, 30]);
  });

  test('parses xychart-beta auto y-axis max', () {
    const source = '''
xychart-beta
    title "销售额"
    x-axis "月份" [1月, 2月, 3月]
    bar [100, 200, 150]
''';
    final result = parser.parse(source);
    expect(result.isSupported, isTrue);
    final d = result.diagram as ChatMermaidXyChartDiagram;
    expect(d.barSeries.first, [100, 200, 150]);
    expect(d.yAxisMax, greaterThan(200));
  });

  test('returns unsupported for empty xychart', () {
    const source = 'xychart-beta\n';
    final result = parser.parse(source);
    expect(result.isSupported, isFalse);
  });
}
