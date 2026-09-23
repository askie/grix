import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:grix/shared/mermaid/chat_mermaid_flowchart_layout.dart';
import 'package:grix/shared/mermaid/chat_mermaid_model.dart';
import 'package:grix/shared/mermaid/chat_mermaid_parser.dart';

/// 自由节点（不属于任何分组）与分组框的重叠消解。
///
/// 节点级去重只管节点对节点，分组消解只管分组对分组，一个自由节点压在别人
/// 分组框的 padding 上两边都不管：这张图里 OBS 就压在 C 框顶上。
const _source = r'''
flowchart LR
  subgraph A[静态生境适宜性 A]
    A1[气候 19 项 bioclim<br/>WorldClim/CHELSA 1km] --> SDM
    A2[地形 DEM 30m<br/>海拔/坡向/坡度/TWI] --> SDM
    A3[宿主林型 GLC_FCS30<br/>针叶/阔叶/混交 + 林龄 CLCD] --> SDM
    A4[土壤 SoilGrids 250m<br/>pH/有机碳/质地] --> SDM
    SDM[MaxEnt / 随机森林<br/>按物种训练] --> RA[每物种 30m 概率栅格]
  end
  subgraph B[季节窗 B]
    B1[出现记录的日期分布<br/>按物种×气候区] --> RB[每物种月份曲线]
  end
  subgraph C[动态出菇指数 C]
    C1[ERA5-Land / Open-Meteo<br/>降水滞后 3–10 天<br/>土壤温湿度 / 积温 / 无霜] --> RC[逐日格点指数]
  end
  RA --> P[P = A × B × C]
  RB --> P
  RC --> P
  OBS[(出现记录库<br/>GBIF/iNat/文献/App 采点)] --> SDM
  OBS --> B1
  P --> Q[查询 API / 分物种分月瓦片]
''';

void main() {
  test('不属于分组的节点不压在任何分组框上', () {
    final result = const ChatMermaidParser().parse(_source);
    final diagram = result.diagram! as ChatMermaidFlowchart;
    final layout = const ChatMermaidFlowchartLayoutEngine().layout(
      diagram: diagram,
      textStyle: const TextStyle(fontSize: 12),
      labelStyle: const TextStyle(fontSize: 12),
      textDirection: TextDirection.ltr,
    );
    for (final box in layout.subgraphRects) {
      final members = box.subgraph.nodeIds.toSet();
      for (final entry in layout.nodeRects.entries) {
        if (members.contains(entry.key)) {
          continue;
        }
        final overlap = entry.value.intersect(box.rect);
        expect(
          overlap.width > 0.5 && overlap.height > 0.5,
          isFalse,
          reason:
              '${entry.key} ${entry.value} 压在分组 ${box.subgraph.id} ${box.rect} 上',
        );
      }
    }
  });
}
