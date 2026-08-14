import 'dart:developer' as developer;

import 'chat_mermaid_model.dart';

class ChatMermaidParser {
  const ChatMermaidParser();

  static void _debugLog(String message) {
    developer.log('[MermaidParser] $message');
  }

  static final RegExp _flowchartHeaderPattern = RegExp(
    r'^(flowchart|graph)\s+(TD|TB|BT|LR|RL)\s*$',
    caseSensitive: false,
  );

  // 匹配 HTML 换行标签：<br>、<br/>、<br />（标签名大小写不敏感）。
  // 斜杠前后允许出现空白甚至换行，用于兼容流式输出把 <br/> 拆成
  // "<br/" 与 ">" 两行的情况；但 "<br" 之间不允许空格，
  // 因为 "< br>" 在 mermaid(HTML 渲染) 中不是合法标签，应作为普通文本。
  static final RegExp _htmlLineBreakPattern = RegExp(
    r'<br\s*/?\s*>',
    caseSensitive: false,
  );

  // 将节点/标签文本中的 <br> 系列标签及字面量 \n 转换为换行符，使渲染层按多行显示。
  // 逐行去除行首尾空白(对应 mermaid 经 HTML 渲染时的空白折叠)，
  // 但保留空行，从而忠实呈现连续 <br><br> 形成的空白行。
  static String _convertHtmlLineBreaks(String input) {
    final hasHtmlBr = input.contains('<');
    final hasEscapedNewline = input.contains(r'\n');
    if (!hasHtmlBr && !hasEscapedNewline) {
      return input;
    }
    var replaced = input;
    if (hasHtmlBr) {
      replaced = replaced.replaceAll(_htmlLineBreakPattern, '\n');
    }
    if (hasEscapedNewline) {
      replaced = replaced.replaceAll(r'\n', '\n');
    }
    if (!replaced.contains('\n')) {
      return replaced;
    }
    return replaced
        .split('\n')
        .map((line) => line.trim())
        .join('\n');
  }

  ChatMermaidParseResult parse(String source) {
    final normalized = source.replaceAll('\r\n', '\n').trim();
    if (normalized.isEmpty) {
      _debugLog('Source is empty');
      return ChatMermaidParseResult.unsupported(error: 'empty mermaid');
    }

    var lines = normalized
        .split('\n')
        .map((line) => line.trimRight())
        .where((line) => line.trim().isNotEmpty)
        .toList(growable: false);
    if (lines.isEmpty) {
      _debugLog('No non-empty lines after normalization');
      return ChatMermaidParseResult.unsupported(error: 'empty mermaid');
    }

    if (lines.first.trim() == '---') {
      final closeIdx = lines.indexWhere((l) => l.trim() == '---', 1);
      if (closeIdx > 0) {
        lines = lines.sublist(closeIdx + 1)
            .where((l) => l.trim().isNotEmpty)
            .toList(growable: false);
        if (lines.isEmpty) {
          return ChatMermaidParseResult.unsupported(
            error: 'only frontmatter, no diagram',
          );
        }
      }
    }

    final firstLine = lines.first.trim();

    if (firstLine == 'sequenceDiagram') {
      return _parseSequence(lines);
    }
    if (firstLine == 'stateDiagram-v2' || firstLine == 'stateDiagram') {
      return _parseState(lines);
    }
    if (firstLine == 'gantt') {
      return _parseGantt(lines);
    }
    if (firstLine == 'classDiagram') {
      return _parseClassDiagram(lines);
    }
    if (firstLine == 'erDiagram') {
      return _parseErDiagram(lines);
    }
    if (firstLine == 'requirementDiagram') {
      return _parseRequirement(lines);
    }
    if (firstLine.startsWith('pie')) {
      return _parsePie(lines);
    }
    if (firstLine == 'mindmap') {
      return _parseMindmap(normalized);
    }
    if (firstLine == 'journey') {
      return _parseJourney(lines);
    }
    if (firstLine.toLowerCase() == 'gitgraph') {
      return _parseGitGraph(lines);
    }
    if (firstLine == 'xychart-beta' ||
        firstLine == 'xychart' ||
        firstLine == 'xychart-beta horizontal' ||
        firstLine == 'xychart horizontal') {
      return _parseXyChart(lines);
    }
    if (firstLine == 'timeline') {
      return _parseTimeline(lines);
    }
    if (firstLine == 'quadrantChart') {
      return _parseQuadrant(lines);
    }
    if (firstLine == 'sankey-beta' || firstLine == 'sankey') {
      return _parseSankey(lines);
    }
    if (firstLine == 'radar-beta' || firstLine == 'radar') {
      return _parseRadar(lines);
    }
    if (firstLine == 'kanban') {
      return _parseKanban(lines);
    }
    if (firstLine == 'treemap-beta' || firstLine == 'treemap') {
      return _parseTreemap(lines);
    }
    if (firstLine == 'block-beta' || firstLine == 'block') {
      return _parseBlock(lines);
    }
    if (firstLine == 'packet-beta' || firstLine == 'packet') {
      return _parsePacket(lines);
    }

    final statements = _splitFlowchartStatements(normalized);
    final firstFlowchartIndex = statements.indexWhere(
      (statement) => _flowchartHeaderPattern.hasMatch(statement),
    );
    if (firstFlowchartIndex < 0) {
      return ChatMermaidParseResult.unsupported(
        error: 'unsupported mermaid header: ${statements.first}',
      );
    }
    final result = _parseFlowchart(
      statements.sublist(firstFlowchartIndex),
    );

    // Debug log for unsupported flowcharts
    if (!result.isSupported) {
      _debugLog('Flowchart parse failed: ${result.error}');
      _debugLog(
          'Source (${lines.length} lines):\n${lines.take(10).join("\n")}');
    }

    return result;
  }

  ChatMermaidParseResult _parseFlowchart(List<String> statements) {
    if (statements.isEmpty) {
      _debugLog('Flowchart: statements is empty');
      return ChatMermaidParseResult.unsupported(error: 'empty mermaid');
    }

    final headerMatch = _flowchartHeaderPattern.firstMatch(statements.first);
    if (headerMatch == null) {
      _debugLog('Flowchart: unsupported header: "${statements.first}"');
      return ChatMermaidParseResult.unsupported(
        error: 'unsupported mermaid header: ${statements.first}',
      );
    }

    final direction = _parseDirection(headerMatch.group(2)!);
    final builder = _FlowchartBuilder(direction: direction);
    final activeSubgraphIds = <String>[];

    for (final statement in statements.skip(1)) {
      if (_isComment(statement)) {
        continue;
      }
      if (_flowchartHeaderPattern.hasMatch(statement)) {
        // Stop at the next flowchart declaration so multi-example snippets
        // still render a valid first diagram instead of failing completely.
        break;
      }
      final subgraph = _tryParseFlowSubgraph(
        statement,
        builder.nextSubgraphOrder(),
        activeSubgraphIds.length,
      );
      if (subgraph != null) {
        builder.addSubgraph(subgraph);
        activeSubgraphIds.add(subgraph.id);
        continue;
      }
      if (statement == 'end') {
        if (activeSubgraphIds.isEmpty) {
          _debugLog('Flowchart: "end" without active subgraph');
          return ChatMermaidParseResult.unsupported(
            error: 'flowchart end without active subgraph',
          );
        }
        activeSubgraphIds.removeLast();
        continue;
      }
      if (_isSkippableFlowchartDirective(statement)) {
        continue;
      }
      if (_tryParseFlowchartStyleDirective(statement, builder)) {
        continue;
      }

      final parser = _FlowchartStatementParser(statement);
      final firstNode = parser.parseNode();
      if (firstNode == null) {
        _debugLog('Flowchart: invalid node statement: "$statement"');
        return ChatMermaidParseResult.unsupported(
          error: 'invalid mermaid node statement: $statement',
        );
      }

      // Collect source nodes connected by & operator (e.g. A & B --> C)
      final sourceNodes = <_ParsedNode>[firstNode];
      while (parser.consumeAnd()) {
        final additionalNode = parser.parseNode();
        if (additionalNode == null) {
          _debugLog('Flowchart: invalid node after &: "$statement"');
          return ChatMermaidParseResult.unsupported(
            error: 'invalid mermaid node after &: $statement',
          );
        }
        sourceNodes.add(additionalNode);
      }

      final sourceReferences = <_ResolvedFlowReference>[];
      for (final node in sourceNodes) {
        final ref = builder.resolveReference(node);
        builder.registerReferenceInActiveSubgraphs(ref, activeSubgraphIds);
        sourceReferences.add(ref);
      }

      // 当前一段链路连接的"上游引用集合";支持源侧与目标侧的 & 链。
      // 例如 A & B --> C & D 表示 {A,B} 各自连到 {C,D}。
      var currentReferences = sourceReferences;
      var sawEdge = false;

      while (!parser.isDone) {
        final edge = parser.parseEdge();
        if (edge == null) {
          _debugLog('Flowchart: invalid edge in statement: "$statement"');
          return ChatMermaidParseResult.unsupported(
            error: 'invalid mermaid edge statement: $statement',
          );
        }

        // 解析一个或多个以 & 连接的目标节点。
        final targetReferences = <_ResolvedFlowReference>[];
        final firstTarget = parser.parseNode();
        if (firstTarget == null) {
          _debugLog(
              'Flowchart: invalid target node in statement: "$statement"');
          return ChatMermaidParseResult.unsupported(
            error: 'invalid mermaid target node: $statement',
          );
        }
        final targetNodes = <_ParsedNode>[firstTarget];
        while (parser.consumeAnd()) {
          final additionalTarget = parser.parseNode();
          if (additionalTarget == null) {
            _debugLog(
                'Flowchart: invalid target node after &: "$statement"');
            return ChatMermaidParseResult.unsupported(
              error: 'invalid mermaid target node after &: $statement',
            );
          }
          targetNodes.add(additionalTarget);
        }
        for (final node in targetNodes) {
          final ref = builder.resolveReference(node);
          builder.registerReferenceInActiveSubgraphs(ref, activeSubgraphIds);
          targetReferences.add(ref);
        }

        // 上游集合中的每个节点都连接到本段每个目标节点。
        for (final sourceRef in currentReferences) {
          for (final targetRef in targetReferences) {
            builder.addEdge(
              sourceId: sourceRef.referenceId,
              targetId: targetRef.referenceId,
              label: edge.label,
              style: edge.style,
            );
          }
        }

        currentReferences = targetReferences;
        sawEdge = true;
      }

      if (!sawEdge && parser.hasTrailingGarbage) {
        _debugLog(
            'Flowchart: unsupported statement with trailing garbage: "$statement"');
        return ChatMermaidParseResult.unsupported(
          error: 'unsupported mermaid statement: $statement',
        );
      }
    }

    if (builder.nodes.isEmpty) {
      _debugLog('Flowchart: no nodes found in diagram');
      return ChatMermaidParseResult.unsupported(
        error: 'mermaid flowchart has no nodes',
      );
    }
    if (activeSubgraphIds.isNotEmpty) {
      _debugLog(
          'Flowchart: subgraph not closed, active: ${activeSubgraphIds.join(", ")}');
      return ChatMermaidParseResult.unsupported(
        error: 'flowchart subgraph not closed',
      );
    }

    return ChatMermaidParseResult.supported(diagram: builder.build());
  }

  ChatMermaidParseResult _parseSequence(List<String> lines) {
    final builder = _SequenceBuilder();
    // 统一的块栈:非空 kind 表示分组(alt/loop/...),null 表示包裹块(box/rect)。
    // end 按栈顶弹出,仅分组才发出 GroupEnd,保证 start/end 事件配平。
    final blockStack = <ChatMermaidSequenceGroupKind?>[];

    for (final rawLine in lines.skip(1)) {
      final line = rawLine.trim();
      if (line.isEmpty || _isComment(line)) {
        continue;
      }

      // box / rect 包裹块:消费起始行并入栈(无事件),其内容照常解析。
      if (line == 'box' ||
          line.startsWith('box ') ||
          line == 'rect' ||
          line.startsWith('rect ')) {
        blockStack.add(null);
        continue;
      }

      // create (participant|actor) Name / create Name:声明参与者。
      if (line.startsWith('create ')) {
        final rest = line.substring('create '.length).trim();
        final created = _tryParseSequenceParticipant(rest);
        if (created != null) {
          builder.upsertParticipant(created);
        } else if (rest.isNotEmpty) {
          builder.ensureParticipant(rest.split(RegExp(r'\s+')).first);
        }
        continue;
      }
      // destroy Name:消费并忽略(生命周期标记不在解析层渲染)。
      if (line.startsWith('destroy ')) {
        final id = line.substring('destroy '.length).trim();
        if (id.isNotEmpty) {
          builder.ensureParticipant(id);
        }
        continue;
      }

      // activate / deactivate Name:消费并忽略(激活条不在解析层渲染)。
      final activation =
          RegExp(r'^(?:activate|deactivate)\s+(.+)$').firstMatch(line);
      if (activation != null) {
        builder.ensureParticipant(activation.group(1)!.trim());
        continue;
      }

      final participant = _tryParseSequenceParticipant(line);
      if (participant != null) {
        builder.upsertParticipant(participant);
        continue;
      }

      final note = _tryParseSequenceNote(line);
      if (note != null) {
        for (final targetId in note.targetIds) {
          builder.ensureParticipant(targetId);
        }
        builder.addEvent(note);
        continue;
      }

      final groupStart = _tryParseSequenceGroupStart(line);
      if (groupStart != null) {
        blockStack.add(groupStart.kind);
        builder.addEvent(groupStart);
        continue;
      }

      final groupDivider = _tryParseSequenceGroupDivider(line);
      if (groupDivider != null) {
        if (!blockStack.any((kind) => kind != null)) {
          return ChatMermaidParseResult.unsupported(
            error: 'sequence divider without active group: $line',
          );
        }
        builder.addEvent(groupDivider);
        continue;
      }

      if (line == 'end') {
        if (blockStack.isEmpty) {
          return ChatMermaidParseResult.unsupported(
            error: 'sequence end without active block',
          );
        }
        final kind = blockStack.removeLast();
        if (kind != null) {
          builder.addEvent(
              ChatMermaidSequenceGroupEnd(order: builder.nextOrder()));
        }
        continue;
      }

      if (line == 'autonumber') {
        continue;
      }

      final message = _tryParseSequenceMessage(line);
      if (message != null) {
        builder.ensureParticipant(message.fromId);
        builder.ensureParticipant(message.toId);
        builder.addEvent(message);
        continue;
      }

      return ChatMermaidParseResult.unsupported(
        error: 'unsupported sequence statement: $line',
      );
    }

    if (blockStack.any((kind) => kind != null)) {
      return ChatMermaidParseResult.unsupported(
        error: 'sequence group not closed',
      );
    }
    if (builder.participants.isEmpty) {
      return ChatMermaidParseResult.unsupported(
        error: 'sequence diagram has no participants',
      );
    }

    return ChatMermaidParseResult.supported(diagram: builder.build());
  }

  ChatMermaidParseResult _parseState(List<String> lines) {
    final builder = _StateBuilder();

    for (final rawLine in lines.skip(1)) {
      final line = rawLine.trim();
      if (line.isEmpty || _isComment(line)) {
        continue;
      }
      if (_isSkippableStateDirective(line)) {
        continue;
      }

      final declaration = _tryParseStateDeclaration(line);
      if (declaration != null) {
        builder.upsertNode(declaration);
        continue;
      }

      final transition = _tryParseStateTransition(line);
      if (transition != null) {
        builder.addTransition(transition);
        continue;
      }

      return ChatMermaidParseResult.unsupported(
        error: 'unsupported state statement: $line',
      );
    }

    if (builder.nodes.isEmpty) {
      return ChatMermaidParseResult.unsupported(
        error: 'state diagram has no states',
      );
    }

    return ChatMermaidParseResult.supported(diagram: builder.build());
  }

  ChatMermaidParseResult _parseGantt(List<String> lines) {
    final sections = <_MutableGanttSection>[];
    _MutableGanttSection? currentSection;
    var title = '';
    var axisFormat = '%m-%d';

    for (final rawLine in lines.skip(1)) {
      final line = rawLine.trim();
      if (line.isEmpty || _isComment(line)) {
        continue;
      }

      if (line.startsWith('title ')) {
        title = line.substring('title '.length).trim();
        continue;
      }
      if (line.startsWith('dateFormat ')) {
        // Accept any dateFormat; we always parse dates as YYYY-MM-DD
        // internally which is the most common format in practice.
        continue;
      }
      if (line.startsWith('axisFormat ')) {
        final value = line.substring('axisFormat '.length).trim();
        if (value.isNotEmpty) {
          axisFormat = value;
        }
        continue;
      }
      if (line.startsWith('section ')) {
        final sectionTitle = line.substring('section '.length).trim();
        if (sectionTitle.isEmpty) {
          return ChatMermaidParseResult.unsupported(
            error: 'gantt section title is empty',
          );
        }
        currentSection = _MutableGanttSection(title: sectionTitle);
        sections.add(currentSection);
        continue;
      }
      // Silently skip known directives that don't affect rendering.
      final directiveWord = line.split(RegExp(r'\s')).first;
      if (_ignoredGanttDirectives.contains(directiveWord)) {
        continue;
      }

      // Auto-create a default section for tasks that appear before any
      // explicit section declaration (Mermaid allows sectionless tasks).
      if (currentSection == null) {
        currentSection = _MutableGanttSection(title: '');
        sections.add(currentSection);
      }

      final taskSpec = _tryParseGanttTask(line);
      if (taskSpec == null) {
        return ChatMermaidParseResult.unsupported(
          error: 'unsupported gantt task: $line',
        );
      }
      currentSection.tasks.add(taskSpec);
    }

    if (sections.isEmpty) {
      return ChatMermaidParseResult.unsupported(
        error: 'gantt has no sections',
      );
    }

    final taskById = <String, ChatMermaidGanttTask>{};
    final builtSections = <ChatMermaidGanttSection>[];
    DateTime? rangeStart;
    DateTime? rangeEndExclusive;
    var taskOrder = 0;

    for (var index = 0; index < sections.length; index += 1) {
      final section = sections[index];
      final tasks = <ChatMermaidGanttTask>[];
      for (final taskSpec in section.tasks) {
        if (taskSpec.id != null && taskById.containsKey(taskSpec.id)) {
          return ChatMermaidParseResult.unsupported(
            error: 'duplicate gantt task id: ${taskSpec.id}',
          );
        }
        final startDate = _resolveGanttTaskStart(taskSpec, taskById);
        if (startDate == null) {
          return ChatMermaidParseResult.unsupported(
            error: 'unresolved gantt dependency: ${taskSpec.startToken}',
          );
        }
        final task = ChatMermaidGanttTask(
          id: taskSpec.id,
          label: taskSpec.label,
          startDate: startDate,
          durationDays: taskSpec.durationDays,
          order: taskOrder++,
        );
        if (task.id != null) {
          taskById[task.id!] = task;
        }
        rangeStart = rangeStart == null || startDate.isBefore(rangeStart)
            ? startDate
            : rangeStart;
        rangeEndExclusive = rangeEndExclusive == null ||
                task.endDateExclusive.isAfter(rangeEndExclusive)
            ? task.endDateExclusive
            : rangeEndExclusive;
        tasks.add(task);
      }
      builtSections.add(
        ChatMermaidGanttSection(
          title: section.title,
          order: index,
          tasks: List.unmodifiable(tasks),
        ),
      );
    }

    if (rangeStart == null || rangeEndExclusive == null) {
      return ChatMermaidParseResult.unsupported(
        error: 'gantt has no tasks',
      );
    }

    return ChatMermaidParseResult.supported(
      diagram: ChatMermaidGanttDiagram(
        title: title,
        axisFormat: axisFormat,
        rangeStart: rangeStart,
        rangeEndExclusive: rangeEndExclusive,
        sections: List.unmodifiable(builtSections),
      ),
    );
  }

  // -------------------------------------------------------------------------
  // Class Diagram Parser
  // -------------------------------------------------------------------------

  static final RegExp _classRelationPattern = RegExp(
    r'^([A-Za-z0-9_]+)\s+'
    r'(<\|--|<\|\.\.|\*--|o--|-->|\.\.>|\.\.\|>|--)'
    r'\s+([A-Za-z0-9_]+)'
    r'(?:\s*:\s*(.+))?$',
  );

  ChatMermaidParseResult _parseClassDiagram(List<String> lines) {
    final classes = <String, _MutableClassItem>{};
    final relations = <ChatMermaidClassRelation>[];
    var relationOrder = 0;
    _MutableClassItem? currentClassBlock;

    for (final rawLine in lines.skip(1)) {
      final line = rawLine.trim();
      if (line.isEmpty || _isComment(line)) {
        continue;
      }

      // End of class block
      if (line == '}') {
        currentClassBlock = null;
        continue;
      }

      // Inside class block — member line
      if (currentClassBlock != null) {
        currentClassBlock.members.add(_convertClassGenerics(line));
        continue;
      }

      // class Name { ... block start(支持泛型 class Box~T~ { )
      final classBlockMatch =
          RegExp(r'^class\s+([A-Za-z0-9_]+)(?:~([^~]*)~)?\s*\{?\s*$')
              .firstMatch(line);
      if (classBlockMatch != null) {
        final id = classBlockMatch.group(1)!;
        final generic = classBlockMatch.group(2);
        final item = classes.putIfAbsent(
          id,
          () => _MutableClassItem(id: id),
        );
        if (generic != null && generic.isNotEmpty) {
          item.label = '$id<$generic>';
        }
        if (line.endsWith('{')) {
          currentClassBlock = item;
        }
        continue;
      }

      // Inline member: ClassName : +member(类名支持泛型)
      final memberMatch =
          RegExp(r'^([A-Za-z0-9_]+)(?:~([^~]*)~)?\s*:\s*(.+)$').firstMatch(line);
      if (memberMatch != null) {
        final id = memberMatch.group(1)!;
        final generic = memberMatch.group(2);
        final member = _convertClassGenerics(memberMatch.group(3)!.trim());
        final item = classes.putIfAbsent(
          id,
          () => _MutableClassItem(id: id),
        );
        if (generic != null && generic.isNotEmpty) {
          item.label = '$id<$generic>';
        }
        item.members.add(member);
        continue;
      }

      // Relationship
      final relMatch = _classRelationPattern.firstMatch(line);
      if (relMatch != null) {
        final sourceId = relMatch.group(1)!;
        final op = relMatch.group(2)!;
        final targetId = relMatch.group(3)!;
        final label = relMatch.group(4)?.trim();
        classes.putIfAbsent(sourceId, () => _MutableClassItem(id: sourceId));
        classes.putIfAbsent(targetId, () => _MutableClassItem(id: targetId));
        relations.add(ChatMermaidClassRelation(
          sourceId: sourceId,
          targetId: targetId,
          relationType: _parseClassRelationType(op),
          label: label,
          order: relationOrder++,
        ));
        continue;
      }

      // Skip annotation lines like <<interface>>
      if (line.startsWith('<<') || line.startsWith('class ')) {
        continue;
      }
    }

    if (classes.isEmpty) {
      return ChatMermaidParseResult.unsupported(
        error: 'class diagram has no classes',
      );
    }

    var classOrder = 0;
    final builtClasses = classes.values
        .map((c) => ChatMermaidClassItem(
              id: c.id,
              label: c.label,
              members: List.unmodifiable(c.members),
              order: classOrder++,
            ))
        .toList(growable: false);

    return ChatMermaidParseResult.supported(
      diagram: ChatMermaidClassDiagram(
        classes: List.unmodifiable(builtClasses),
        relations: List.unmodifiable(relations),
      ),
    );
  }

  // 将类图中的泛型记法 ~T~ 转换为 <T> 以便显示(对齐 mermaid)。
  String _convertClassGenerics(String input) {
    if (!input.contains('~')) {
      return input;
    }
    return input.replaceAllMapped(
      RegExp(r'~([^~]*)~'),
      (match) => '<${match.group(1)}>',
    );
  }

  ChatMermaidClassRelationType _parseClassRelationType(String op) {
    switch (op) {
      case '<|--':
        return ChatMermaidClassRelationType.inheritance;
      case '<|..':
        return ChatMermaidClassRelationType.realization;
      case '*--':
        return ChatMermaidClassRelationType.composition;
      case 'o--':
        return ChatMermaidClassRelationType.aggregation;
      case '-->':
        return ChatMermaidClassRelationType.association;
      case '..>':
        return ChatMermaidClassRelationType.dependency;
      case '..|>':
        return ChatMermaidClassRelationType.realization;
      default:
        return ChatMermaidClassRelationType.association;
    }
  }

  // -------------------------------------------------------------------------
  // ER Diagram Parser
  // -------------------------------------------------------------------------

  static final RegExp _erRelationPattern = RegExp(
    r'^([A-Za-z0-9_-]+)\s+'
    r'([|o}]{1,2})(?:--|\.\.)([|o{]{1,2})'
    r'\s+([A-Za-z0-9_-]+)\s*:\s*(.+)$',
  );

  ChatMermaidParseResult _parseErDiagram(List<String> lines) {
    final entities = <String, int>{};
    final relations = <ChatMermaidErRelation>[];
    var entityOrder = 0;
    var relationOrder = 0;
    var inAttributeBlock = false;

    for (final rawLine in lines.skip(1)) {
      final line = rawLine.trim();
      if (line.isEmpty || _isComment(line)) {
        continue;
      }

      // 实体属性块结束
      if (line == '}') {
        inAttributeBlock = false;
        continue;
      }
      // 属性块内的属性行(type name [PK/FK] 等):跳过(模型不含属性)。
      if (inAttributeBlock) {
        continue;
      }

      final match = _erRelationPattern.firstMatch(line);
      if (match != null) {
        final sourceId = match.group(1)!;
        final leftSymbol = match.group(2)!;
        final rightSymbol = match.group(3)!;
        final targetId = match.group(4)!;
        final label = match.group(5)!.trim();

        entities.putIfAbsent(sourceId, () => entityOrder++);
        entities.putIfAbsent(targetId, () => entityOrder++);

        relations.add(ChatMermaidErRelation(
          sourceId: sourceId,
          targetId: targetId,
          sourceCardinality: _parseErCardinality(leftSymbol),
          targetCardinality: _parseErCardinality(rightSymbol),
          label: label,
          order: relationOrder++,
        ));
        continue;
      }

      // 实体属性块起始:NAME {  → 注册实体并进入属性块。
      final entityBlock =
          RegExp(r'^([A-Za-z0-9_-]+)\s*\{$').firstMatch(line);
      if (entityBlock != null) {
        entities.putIfAbsent(entityBlock.group(1)!, () => entityOrder++);
        inAttributeBlock = true;
        continue;
      }

      // 独立实体声明(无属性块):仅一个实体名。
      final bareEntity = RegExp(r'^([A-Za-z0-9_-]+)$').firstMatch(line);
      if (bareEntity != null) {
        entities.putIfAbsent(bareEntity.group(1)!, () => entityOrder++);
        continue;
      }
    }

    if (entities.isEmpty) {
      return ChatMermaidParseResult.unsupported(
        error: 'er diagram has no entities',
      );
    }

    final builtEntities = entities.entries
        .map((e) => ChatMermaidErEntity(id: e.key, order: e.value))
        .toList(growable: false)
      ..sort((a, b) => a.order.compareTo(b.order));

    return ChatMermaidParseResult.supported(
      diagram: ChatMermaidErDiagram(
        entities: List.unmodifiable(builtEntities),
        relations: List.unmodifiable(relations),
      ),
    );
  }

  ChatMermaidErCardinality _parseErCardinality(String symbol) {
    switch (symbol) {
      case '||':
        return ChatMermaidErCardinality.exactlyOne;
      case 'o|':
      case '|o':
        return ChatMermaidErCardinality.zeroOrOne;
      case '}|':
      case '|{':
        return ChatMermaidErCardinality.oneOrMore;
      case '}o':
      case 'o{':
        return ChatMermaidErCardinality.zeroOrMore;
      default:
        return ChatMermaidErCardinality.exactlyOne;
    }
  }

  // -------------------------------------------------------------------------
  // Requirement Diagram Parser
  // -------------------------------------------------------------------------

  static const _reqKinds = <String, ChatMermaidRequirementKind>{
    'requirement': ChatMermaidRequirementKind.requirement,
    'functionalrequirement': ChatMermaidRequirementKind.functionalRequirement,
    'interfacerequirement': ChatMermaidRequirementKind.interfaceRequirement,
    'performancerequirement': ChatMermaidRequirementKind.performanceRequirement,
    'physicalrequirement': ChatMermaidRequirementKind.physicalRequirement,
    'designconstraint': ChatMermaidRequirementKind.designConstraint,
  };

  static const _reqRelTypes = <String>{
    'contains', 'copies', 'derives', 'satisfies', 'verifies', 'refines', 'traces',
  };

  ChatMermaidParseResult _parseRequirement(List<String> lines) {
    final reqs = <ChatMermaidRequirementNode>[];
    final elems = <ChatMermaidRequirementElement>[];
    final rels = <ChatMermaidRequirementRelation>[];
    var reqO = 0, elemO = 0, relO = 0;
    String? blockType; // 'req' | 'elem'
    String blockName = '';
    ChatMermaidRequirementKind blockKind = ChatMermaidRequirementKind.requirement;
    String bId = '', bText = '', bRisk = '', bVerify = '';
    String bElemType = '', bDocref = '';

    void flushBlock() {
      if (blockType == 'req') {
        reqs.add(ChatMermaidRequirementNode(
          name: blockName, kind: blockKind, id: bId, text: bText,
          risk: bRisk.isEmpty ? null : bRisk,
          verifyMethod: bVerify.isEmpty ? null : bVerify, order: reqO++,
        ));
      } else if (blockType == 'elem') {
        elems.add(ChatMermaidRequirementElement(
          name: blockName,
          elementType: bElemType.isEmpty ? null : bElemType,
          docref: bDocref.isEmpty ? null : bDocref, order: elemO++,
        ));
      }
      blockType = null;
      bId = bText = bRisk = bVerify = bElemType = bDocref = '';
    }

    for (final rawLine in lines.skip(1)) {
      final line = rawLine.trim();
      if (line.isEmpty || _isComment(line)) continue;
      if (line == '}') { flushBlock(); continue; }

      if (blockType != null) {
        final colon = line.indexOf(':');
        if (colon > 0) {
          final key = line.substring(0, colon).trim().toLowerCase();
          final val = _stripReqQuotes(line.substring(colon + 1).trim());
          if (blockType == 'req') {
            if (key == 'id') bId = val;
            else if (key == 'text') bText = val;
            else if (key == 'risk') bRisk = val;
            else if (key == 'verifymethod') bVerify = val;
          } else {
            if (key == 'type') bElemType = val;
            else if (key == 'docref') bDocref = val;
          }
        }
        continue;
      }

      final lower = line.toLowerCase();
      if (lower.startsWith('direction ') || lower.startsWith('style ') ||
          lower.startsWith('classdef ') || lower.startsWith('class ')) continue;

      // 关系: A - type -> B  或 B <- type - A
      final rel = _parseReqRelation(line);
      if (rel != null) { rels.add(ChatMermaidRequirementRelation(sourceName: rel.$1, targetName: rel.$3, type: rel.$2, order: relO++)); continue; }

      // 块起始: keyword name[:::class] {
      final opener = RegExp(r'^(\w+)\s+(.+?)\s*(?::::\S+)?\s*\{?\s*$').firstMatch(line);
      if (opener != null) {
        final kw = opener.group(1)!.toLowerCase();
        final name = _stripReqQuotes(opener.group(2)!.replaceFirst(RegExp(r'\s*:::\S+'), '').trim());
        if (kw == 'element') {
          blockType = 'elem'; blockName = name; continue;
        }
        final kind = _reqKinds[kw];
        if (kind != null) {
          blockType = 'req'; blockName = name; blockKind = kind; continue;
        }
      }
    }
    flushBlock();

    if (reqs.isEmpty && elems.isEmpty) {
      return ChatMermaidParseResult.unsupported(error: 'requirement diagram has no requirements or elements');
    }
    return ChatMermaidParseResult.supported(diagram: ChatMermaidRequirementDiagram(
      requirements: List.unmodifiable(reqs), elements: List.unmodifiable(elems), relations: List.unmodifiable(rels),
    ));
  }

  (String, String, String)? _parseReqRelation(String line) {
    final fwd = RegExp(r'^(.+?)\s*-\s*(\w+)\s*->\s*(.+)$').firstMatch(line);
    if (fwd != null && _reqRelTypes.contains(fwd.group(2))) {
      return (_stripReqQuotes(fwd.group(1)!.trim()), fwd.group(2)!, _stripReqQuotes(fwd.group(3)!.trim()));
    }
    final bwd = RegExp(r'^(.+?)\s*<-\s*(\w+)\s*-\s*(.+)$').firstMatch(line);
    if (bwd != null && _reqRelTypes.contains(bwd.group(2))) {
      return (_stripReqQuotes(bwd.group(3)!.trim()), bwd.group(2)!, _stripReqQuotes(bwd.group(1)!.trim()));
    }
    return null;
  }

  String _stripReqQuotes(String s) {
    final t = s.trim();
    if (t.length >= 2 && ((t.startsWith('"') && t.endsWith('"')) || (t.startsWith("'") && t.endsWith("'")))) {
      return t.substring(1, t.length - 1);
    }
    return t;
  }

  // -------------------------------------------------------------------------
  // Pie Diagram Parser
  // -------------------------------------------------------------------------

  ChatMermaidParseResult _parsePie(List<String> lines) {
    var title = '';
    final slices = <ChatMermaidPieSlice>[];
    var sliceOrder = 0;

    final firstLine = lines.first.trim();
    // Handle "pie title My Title" or "pie showData" or just "pie"
    if (firstLine.startsWith('pie title ')) {
      title = firstLine.substring('pie title '.length).trim();
    } else if (firstLine.contains('title ')) {
      final titleIdx = firstLine.indexOf('title ');
      title = firstLine.substring(titleIdx + 'title '.length).trim();
    }

    for (final rawLine in lines.skip(1)) {
      final line = rawLine.trim();
      if (line.isEmpty || _isComment(line)) {
        continue;
      }

      // title on separate line
      if (line.startsWith('title ')) {
        title = line.substring('title '.length).trim();
        continue;
      }

      // Skip showData
      if (line == 'showData') {
        continue;
      }

      // "Label" : value
      final match = RegExp(r'^"([^"]+)"\s*:\s*([0-9]+(?:\.[0-9]+)?)\s*$')
          .firstMatch(line);
      if (match != null) {
        final label = match.group(1)!;
        final value = double.tryParse(match.group(2)!);
        if (value != null && value > 0) {
          slices.add(ChatMermaidPieSlice(
            label: label,
            value: value,
            order: sliceOrder++,
          ));
        }
        continue;
      }
    }

    if (slices.isEmpty) {
      return ChatMermaidParseResult.unsupported(
        error: 'pie chart has no slices',
      );
    }

    return ChatMermaidParseResult.supported(
      diagram: ChatMermaidPieDiagram(
        title: title,
        slices: List.unmodifiable(slices),
      ),
    );
  }

  // -------------------------------------------------------------------------
  // Mindmap Parser
  // -------------------------------------------------------------------------

  ChatMermaidParseResult _parseMindmap(String source) {
    final rawLines = source.replaceAll('\r\n', '\n').split('\n');
    if (rawLines.isEmpty || rawLines.first.trim() != 'mindmap') {
      return ChatMermaidParseResult.unsupported(
        error: 'not a mindmap',
      );
    }

    // Parse indentation-based tree
    final contentLines = <_MindmapLine>[];
    for (final line in rawLines.skip(1)) {
      if (line.trim().isEmpty || _isComment(line.trim())) {
        continue;
      }
      final indent = line.length - line.trimLeft().length;
      final text = line.trim();
      contentLines.add(_MindmapLine(indent: indent, text: text));
    }

    if (contentLines.isEmpty) {
      return ChatMermaidParseResult.unsupported(
        error: 'mindmap has no nodes',
      );
    }

    var order = 0;
    ChatMermaidMindmapNode buildNode(int index) {
      final currentLine = contentLines[index];
      final parsed = _parseMindmapNodeText(currentLine.text);
      final children = <ChatMermaidMindmapNode>[];

      var childIndex = index + 1;
      while (childIndex < contentLines.length &&
          contentLines[childIndex].indent > currentLine.indent) {
        // Only direct children (first level deeper)
        if (childIndex == index + 1 ||
            contentLines[childIndex].indent <=
                contentLines[childIndex - 1].indent ||
            contentLines[childIndex].indent == contentLines[index + 1].indent) {
          if (contentLines[childIndex].indent > currentLine.indent) {
            final isDirectChild = contentLines[childIndex].indent ==
                _findChildIndent(contentLines, index);
            if (isDirectChild) {
              children.add(buildNode(childIndex));
            }
          }
        }
        childIndex++;
      }

      return ChatMermaidMindmapNode(
        label: parsed.label,
        shape: parsed.shape,
        children: List.unmodifiable(children),
        order: order++,
      );
    }

    final root = buildNode(0);
    return ChatMermaidParseResult.supported(
      diagram: ChatMermaidMindmapDiagram(root: root),
    );
  }

  int _findChildIndent(List<_MindmapLine> lines, int parentIndex) {
    final parentIndent = lines[parentIndex].indent;
    for (var i = parentIndex + 1; i < lines.length; i++) {
      if (lines[i].indent > parentIndent) {
        return lines[i].indent;
      }
      if (lines[i].indent <= parentIndent) {
        break;
      }
    }
    return parentIndent + 2;
  }

  _ParsedMindmapNode _parseMindmapNodeText(String text) {
    // Strip 'root' keyword if present
    var nodeText = text.trim();
    if (nodeText.startsWith('root')) {
      nodeText = nodeText.substring(4).trim();
    }

    // ((text)) → circle
    if (nodeText.startsWith('((') && nodeText.endsWith('))')) {
      return _ParsedMindmapNode(
        label: nodeText.substring(2, nodeText.length - 2).trim(),
        shape: ChatMermaidNodeShape.circle,
      );
    }
    // (text) → rounded
    if (nodeText.startsWith('(') &&
        nodeText.endsWith(')') &&
        !nodeText.startsWith('((')) {
      return _ParsedMindmapNode(
        label: nodeText.substring(1, nodeText.length - 1).trim(),
        shape: ChatMermaidNodeShape.rounded,
      );
    }
    // [text] → rectangle
    if (nodeText.startsWith('[') && nodeText.endsWith(']')) {
      return _ParsedMindmapNode(
        label: nodeText.substring(1, nodeText.length - 1).trim(),
        shape: ChatMermaidNodeShape.rectangle,
      );
    }
    // {{text}} → hexagon
    if (nodeText.startsWith('{{') && nodeText.endsWith('}}')) {
      return _ParsedMindmapNode(
        label: nodeText.substring(2, nodeText.length - 2).trim(),
        shape: ChatMermaidNodeShape.hexagon,
      );
    }
    // default → rounded
    return _ParsedMindmapNode(
      label: text,
      shape: ChatMermaidNodeShape.rounded,
    );
  }

  // -------------------------------------------------------------------------
  // Journey Diagram Parser
  // -------------------------------------------------------------------------

  ChatMermaidParseResult _parseJourney(List<String> lines) {
    var title = '';
    final sections = <_MutableJourneySection>[];
    _MutableJourneySection? currentSection;
    var sectionOrder = 0;
    var taskOrder = 0;

    for (final rawLine in lines.skip(1)) {
      final line = rawLine.trim();
      if (line.isEmpty || _isComment(line)) {
        continue;
      }

      if (line.startsWith('title ')) {
        title = line.substring('title '.length).trim();
        continue;
      }

      if (line.startsWith('section ')) {
        final sectionTitle = line.substring('section '.length).trim();
        currentSection = _MutableJourneySection(
          title: sectionTitle,
          order: sectionOrder++,
        );
        sections.add(currentSection);
        continue;
      }

      // Task: label: score: actor1, actor2
      final taskMatch = RegExp(r'^(.+):\s*(\d)\s*:\s*(.+)$').firstMatch(line);
      if (taskMatch != null) {
        if (currentSection == null) {
          currentSection = _MutableJourneySection(
            title: '',
            order: sectionOrder++,
          );
          sections.add(currentSection);
        }
        final label = taskMatch.group(1)!.trim();
        final score = int.tryParse(taskMatch.group(2)!) ?? 3;
        final actors = taskMatch
            .group(3)!
            .split(',')
            .map((a) => a.trim())
            .where((a) => a.isNotEmpty)
            .toList(growable: false);
        currentSection.tasks.add(ChatMermaidJourneyTask(
          label: label,
          score: score.clamp(1, 5),
          actors: actors,
          order: taskOrder++,
        ));
        continue;
      }
    }

    if (sections.isEmpty) {
      return ChatMermaidParseResult.unsupported(
        error: 'journey has no sections',
      );
    }

    final builtSections = sections
        .map((s) => ChatMermaidJourneySection(
              title: s.title,
              tasks: List.unmodifiable(s.tasks),
              order: s.order,
            ))
        .toList(growable: false);

    return ChatMermaidParseResult.supported(
      diagram: ChatMermaidJourneyDiagram(
        title: title,
        sections: List.unmodifiable(builtSections),
      ),
    );
  }

  // -------------------------------------------------------------------------
  // Timeline Parser
  // -------------------------------------------------------------------------

  ChatMermaidParseResult _parseTimeline(List<String> lines) {
    var title = '';
    final sections = <_MutableTimelineSection>[];
    _MutableTimelineSection? currentSection;
    var sectionOrder = 0;
    var periodOrder = 0;

    _MutableTimelineSection ensureSection() {
      currentSection ??= () {
        final created = _MutableTimelineSection(title: '', order: sectionOrder++);
        sections.add(created);
        return created;
      }();
      return currentSection!;
    }

    for (final rawLine in lines.skip(1)) {
      final line = rawLine.trim();
      if (line.isEmpty || _isComment(line)) {
        continue;
      }

      if (line.startsWith('title ')) {
        title = line.substring('title '.length).trim();
        continue;
      }

      if (line.startsWith('section ')) {
        final sectionTitle = line.substring('section '.length).trim();
        currentSection =
            _MutableTimelineSection(title: sectionTitle, order: sectionOrder++);
        sections.add(currentSection!);
        continue;
      }

      // 以 ":" 开头的续行:把事件追加到上一个时间段。
      if (line.startsWith(':')) {
        final section = currentSection;
        final lastPeriod = section?.periods.isNotEmpty == true
            ? section!.periods.last
            : null;
        if (lastPeriod != null) {
          for (final event in _splitTimelineEvents(line.substring(1))) {
            lastPeriod.events.add(event);
          }
          continue;
        }
        // 没有上一个时间段则忽略孤立的续行。
        continue;
      }

      // 时间段行:"period : event1 : event2 ...";冒号可缺省(仅时间段无事件)。
      final colonIndex = line.indexOf(':');
      final periodLabel =
          (colonIndex >= 0 ? line.substring(0, colonIndex) : line).trim();
      if (periodLabel.isEmpty) {
        continue;
      }
      final events = colonIndex >= 0
          ? _splitTimelineEvents(line.substring(colonIndex + 1))
          : <String>[];
      ensureSection().periods.add(
            _MutableTimelinePeriod(
              label: periodLabel,
              events: events.toList(),
              order: periodOrder++,
            ),
          );
    }

    final hasPeriod = sections.any((section) => section.periods.isNotEmpty);
    if (!hasPeriod) {
      return ChatMermaidParseResult.unsupported(
        error: 'timeline has no periods',
      );
    }

    final builtSections = sections
        .where((section) => section.periods.isNotEmpty)
        .map((section) => ChatMermaidTimelineSection(
              title: section.title,
              order: section.order,
              periods: List.unmodifiable(
                section.periods.map((period) => ChatMermaidTimelinePeriod(
                      label: period.label,
                      events: List.unmodifiable(period.events),
                      order: period.order,
                    )),
              ),
            ))
        .toList(growable: false);

    return ChatMermaidParseResult.supported(
      diagram: ChatMermaidTimelineDiagram(
        title: title,
        sections: List.unmodifiable(builtSections),
      ),
    );
  }

  List<String> _splitTimelineEvents(String raw) {
    return raw
        .split(':')
        .map((event) => event.trim())
        .where((event) => event.isNotEmpty)
        .toList(growable: false);
  }

  // -------------------------------------------------------------------------
  // Quadrant Chart Parser
  // -------------------------------------------------------------------------

  ChatMermaidParseResult _parseQuadrant(List<String> lines) {
    var title = '';
    var xAxisLeft = '';
    var xAxisRight = '';
    var yAxisBottom = '';
    var yAxisTop = '';
    var quadrant1 = '';
    var quadrant2 = '';
    var quadrant3 = '';
    var quadrant4 = '';
    final points = <ChatMermaidQuadrantPoint>[];
    var pointOrder = 0;

    for (final rawLine in lines.skip(1)) {
      final line = rawLine.trim();
      if (line.isEmpty || _isComment(line)) {
        continue;
      }

      if (line.startsWith('title ')) {
        title = line.substring('title '.length).trim();
        continue;
      }
      if (line.startsWith('x-axis ')) {
        final axis = _parseQuadrantAxis(line.substring('x-axis '.length));
        xAxisLeft = axis.$1;
        xAxisRight = axis.$2;
        continue;
      }
      if (line.startsWith('y-axis ')) {
        final axis = _parseQuadrantAxis(line.substring('y-axis '.length));
        yAxisBottom = axis.$1;
        yAxisTop = axis.$2;
        continue;
      }
      final quadrantMatch =
          RegExp(r'^quadrant-([1-4])\s+(.+)$').firstMatch(line);
      if (quadrantMatch != null) {
        final label = _stripQuotes(quadrantMatch.group(2)!.trim());
        switch (quadrantMatch.group(1)!) {
          case '1':
            quadrant1 = label;
            break;
          case '2':
            quadrant2 = label;
            break;
          case '3':
            quadrant3 = label;
            break;
          case '4':
            quadrant4 = label;
            break;
        }
        continue;
      }

      // 数据点:Label: [x, y](x、y 取 0..1)。
      final pointMatch = RegExp(
        r'^(.+?)\s*:\s*\[\s*(-?[0-9]*\.?[0-9]+)\s*,\s*(-?[0-9]*\.?[0-9]+)\s*\]$',
      ).firstMatch(line);
      if (pointMatch != null) {
        final x = double.tryParse(pointMatch.group(2)!);
        final y = double.tryParse(pointMatch.group(3)!);
        if (x != null && y != null) {
          points.add(ChatMermaidQuadrantPoint(
            label: _stripQuotes(pointMatch.group(1)!.trim()),
            x: x.clamp(0.0, 1.0),
            y: y.clamp(0.0, 1.0),
            order: pointOrder++,
          ));
        }
        continue;
      }
    }

    final hasContent = points.isNotEmpty ||
        quadrant1.isNotEmpty ||
        quadrant2.isNotEmpty ||
        quadrant3.isNotEmpty ||
        quadrant4.isNotEmpty;
    if (!hasContent) {
      return ChatMermaidParseResult.unsupported(
        error: 'quadrant chart has no points or quadrant labels',
      );
    }

    return ChatMermaidParseResult.supported(
      diagram: ChatMermaidQuadrantDiagram(
        title: title,
        xAxisLeft: xAxisLeft,
        xAxisRight: xAxisRight,
        yAxisBottom: yAxisBottom,
        yAxisTop: yAxisTop,
        quadrant1: quadrant1,
        quadrant2: quadrant2,
        quadrant3: quadrant3,
        quadrant4: quadrant4,
        points: List.unmodifiable(points),
      ),
    );
  }

  /// 解析轴定义 "Low --> High";无 "-->" 时整体作为起点标签。
  (String, String) _parseQuadrantAxis(String raw) {
    final parts = raw.split('-->');
    if (parts.length >= 2) {
      return (
        _stripQuotes(parts[0].trim()),
        _stripQuotes(parts.sublist(1).join('-->').trim()),
      );
    }
    return (_stripQuotes(raw.trim()), '');
  }

  String _stripQuotes(String input) {
    final trimmed = input.trim();
    if (trimmed.length >= 2 &&
        ((trimmed.startsWith('"') && trimmed.endsWith('"')) ||
            (trimmed.startsWith("'") && trimmed.endsWith("'")))) {
      return trimmed.substring(1, trimmed.length - 1);
    }
    return trimmed;
  }

  // -------------------------------------------------------------------------
  // Sankey Parser
  // -------------------------------------------------------------------------

  ChatMermaidParseResult _parseSankey(List<String> lines) {
    final nodeOrder = <String, int>{};
    final links = <ChatMermaidSankeyLink>[];
    var linkOrder = 0;

    for (final rawLine in lines.skip(1)) {
      final line = rawLine.trim();
      if (line.isEmpty || _isComment(line)) {
        continue;
      }

      final fields = _parseCsvRow(line);
      if (fields.length < 3) {
        continue;
      }
      final source = fields[0].trim();
      final target = fields[1].trim();
      final value = double.tryParse(fields[2].trim());
      if (source.isEmpty || target.isEmpty || value == null || value <= 0) {
        continue;
      }

      nodeOrder.putIfAbsent(source, () => nodeOrder.length);
      nodeOrder.putIfAbsent(target, () => nodeOrder.length);
      links.add(ChatMermaidSankeyLink(
        sourceId: source,
        targetId: target,
        value: value,
        order: linkOrder++,
      ));
    }

    if (links.isEmpty) {
      return ChatMermaidParseResult.unsupported(
        error: 'sankey has no links',
      );
    }

    final nodes = nodeOrder.entries
        .map((entry) => ChatMermaidSankeyNode(id: entry.key, order: entry.value))
        .toList(growable: false)
      ..sort((a, b) => a.order.compareTo(b.order));

    return ChatMermaidParseResult.supported(
      diagram: ChatMermaidSankeyDiagram(
        nodes: List.unmodifiable(nodes),
        links: List.unmodifiable(links),
      ),
    );
  }

  /// 解析单行 CSV:支持双引号包裹字段,字段内 "" 表示一个字面量引号,
  /// 引号内的逗号不作为分隔符。
  List<String> _parseCsvRow(String line) {
    final fields = <String>[];
    final buffer = StringBuffer();
    var inQuotes = false;

    for (var i = 0; i < line.length; i += 1) {
      final char = line[i];
      if (inQuotes) {
        if (char == '"') {
          if (i + 1 < line.length && line[i + 1] == '"') {
            buffer.write('"');
            i += 1;
          } else {
            inQuotes = false;
          }
        } else {
          buffer.write(char);
        }
        continue;
      }
      if (char == '"') {
        inQuotes = true;
        continue;
      }
      if (char == ',') {
        fields.add(buffer.toString());
        buffer.clear();
        continue;
      }
      buffer.write(char);
    }
    fields.add(buffer.toString());
    return fields;
  }

  // -------------------------------------------------------------------------
  // Radar Parser
  // -------------------------------------------------------------------------

  ChatMermaidParseResult _parseRadar(List<String> lines) {
    var title = '';
    final axes = <ChatMermaidRadarAxis>[];
    final axisIndex = <String, int>{};
    final rawCurves = <_RawRadarCurve>[];
    double? minValue;
    double? maxValue;
    var ticks = 5;
    var graticule = ChatMermaidRadarGraticule.circle;
    var showLegend = true;

    for (final rawLine in lines.skip(1)) {
      final line = rawLine.trim();
      if (line.isEmpty || _isComment(line)) {
        continue;
      }

      if (line.startsWith('title ')) {
        title = line.substring('title '.length).trim();
        continue;
      }
      if (line.startsWith('axis ')) {
        for (final token in _splitRadarTopLevel(line.substring('axis '.length))) {
          final axis = _parseRadarAxisToken(token, axes.length);
          if (axis != null && !axisIndex.containsKey(axis.id)) {
            axisIndex[axis.id] = axes.length;
            axes.add(axis);
          }
        }
        continue;
      }
      if (line.startsWith('curve ')) {
        for (final token
            in _splitRadarTopLevel(line.substring('curve '.length))) {
          final curve = _parseRadarCurveToken(token, rawCurves.length);
          if (curve != null) {
            rawCurves.add(curve);
          }
        }
        continue;
      }
      if (line.startsWith('max ')) {
        maxValue = double.tryParse(line.substring('max '.length).trim());
        continue;
      }
      if (line.startsWith('min ')) {
        minValue = double.tryParse(line.substring('min '.length).trim());
        continue;
      }
      if (line.startsWith('ticks ')) {
        final value = int.tryParse(line.substring('ticks '.length).trim());
        if (value != null && value > 0) {
          ticks = value;
        }
        continue;
      }
      if (line.startsWith('graticule ')) {
        final value = line.substring('graticule '.length).trim().toLowerCase();
        graticule = value == 'polygon'
            ? ChatMermaidRadarGraticule.polygon
            : ChatMermaidRadarGraticule.circle;
        continue;
      }
      if (line.startsWith('showLegend ')) {
        final value = line.substring('showLegend '.length).trim().toLowerCase();
        showLegend = value != 'false';
        continue;
      }
    }

    if (axes.length < 3 || rawCurves.isEmpty) {
      return ChatMermaidParseResult.unsupported(
        error: 'radar requires at least 3 axes and 1 curve',
      );
    }

    final resolvedMin = minValue ?? 0;
    // 将每条曲线对齐到轴顺序;键值形式按轴 id 取值,缺失补 min。
    final curves = <ChatMermaidRadarCurve>[];
    var dataMax = resolvedMin;
    for (final raw in rawCurves) {
      final values = List<double>.filled(axes.length, resolvedMin);
      if (raw.keyed != null) {
        raw.keyed!.forEach((axisId, value) {
          final idx = axisIndex[axisId];
          if (idx != null) {
            values[idx] = value;
          }
        });
      } else {
        for (var i = 0; i < axes.length && i < raw.positional!.length; i += 1) {
          values[i] = raw.positional![i];
        }
      }
      for (final value in values) {
        if (value > dataMax) dataMax = value;
      }
      curves.add(ChatMermaidRadarCurve(
        id: raw.id,
        label: raw.label,
        values: List.unmodifiable(values),
        order: raw.order,
      ));
    }

    final resolvedMax = maxValue ?? (dataMax > resolvedMin ? dataMax : resolvedMin + 1);

    return ChatMermaidParseResult.supported(
      diagram: ChatMermaidRadarDiagram(
        title: title,
        axes: List.unmodifiable(axes),
        curves: List.unmodifiable(curves),
        minValue: resolvedMin,
        maxValue: resolvedMax,
        ticks: ticks,
        graticule: graticule,
        showLegend: showLegend,
      ),
    );
  }

  /// 按顶层逗号拆分,尊重 []、{}、"" 的嵌套(用于 axis/curve 多项同行)。
  List<String> _splitRadarTopLevel(String input) {
    final tokens = <String>[];
    final buffer = StringBuffer();
    var square = 0;
    var curly = 0;
    var inQuotes = false;

    for (var i = 0; i < input.length; i += 1) {
      final char = input[i];
      if (inQuotes) {
        buffer.write(char);
        if (char == '"') inQuotes = false;
        continue;
      }
      switch (char) {
        case '"':
          inQuotes = true;
          buffer.write(char);
          break;
        case '[':
          square += 1;
          buffer.write(char);
          break;
        case ']':
          if (square > 0) square -= 1;
          buffer.write(char);
          break;
        case '{':
          curly += 1;
          buffer.write(char);
          break;
        case '}':
          if (curly > 0) curly -= 1;
          buffer.write(char);
          break;
        case ',':
          if (square == 0 && curly == 0) {
            tokens.add(buffer.toString().trim());
            buffer.clear();
          } else {
            buffer.write(char);
          }
          break;
        default:
          buffer.write(char);
      }
    }
    final tail = buffer.toString().trim();
    if (tail.isNotEmpty) {
      tokens.add(tail);
    }
    return tokens.where((token) => token.isNotEmpty).toList(growable: false);
  }

  ChatMermaidRadarAxis? _parseRadarAxisToken(String token, int order) {
    final match =
        RegExp(r'^([^\[\]]+?)\s*(?:\[(.*)\])?$').firstMatch(token.trim());
    if (match == null) {
      return null;
    }
    final id = match.group(1)!.trim();
    if (id.isEmpty) {
      return null;
    }
    final label =
        match.group(2) != null ? _stripQuotes(match.group(2)!) : id;
    return ChatMermaidRadarAxis(
      id: id,
      label: label.isEmpty ? id : label,
      order: order,
    );
  }

  _RawRadarCurve? _parseRadarCurveToken(String token, int order) {
    final match = RegExp(r'^([^\[\{]+?)\s*(?:\[(.*?)\])?\s*\{(.*)\}$')
        .firstMatch(token.trim());
    if (match == null) {
      return null;
    }
    final id = match.group(1)!.trim();
    if (id.isEmpty) {
      return null;
    }
    final label = match.group(2) != null ? _stripQuotes(match.group(2)!) : id;
    final body = match.group(3)!.trim();

    if (body.contains(':')) {
      final keyed = <String, double>{};
      for (final pair in body.split(',')) {
        final kv = pair.split(':');
        if (kv.length == 2) {
          final value = double.tryParse(kv[1].trim());
          if (value != null) {
            keyed[kv[0].trim()] = value;
          }
        }
      }
      return _RawRadarCurve(
        id: id,
        label: label.isEmpty ? id : label,
        keyed: keyed,
        order: order,
      );
    }

    final positional = body
        .split(',')
        .map((value) => double.tryParse(value.trim()))
        .where((value) => value != null)
        .map((value) => value!)
        .toList(growable: false);
    return _RawRadarCurve(
      id: id,
      label: label.isEmpty ? id : label,
      positional: positional,
      order: order,
    );
  }

  // -------------------------------------------------------------------------
  // Kanban Parser
  // -------------------------------------------------------------------------

  ChatMermaidParseResult _parseKanban(List<String> lines) {
    final columns = <_MutableKanbanColumn>[];
    _MutableKanbanColumn? current;
    int? columnIndent;
    var columnOrder = 0;
    var itemOrder = 0;

    for (final rawLine in lines.skip(1)) {
      if (rawLine.trim().isEmpty || _isComment(rawLine.trim())) {
        continue;
      }
      final indent = rawLine.length - rawLine.trimLeft().length;
      final content = rawLine.trim();
      columnIndent ??= indent;

      if (indent <= columnIndent) {
        // 列(阶段)
        final node = _parseKanbanNode(content);
        if (node == null) {
          continue;
        }
        current = _MutableKanbanColumn(
          id: node.$1,
          title: node.$2,
          order: columnOrder++,
        );
        columns.add(current);
      } else {
        // 任务卡
        if (current == null) {
          continue;
        }
        final item = _parseKanbanItem(content, itemOrder++);
        if (item != null) {
          current.items.add(item);
        }
      }
    }

    if (columns.isEmpty) {
      return ChatMermaidParseResult.unsupported(
        error: 'kanban has no columns',
      );
    }

    final builtColumns = columns
        .map((column) => ChatMermaidKanbanColumn(
              id: column.id,
              title: column.title,
              order: column.order,
              items: List.unmodifiable(column.items),
            ))
        .toList(growable: false);

    return ChatMermaidParseResult.supported(
      diagram: ChatMermaidKanbanDiagram(
        columns: List.unmodifiable(builtColumns),
      ),
    );
  }

  /// 解析 id[标题] / [标题] / 裸标题,返回 (id, 标题)。
  (String, String)? _parseKanbanNode(String text) {
    final trimmed = text.trim();
    if (trimmed.isEmpty) {
      return null;
    }
    final bracket = RegExp(r'^([^\[\]]*?)\s*\[(.*)\]$').firstMatch(trimmed);
    if (bracket != null) {
      final id = bracket.group(1)!.trim();
      final label = bracket.group(2)!.trim();
      return (id.isEmpty ? label : id, label);
    }
    return (trimmed, trimmed);
  }

  ChatMermaidKanbanItem? _parseKanbanItem(String text, int order) {
    var body = text.trim();
    String? assigned;
    String? ticket;
    String? priority;

    // 尾部 @{ ... } 元数据
    final metaMatch = RegExp(r'@\{(.*)\}\s*$').firstMatch(body);
    if (metaMatch != null) {
      final meta = _parseKanbanMetadata(metaMatch.group(1)!);
      assigned = meta['assigned'];
      ticket = meta['ticket'];
      priority = meta['priority'];
      body = body.substring(0, metaMatch.start).trim();
    }

    final node = _parseKanbanNode(body);
    if (node == null) {
      return null;
    }
    return ChatMermaidKanbanItem(
      id: node.$1,
      text: node.$2,
      assigned: assigned,
      ticket: ticket,
      priority: priority,
      order: order,
    );
  }

  Map<String, String> _parseKanbanMetadata(String raw) {
    final result = <String, String>{};
    for (final pair in raw.split(',')) {
      final idx = pair.indexOf(':');
      if (idx <= 0) {
        continue;
      }
      final key = pair.substring(0, idx).trim();
      final value = _stripQuotes(pair.substring(idx + 1).trim());
      if (key.isNotEmpty && value.isNotEmpty) {
        result[key] = value;
      }
    }
    return result;
  }

  // -------------------------------------------------------------------------
  // Treemap Parser
  // -------------------------------------------------------------------------

  ChatMermaidParseResult _parseTreemap(List<String> lines) {
    // 收集 (缩进, 标签, 显式取值)
    final parsed = <_RawTreemapLine>[];
    for (final rawLine in lines.skip(1)) {
      if (rawLine.trim().isEmpty || _isComment(rawLine.trim())) {
        continue;
      }
      final indent = rawLine.length - rawLine.trimLeft().length;
      final node = _parseTreemapLine(rawLine.trim());
      if (node != null) {
        parsed.add(_RawTreemapLine(
          indent: indent,
          label: node.$1,
          value: node.$2,
        ));
      }
    }
    if (parsed.isEmpty) {
      return ChatMermaidParseResult.unsupported(error: 'treemap has no nodes');
    }

    // 基于缩进的栈式建树
    final roots = <_MutableTreemapNode>[];
    final stack = <_MutableTreemapNode>[];
    var order = 0;
    for (final line in parsed) {
      final node = _MutableTreemapNode(
        label: line.label,
        explicitValue: line.value,
        order: order++,
      );
      while (stack.isNotEmpty && stack.last.indent >= line.indent) {
        stack.removeLast();
      }
      node.indent = line.indent;
      if (stack.isEmpty) {
        roots.add(node);
      } else {
        stack.last.children.add(node);
      }
      stack.add(node);
    }

    final builtRoots =
        roots.map(_buildTreemapNode).toList(growable: false);
    final total = builtRoots.fold<double>(0, (sum, n) => sum + n.value);
    if (total <= 0) {
      return ChatMermaidParseResult.unsupported(
        error: 'treemap has no positive values',
      );
    }

    return ChatMermaidParseResult.supported(
      diagram: ChatMermaidTreemapDiagram(roots: List.unmodifiable(builtRoots)),
    );
  }

  ChatMermaidTreemapNode _buildTreemapNode(_MutableTreemapNode node) {
    if (node.children.isEmpty) {
      return ChatMermaidTreemapNode(
        label: node.label,
        value: node.explicitValue ?? 0,
        isLeaf: true,
        children: const <ChatMermaidTreemapNode>[],
        order: node.order,
      );
    }
    final children = node.children.map(_buildTreemapNode).toList(growable: false);
    final sum = children.fold<double>(0, (acc, c) => acc + c.value);
    return ChatMermaidTreemapNode(
      label: node.label,
      value: sum,
      isLeaf: false,
      children: List.unmodifiable(children),
      order: node.order,
    );
  }

  /// 解析单行:"名称" 或 "名称": 值,可带尾部 :::class(忽略);也兼容无引号。
  (String, double?)? _parseTreemapLine(String text) {
    var body = text.trim();
    // 去除尾部 :::class
    body = body.replaceFirst(RegExp(r'\s*:::\S+\s*$'), '').trim();
    if (body.isEmpty) {
      return null;
    }

    // 带引号:"名称" 或 "名称": 值
    final quoted =
        RegExp(r'^"([^"]*)"\s*(?::\s*([\d.,]+))?$').firstMatch(body);
    if (quoted != null) {
      final label = quoted.group(1)!.trim();
      final value = _parseTreemapValue(quoted.group(2));
      return label.isEmpty ? null : (label, value);
    }

    // 无引号叶:名称: 值
    final leaf = RegExp(r'^(.+?)\s*:\s*([\d.,]+)$').firstMatch(body);
    if (leaf != null) {
      return (leaf.group(1)!.trim(), _parseTreemapValue(leaf.group(2)));
    }

    // 无引号分组:整体作为标签
    return (body, null);
  }

  double? _parseTreemapValue(String? token) {
    if (token == null) {
      return null;
    }
    return double.tryParse(token.replaceAll(',', ''));
  }

  // -------------------------------------------------------------------------
  // Block Parser
  // -------------------------------------------------------------------------

  ChatMermaidParseResult _parseBlock(List<String> lines) {
    final rootItems = <_MutableBlockItem>[];
    final edges = <ChatMermaidBlockEdge>[];
    int? rootColumns;
    var rootFirstRowCount = 0;
    var order = 0;
    var edgeOrder = 0;

    // 复合块容器栈
    final stack = <_MutableBlockItem>[];
    List<_MutableBlockItem> currentItems() =>
        stack.isEmpty ? rootItems : stack.last.children;

    for (final rawLine in lines.skip(1)) {
      final line = rawLine.trim();
      if (line.isEmpty || _isComment(line)) {
        continue;
      }
      if (line == 'end') {
        if (stack.isNotEmpty) {
          stack.removeLast();
        }
        continue;
      }
      if (line.startsWith('columns ')) {
        final value = int.tryParse(line.substring('columns '.length).trim());
        if (value != null && value > 0) {
          if (stack.isEmpty) {
            rootColumns = value;
          } else {
            stack.last.explicitColumns = value;
          }
        }
        continue;
      }
      if (_isSkippableBlockDirective(line)) {
        continue;
      }
      // 边语句(含 --> 或 ---)
      if (line.contains('-->') || line.contains('---')) {
        final edge = _parseBlockEdge(line);
        if (edge != null) {
          edges.add(ChatMermaidBlockEdge(
            sourceId: edge.$1,
            targetId: edge.$3,
            label: edge.$2,
            order: edgeOrder++,
          ));
        }
        continue;
      }

      // 块定义行:按顶层空白拆分为若干 token
      final tokens = _splitBlockTokens(line);
      // 行起始所属容器(null 表示根);只统计加入该容器的单元数作为其首行列数。
      final startContainer = stack.isEmpty ? null : stack.last;
      var directCount = 0;
      for (final token in tokens) {
        final atStartLevel =
            (stack.isEmpty ? null : stack.last) == startContainer;
        if (token.startsWith('block:')) {
          final rest = token.substring('block:'.length);
          final m = RegExp(r'^([^:\s]+)(?::(\d+))?$').firstMatch(rest);
          final composite = _MutableBlockItem(
            id: m?.group(1) ?? rest,
            label: m?.group(1) ?? rest,
            shape: ChatMermaidNodeShape.rectangle,
            width: 1,
            isSpace: false,
            isComposite: true,
            explicitColumns:
                m?.group(2) != null ? int.tryParse(m!.group(2)!) : null,
            order: order++,
          );
          currentItems().add(composite);
          if (atStartLevel) directCount += 1;
          stack.add(composite);
          continue;
        }
        final item = _parseBlockToken(token, order++);
        if (item != null) {
          currentItems().add(item);
          if (atStartLevel) directCount += 1;
        }
      }
      if (startContainer == null) {
        if (rootFirstRowCount == 0) rootFirstRowCount = directCount;
      } else {
        if (startContainer.firstRowCount == 0) {
          startContainer.firstRowCount = directCount;
        }
      }
    }

    if (rootItems.isEmpty) {
      return ChatMermaidParseResult.unsupported(error: 'block has no blocks');
    }

    return ChatMermaidParseResult.supported(
      diagram: ChatMermaidBlockDiagram(
        columns: rootColumns ?? (rootFirstRowCount > 0 ? rootFirstRowCount : 1),
        items: List.unmodifiable(rootItems.map(_buildBlockItem)),
        edges: List.unmodifiable(edges),
      ),
    );
  }

  ChatMermaidBlockItem _buildBlockItem(_MutableBlockItem node) {
    final children = node.children.map(_buildBlockItem).toList(growable: false);
    return ChatMermaidBlockItem(
      id: node.id,
      label: node.label,
      shape: node.shape,
      width: node.width,
      isSpace: node.isSpace,
      isComposite: node.isComposite,
      compositeColumns: node.explicitColumns ??
          (node.firstRowCount > 0 ? node.firstRowCount : 1),
      children: List.unmodifiable(children),
      order: node.order,
    );
  }

  bool _isSkippableBlockDirective(String line) {
    final lower = line.toLowerCase();
    return lower.startsWith('style ') ||
        lower.startsWith('classdef ') ||
        lower.startsWith('class ') ||
        lower.startsWith('click ');
  }

  /// 解析单个块 token:space[:n] 或 id[shape/label][:width]。
  _MutableBlockItem? _parseBlockToken(String token, int order) {
    var text = token.trim();
    if (text.isEmpty) {
      return null;
    }
    // space / space:n
    final spaceMatch = RegExp(r'^space(?::(\d+))?$').firstMatch(text);
    if (spaceMatch != null) {
      final w = spaceMatch.group(1) != null
          ? (int.tryParse(spaceMatch.group(1)!) ?? 1)
          : 1;
      return _MutableBlockItem(
        id: '_space_$order',
        label: '',
        shape: ChatMermaidNodeShape.rectangle,
        width: w < 1 ? 1 : w,
        isSpace: true,
        isComposite: false,
        explicitColumns: null,
        order: order,
      );
    }

    // 末尾 :width(在闭合括号之后)
    var width = 1;
    final widthMatch = RegExp(r':(\d+)$').firstMatch(text);
    if (widthMatch != null) {
      width = int.tryParse(widthMatch.group(1)!) ?? 1;
      text = text.substring(0, widthMatch.start).trim();
    }

    // 复用流程图节点解析器识别形状与标签
    final parser = _FlowchartStatementParser(text);
    final node = parser.parseNode();
    final useNode = node != null && !parser.hasTrailingGarbage;
    return _MutableBlockItem(
      id: useNode ? node.id : text,
      label: useNode ? (node.label ?? node.id) : text,
      shape: useNode
          ? (node.shape ?? ChatMermaidNodeShape.rectangle)
          : ChatMermaidNodeShape.rectangle,
      width: width < 1 ? 1 : width,
      isSpace: false,
      isComposite: false,
      explicitColumns: null,
      order: order,
    );
  }

  /// 解析块图边:A --> B / A --- B / A -- "text" --> B。
  (String, String?, String)? _parseBlockEdge(String line) {
    final withText = RegExp(
      r'^(\S+)\s*--\s*"?([^"<>]*?)"?\s*-->\s*(\S+)$',
    ).firstMatch(line);
    if (withText != null) {
      final label = withText.group(2)!.trim();
      return (
        withText.group(1)!,
        label.isEmpty ? null : label,
        withText.group(3)!,
      );
    }
    final plain = RegExp(r'^(\S+)\s*(?:-->|---)\s*(\S+)$').firstMatch(line);
    if (plain != null) {
      return (plain.group(1)!, null, plain.group(2)!);
    }
    return null;
  }

  /// 按顶层空白拆分块定义行,尊重 []{}()"" 的嵌套。
  List<String> _splitBlockTokens(String line) {
    final tokens = <String>[];
    final buffer = StringBuffer();
    var square = 0;
    var curly = 0;
    var paren = 0;
    var inQuotes = false;

    void flush() {
      final t = buffer.toString().trim();
      if (t.isNotEmpty) tokens.add(t);
      buffer.clear();
    }

    for (var i = 0; i < line.length; i += 1) {
      final char = line[i];
      if (inQuotes) {
        buffer.write(char);
        if (char == '"') inQuotes = false;
        continue;
      }
      switch (char) {
        case '"':
          inQuotes = true;
          buffer.write(char);
          break;
        case '[':
          square += 1;
          buffer.write(char);
          break;
        case ']':
          if (square > 0) square -= 1;
          buffer.write(char);
          break;
        case '{':
          curly += 1;
          buffer.write(char);
          break;
        case '}':
          if (curly > 0) curly -= 1;
          buffer.write(char);
          break;
        case '(':
          paren += 1;
          buffer.write(char);
          break;
        case ')':
          if (paren > 0) paren -= 1;
          buffer.write(char);
          break;
        case ' ':
        case '\t':
          if (square == 0 && curly == 0 && paren == 0) {
            flush();
          } else {
            buffer.write(char);
          }
          break;
        default:
          buffer.write(char);
      }
    }
    flush();
    return tokens;
  }

  // -------------------------------------------------------------------------
  // Packet Parser
  // -------------------------------------------------------------------------

  ChatMermaidParseResult _parsePacket(List<String> lines) {
    var title = '';
    final fields = <ChatMermaidPacketField>[];
    var cursor = 0; // 下一个自动起始比特位
    var order = 0;

    for (final rawLine in lines.skip(1)) {
      var line = rawLine.trim();
      if (line.isEmpty || _isComment(line)) {
        continue;
      }
      // 去除行内 %% 注释(在引号外)
      final commentIdx = _indexOfTopLevelComment(line);
      if (commentIdx >= 0) {
        line = line.substring(0, commentIdx).trim();
        if (line.isEmpty) continue;
      }

      if (line.startsWith('title ')) {
        title = line.substring('title '.length).trim();
        continue;
      }

      final match = RegExp(
        r'^(\+\d+|\d+(?:-\d+)?)\s*:\s*(.+)$',
      ).firstMatch(line);
      if (match == null) {
        continue;
      }
      final rangeToken = match.group(1)!;
      final label = _stripQuotes(match.group(2)!.trim());

      int start;
      int end;
      if (rangeToken.startsWith('+')) {
        final count = int.tryParse(rangeToken.substring(1)) ?? 0;
        if (count <= 0) continue;
        start = cursor;
        end = cursor + count - 1;
      } else if (rangeToken.contains('-')) {
        final parts = rangeToken.split('-');
        start = int.tryParse(parts[0]) ?? -1;
        end = int.tryParse(parts[1]) ?? -1;
      } else {
        start = int.tryParse(rangeToken) ?? -1;
        end = start;
      }
      if (start < 0 || end < start) {
        continue;
      }
      cursor = end + 1;
      fields.add(ChatMermaidPacketField(
        start: start,
        end: end,
        label: label,
        order: order++,
      ));
    }

    if (fields.isEmpty) {
      return ChatMermaidParseResult.unsupported(error: 'packet has no fields');
    }

    final maxEnd = fields.fold<int>(0, (m, f) => f.end > m ? f.end : m);
    final totalBits = maxEnd + 1;
    final bitsPerRow = totalBits <= 32 ? totalBits : 32;

    return ChatMermaidParseResult.supported(
      diagram: ChatMermaidPacketDiagram(
        title: title,
        fields: List.unmodifiable(fields),
        bitsPerRow: bitsPerRow < 1 ? 1 : bitsPerRow,
      ),
    );
  }

  /// 返回行内顶层 %%(引号外)的位置,无则 -1。
  int _indexOfTopLevelComment(String line) {
    var inQuotes = false;
    for (var i = 0; i < line.length - 1; i += 1) {
      final char = line[i];
      if (char == '"') {
        inQuotes = !inQuotes;
      } else if (!inQuotes && char == '%' && line[i + 1] == '%') {
        return i;
      }
    }
    return -1;
  }

  // -------------------------------------------------------------------------
  // Git Graph Parser
  // -------------------------------------------------------------------------

  ChatMermaidParseResult _parseGitGraph(List<String> lines) {
    final commits = <ChatMermaidGitCommit>[];
    final branches = <String>[];
    var currentBranch = 'main';
    var commitOrder = 0;
    var commitId = 0;

    branches.add(currentBranch);

    for (final rawLine in lines.skip(1)) {
      final line = rawLine.trim();
      if (line.isEmpty || _isComment(line)) {
        continue;
      }

      // commit id: "abc" tag: "v1.0"
      if (line.startsWith('commit')) {
        String? tag;
        String? id;
        final tagMatch = RegExp(r'tag:\s*"([^"]+)"').firstMatch(line);
        if (tagMatch != null) {
          tag = tagMatch.group(1);
        }
        final idMatch = RegExp(r'id:\s*"([^"]+)"').firstMatch(line);
        if (idMatch != null) {
          id = idMatch.group(1);
        }
        commits.add(ChatMermaidGitCommit(
          id: id ?? 'c${commitId++}',
          branch: currentBranch,
          tag: tag,
          order: commitOrder++,
        ));
        continue;
      }

      // branch name
      if (line.startsWith('branch ')) {
        final branchName =
            line.substring('branch '.length).trim().split(' ').first;
        if (!branches.contains(branchName)) {
          branches.add(branchName);
        }
        continue;
      }

      // checkout name
      if (line.startsWith('checkout ')) {
        currentBranch = line.substring('checkout '.length).trim();
        if (!branches.contains(currentBranch)) {
          branches.add(currentBranch);
        }
        continue;
      }

      // merge name
      if (line.startsWith('merge ')) {
        final mergeFrom =
            line.substring('merge '.length).trim().split(' ').first;
        String? tag;
        final tagMatch = RegExp(r'tag:\s*"([^"]+)"').firstMatch(line);
        if (tagMatch != null) {
          tag = tagMatch.group(1);
        }
        commits.add(ChatMermaidGitCommit(
          id: 'c${commitId++}',
          branch: currentBranch,
          tag: tag,
          mergeFrom: mergeFrom,
          order: commitOrder++,
        ));
        continue;
      }

      // cherry-pick — skip
      if (line.startsWith('cherry-pick')) {
        continue;
      }
    }

    if (commits.isEmpty) {
      return ChatMermaidParseResult.unsupported(
        error: 'git graph has no commits',
      );
    }

    return ChatMermaidParseResult.supported(
      diagram: ChatMermaidGitGraphDiagram(
        commits: List.unmodifiable(commits),
        branches: List.unmodifiable(branches),
      ),
    );
  }

  List<String> _splitFlowchartStatements(String source) {
    final normalized = source.replaceAll('\r\n', '\n').trim();
    if (normalized.isEmpty) {
      return const <String>[];
    }

    final statements = <String>[];
    final buffer = StringBuffer();
    var squareDepth = 0;
    var curlyDepth = 0;
    var parenDepth = 0;
    var inPipeLabel = false;
    String? quote;

    for (var index = 0; index < normalized.length; index += 1) {
      final char = normalized[index];
      final previous = index > 0 ? normalized[index - 1] : '';

      if (quote != null) {
        buffer.write(char);
        if (char == quote && previous != r'\') {
          quote = null;
        }
        continue;
      }

      if (char == '"' || char == "'") {
        quote = char;
        buffer.write(char);
        continue;
      }

      if (char == '|' &&
          squareDepth == 0 &&
          curlyDepth == 0 &&
          parenDepth == 0) {
        inPipeLabel = !inPipeLabel;
        buffer.write(char);
        continue;
      }

      if (!inPipeLabel) {
        switch (char) {
          case '[':
            squareDepth += 1;
            break;
          case ']':
            squareDepth = squareDepth > 0 ? squareDepth - 1 : 0;
            break;
          case '{':
            curlyDepth += 1;
            break;
          case '}':
            curlyDepth = curlyDepth > 0 ? curlyDepth - 1 : 0;
            break;
          case '(':
            parenDepth += 1;
            break;
          case ')':
            parenDepth = parenDepth > 0 ? parenDepth - 1 : 0;
            break;
        }
      }

      final isSeparator = (char == '\n' || char == ';') &&
          squareDepth == 0 &&
          curlyDepth == 0 &&
          parenDepth == 0 &&
          !inPipeLabel;
      if (isSeparator) {
        final statement = buffer.toString().trim();
        if (statement.isNotEmpty) {
          statements.add(statement);
        }
        buffer.clear();
        continue;
      }

      buffer.write(char);
    }

    final tail = buffer.toString().trim();
    if (tail.isNotEmpty) {
      statements.add(tail);
    }

    return statements;
  }

  _ParsedSequenceParticipant? _tryParseSequenceParticipant(String line) {
    // 处理带引号的参与者名称：participant "Name" 或 participant "Name With Spaces" as Alias。
    // 必须在通用模式前匹配，否则 [^\s]+ 会在引号内的第一个空格处截断，
    // 导致 id 带上残缺引号（如 `"Alice`），或整体 match 失败（含空格时）。
    final quotedMatch = RegExp(
      r'''^(participant|actor)\s+"([^"]+)"(?:\s+as\s+(.+))?$''',
    ).firstMatch(line);
    if (quotedMatch != null) {
      final keyword = quotedMatch.group(1)!;
      final name = quotedMatch.group(2)!;
      final alias = quotedMatch.group(3)?.trim();
      final effectiveAlias =
          alias != null && alias.isNotEmpty ? _stripQuotes(alias) : null;
      return _ParsedSequenceParticipant(
        id: effectiveAlias ?? name,
        label: name,
        isActor: keyword == 'actor',
      );
    }

    // Support Unicode characters (Chinese, etc.) in participant names
    // Pattern: participant/actor Name (or Name with "as" alias)
    final match = RegExp(
      r'^(participant|actor)\s+([^\s]+)(?:\s+as\s+(.+))?$',
    ).firstMatch(line);
    if (match == null) {
      return null;
    }

    final keyword = match.group(1)!;
    final id = match.group(2)!;
    final label = match.group(3)?.trim();
    return _ParsedSequenceParticipant(
      id: id,
      label: label == null || label.isEmpty ? id : _stripQuotes(label),
      isActor: keyword == 'actor',
    );
  }

  ChatMermaidSequenceNote? _tryParseSequenceNote(String line) {
    final match = RegExp(
      r'^Note\s+(over|left of|right of)\s+([^:]+)\s*:\s*(.+)$',
    ).firstMatch(line);
    if (match == null) {
      return null;
    }

    final positionToken = match.group(1)!;
    final targetIds = match
        .group(2)!
        .split(',')
        .map((item) => item.trim())
        .where((item) => item.isNotEmpty)
        .toList(growable: false);
    final text = match.group(3)!.trim();
    if (targetIds.isEmpty || text.isEmpty) {
      return null;
    }

    return ChatMermaidSequenceNote(
      order: 0,
      position: _parseNotePosition(positionToken),
      targetIds: targetIds,
      text: text,
    );
  }

  ChatMermaidSequenceMessage? _tryParseSequenceMessage(String line) {
    // Support Unicode characters (Chinese, etc.) in participant names
    final match = RegExp(
      r'^([^\s]+?)\s*(-->>|->>|-->|->)\s*([^\s]+)\s*:\s*(.+)$',
    ).firstMatch(line);
    if (match == null) {
      return null;
    }

    // 去除目标前的 +/- 激活/反激活标记(A->>+B、B-->>-A):激活条不在解析层渲染,
    // 但需保证参与者 id 干净。
    var toId = match.group(3)!;
    if (toId.startsWith('+') || toId.startsWith('-')) {
      toId = toId.substring(1);
    }
    if (toId.isEmpty) {
      return null;
    }

    return ChatMermaidSequenceMessage(
      order: 0,
      fromId: match.group(1)!,
      toId: toId,
      label: match.group(4)!.trim(),
      style: _parseSequenceMessageStyle(match.group(2)!),
    );
  }

  ChatMermaidSequenceGroupStart? _tryParseSequenceGroupStart(String line) {
    for (final prefix in _sequenceGroupPrefixes.entries) {
      if (line.startsWith('${prefix.key} ')) {
        final label = line.substring(prefix.key.length).trim();
        if (label.isEmpty) {
          return null;
        }
        return ChatMermaidSequenceGroupStart(
          order: 0,
          kind: prefix.value,
          label: label,
        );
      }
    }
    return null;
  }

  ChatMermaidSequenceGroupDivider? _tryParseSequenceGroupDivider(String line) {
    if (line == 'else' || line == 'and' || line == 'option') {
      return ChatMermaidSequenceGroupDivider(order: 0, label: line);
    }
    for (final keyword in const <String>['else ', 'and ', 'option ']) {
      if (line.startsWith(keyword)) {
        final label = line.substring(keyword.length).trim();
        return ChatMermaidSequenceGroupDivider(
          order: 0,
          label: label.isEmpty ? keyword.trim() : label,
        );
      }
    }
    return null;
  }

  _ParsedStateNode? _tryParseStateDeclaration(String line) {
    final aliased = RegExp(
      r'''^state\s+("([^"]+)"|'([^']+)')\s+as\s+([A-Za-z0-9_.-]+)\s*$''',
    ).firstMatch(line);
    if (aliased != null) {
      return _ParsedStateNode(
        id: aliased.group(4)!,
        label: aliased.group(2) ?? aliased.group(3)!,
      );
    }

    // state ID <<fork>> / <<join>> / <<choice>>:特殊节点。
    // 模型无 fork/join/choice 类型,映射为普通节点以便注册与转换解析。
    final special = RegExp(
      r'^state\s+([A-Za-z0-9_.-]+)\s+<<(?:fork|join|choice)>>\s*$',
    ).firstMatch(line);
    if (special != null) {
      return _ParsedStateNode(id: special.group(1)!, label: special.group(1)!);
    }

    final bare = RegExp(r'^state\s+([A-Za-z0-9_.-]+)\s*$').firstMatch(line);
    if (bare == null) {
      return null;
    }
    return _ParsedStateNode(id: bare.group(1)!, label: bare.group(1)!);
  }

  _ParsedStateTransition? _tryParseStateTransition(String line) {
    // Support Unicode characters (Chinese, etc.) in state names
    final match = RegExp(
      r'^(\[\*\]|[^\s]+)\s*-->\s*(\[\*\]|[^\s]+)(?:\s*:\s*(.+))?$',
    ).firstMatch(line);
    if (match == null) {
      return null;
    }

    final sourceToken = match.group(1)!;
    final targetToken = match.group(2)!;
    final label = match.group(3)?.trim();
    return _ParsedStateTransition(
      source: _parseStateReference(sourceToken, isSource: true),
      target: _parseStateReference(targetToken, isSource: false),
      label: label == null || label.isEmpty ? null : label,
    );
  }

  _ParsedStateNode _parseStateReference(
    String token, {
    required bool isSource,
  }) {
    if (token == '[*]') {
      return isSource
          ? const _ParsedStateNode(
              id: _stateStartId,
              label: '',
              kind: ChatMermaidStateNodeKind.start,
            )
          : const _ParsedStateNode(
              id: _stateEndId,
              label: '',
              kind: ChatMermaidStateNodeKind.end,
            );
    }
    return _ParsedStateNode(id: token, label: token);
  }

  _ParsedGanttTask? _tryParseGanttTask(String line) {
    final separatorIndex = line.indexOf(':');
    if (separatorIndex <= 0 || separatorIndex == line.length - 1) {
      return null;
    }

    final label = line.substring(0, separatorIndex).trim();
    if (label.isEmpty) {
      return null;
    }

    final allTokens = line
        .substring(separatorIndex + 1)
        .split(',')
        .map((token) => token.trim())
        .where((token) => token.isNotEmpty)
        .toList(growable: false);

    // 去除前导状态标签(done/active/crit/milestone),它们不影响时间计算。
    const statusTags = <String>{'active', 'done', 'crit', 'milestone'};
    var startIndex = 0;
    while (startIndex < allTokens.length &&
        statusTags.contains(allTokens[startIndex].toLowerCase())) {
      startIndex += 1;
    }
    final tokens = allTokens.sublist(startIndex);
    if (tokens.length < 2 || tokens.length > 3) {
      return null;
    }

    String? id;
    late final String startToken;
    late final String durationToken;
    if (tokens.length == 3) {
      id = tokens[0];
      startToken = tokens[1];
      durationToken = tokens[2];
      if (!_ganttIdPattern.hasMatch(id)) {
        return null;
      }
    } else {
      startToken = tokens[0];
      durationToken = tokens[1];
    }

    // Duration can be a duration literal (e.g. 3d, 2w) or an end date
    // (e.g. 2024-01-15). Try duration first, then end date.
    int? durationDays = _parseGanttDuration(durationToken);
    final DateTime? endDate;
    if (durationDays != null) {
      endDate = null;
    } else {
      endDate = _parseIsoDate(durationToken);
      if (endDate == null) {
        return null;
      }
      // Will compute actual duration after resolving start date.
    }

    if (startToken.startsWith('after ')) {
      final dependencyId = startToken.substring('after '.length).trim();
      if (!_ganttIdPattern.hasMatch(dependencyId)) {
        return null;
      }
      // For end-date format with dependency, we cannot resolve duration yet;
      // fall back to unsupported since dependency start is unknown at parse
      // time. Use a placeholder; resolution happens in the build phase.
      if (durationDays == null) {
        // Can't compute duration without knowing start; not supported with
        // after-dependency syntax.
        return null;
      }
      return _ParsedGanttTask.afterDependency(
        id: id,
        label: label,
        dependencyId: dependencyId,
        durationDays: durationDays,
      );
    }

    var startDate = _parseIsoDate(startToken);
    // Plain number as start: treat as day offset from epoch (dateFormat X/x).
    if (startDate == null) {
      final numericStart = int.tryParse(startToken);
      if (numericStart != null && numericStart >= 0) {
        startDate = DateTime(2000).add(Duration(days: numericStart));
      }
    }
    if (startDate == null) {
      return null;
    }
    if (durationDays == null && endDate != null) {
      durationDays = endDate.difference(startDate).inDays;
      if (durationDays <= 0) {
        durationDays = 1;
      }
    }
    return _ParsedGanttTask.absolute(
      id: id,
      label: label,
      startDate: startDate,
      durationDays: durationDays!,
    );
  }

  int? _parseGanttDuration(String token) {
    final match = RegExp(r'^(\d+)(h|m|d|w|M|y)$').firstMatch(token);
    if (match != null) {
      final count = int.tryParse(match.group(1)!);
      if (count == null || count < 0) return null;
      final days = switch (match.group(2)!) {
        'h' => (count / 24).ceil(),
        'm' => (count / 1440).ceil(),
        'd' => count,
        'w' => count * 7,
        'M' => count * 30,
        'y' => count * 365,
        _ => -1,
      };
      if (days < 0) return null;
      return days == 0 ? 1 : days;
    }
    // Plain number without unit: treat as days (supports dateFormat X/x).
    final plain = int.tryParse(token);
    if (plain != null && plain >= 0) {
      return plain == 0 ? 1 : plain;
    }
    return null;
  }

  DateTime? _resolveGanttTaskStart(
    _ParsedGanttTask task,
    Map<String, ChatMermaidGanttTask> taskById,
  ) {
    if (task.startDate != null) {
      return task.startDate;
    }
    final dependencyId = task.dependencyId;
    if (dependencyId == null) {
      return null;
    }
    return taskById[dependencyId]?.endDateExclusive;
  }

  DateTime? _parseIsoDate(String token) {
    final match = RegExp(r'^(\d{4})-(\d{2})-(\d{2})$').firstMatch(token);
    if (match == null) {
      return null;
    }
    final year = int.parse(match.group(1)!);
    final month = int.parse(match.group(2)!);
    final day = int.parse(match.group(3)!);
    if (month < 1 || month > 12 || day < 1 || day > 31) {
      return null;
    }
    final date = DateTime.utc(year, month, day);
    if (date.year != year || date.month != month || date.day != day) {
      return null;
    }
    return date;
  }

  bool _isComment(String statement) => statement.trimLeft().startsWith('%%');

  bool _isSkippableFlowchartDirective(String statement) {
    final trimmed = statement.trimLeft().toLowerCase();
    return trimmed.startsWith('linkstyle ') ||
        trimmed.startsWith('direction ') ||
        trimmed.startsWith('acctitle:') ||
        trimmed.startsWith('accdescr:') ||
        trimmed.startsWith('accdescr {') ||
        trimmed.startsWith('click ');
  }

  /// 尝试解析 classDef/style/class 指令。返回 true 表示已处理。
  bool _tryParseFlowchartStyleDirective(
    String statement,
    _FlowchartBuilder builder,
  ) {
    final trimmed = statement.trimLeft();
    final lower = trimmed.toLowerCase();

    // classDef className fill:#f9f,stroke:#333
    if (lower.startsWith('classdef ')) {
      final rest = trimmed.substring('classdef '.length).trim();
      final spaceIdx = rest.indexOf(' ');
      if (spaceIdx < 1) return true; // 语法不完整，忽略
      final className = rest.substring(0, spaceIdx).trim();
      final props = rest.substring(spaceIdx + 1).trim();
      final style = _parseCssStyleProps(props);
      if (style != null) {
        builder.addClassDef(className, style);
      }
      return true;
    }

    // style nodeId1,nodeId2 fill:#f9f,stroke:#333
    if (lower.startsWith('style ')) {
      final rest = trimmed.substring('style '.length).trim();
      final spaceIdx = rest.indexOf(' ');
      if (spaceIdx < 1) return true;
      final idsPart = rest.substring(0, spaceIdx).trim();
      final props = rest.substring(spaceIdx + 1).trim();
      final style = _parseCssStyleProps(props);
      if (style != null) {
        for (final id in idsPart.split(',')) {
          final trimId = id.trim();
          if (trimId.isNotEmpty) {
            builder.addNodeStyle(trimId, style);
          }
        }
      }
      return true;
    }

    // class nodeId1,nodeId2 className
    if (lower.startsWith('class ')) {
      final rest = trimmed.substring('class '.length).trim();
      final spaceIdx = rest.lastIndexOf(' ');
      if (spaceIdx < 1) return true;
      final idsPart = rest.substring(0, spaceIdx).trim();
      final className = rest.substring(spaceIdx + 1).trim();
      for (final id in idsPart.split(',')) {
        final trimId = id.trim();
        if (trimId.isNotEmpty) {
          builder.assignClass(trimId, className);
        }
      }
      return true;
    }

    return false;
  }

  /// 解析 CSS 风格的属性字符串，提取 fill、stroke、color 颜色。
  static _NodeColors? _parseCssStyleProps(String props) {
    int? fill;
    int? stroke;
    int? color;
    for (final part in props.split(',')) {
      final kv = part.split(':');
      if (kv.length != 2) continue;
      final key = kv[0].trim().toLowerCase();
      final value = kv[1].trim();
      if (key == 'fill') {
        fill = _parseCssColor(value);
      } else if (key == 'stroke') {
        stroke = _parseCssColor(value);
      } else if (key == 'color') {
        color = _parseCssColor(value);
      }
    }
    if (fill == null && stroke == null && color == null) return null;
    return _NodeColors(fill: fill, stroke: stroke, color: color);
  }

  /// 解析 CSS 颜色值为 ARGB 整数。支持 #RGB、#RRGGBB、#RRGGBBAA 和常用颜色名。
  static int? _parseCssColor(String raw) {
    final value = raw.trim().toLowerCase();
    if (value.startsWith('#')) {
      final hex = value.substring(1);
      switch (hex.length) {
        case 3:
          final r = hex[0], g = hex[1], b = hex[2];
          return int.tryParse('ff$r$r$g$g$b$b', radix: 16);
        case 6:
          return int.tryParse('ff$hex', radix: 16);
        case 8:
          final rgb = hex.substring(0, 6);
          final alpha = hex.substring(6, 8);
          return int.tryParse('$alpha$rgb', radix: 16);
        default:
          return null;
      }
    }
    return _cssNamedColors[value];
  }

  static const Map<String, int> _cssNamedColors = {
    'red': 0xFFFF0000,
    'green': 0xFF008000,
    'blue': 0xFF0000FF,
    'white': 0xFFFFFFFF,
    'black': 0xFF000000,
    'yellow': 0xFFFFFF00,
    'orange': 0xFFFFA500,
    'purple': 0xFF800080,
    'pink': 0xFFFFC0CB,
    'gray': 0xFF808080,
    'grey': 0xFF808080,
    'cyan': 0xFF00FFFF,
    'magenta': 0xFFFF00FF,
    'lime': 0xFF00FF00,
    'navy': 0xFF000080,
    'teal': 0xFF008080,
    'maroon': 0xFF800000,
    'olive': 0xFF808000,
    'aqua': 0xFF00FFFF,
    'silver': 0xFFC0C0C0,
    'coral': 0xFFFF7F50,
    'gold': 0xFFFFD700,
    'lightblue': 0xFFADD8E6,
    'lightgreen': 0xFF90EE90,
    'lightyellow': 0xFFFFFFE0,
    'lightgray': 0xFFD3D3D3,
    'lightgrey': 0xFFD3D3D3,
    'darkblue': 0xFF00008B,
    'darkgreen': 0xFF006400,
    'darkred': 0xFF8B0000,
    'transparent': 0x00000000,
    'none': 0x00000000,
  };

  bool _isSkippableStateDirective(String statement) {
    final trimmed = statement.trimLeft().toLowerCase();
    return trimmed.startsWith('direction ') ||
        trimmed.startsWith('note ') ||
        trimmed.startsWith('classdef ') ||
        trimmed.startsWith('class ') ||
        trimmed.startsWith('style ') ||
        trimmed == '--' ||
        trimmed.contains('{') ||
        trimmed.contains('}');
  }

  _ParsedFlowSubgraph? _tryParseFlowSubgraph(
    String statement,
    int order,
    int depth,
  ) {
    if (!statement.startsWith('subgraph ')) {
      return null;
    }
    final body = statement.substring('subgraph '.length).trim();
    if (body.isEmpty) {
      return null;
    }

    // Try id["label"] or id["label"] pattern first (bracket-quoted label).
    final bracketMatch =
        RegExp(r'^(\S+)\s*\[(.+)\]\s*$').firstMatch(body);
    if (bracketMatch != null) {
      final id = bracketMatch.group(1)!;
      final label = _stripQuotedLabel(bracketMatch.group(2)!);
      return _ParsedFlowSubgraph(
        id: id,
        label: label.isEmpty ? id : label,
        order: order,
        depth: depth,
      );
    }

    // subgraph "Label With Spaces" — 整体是一个引号字符串，没有独立 id。
    // 必须在 explicit 之前判断，否则 `"emoji 中文"` 会被 \S+ 在第一个空格前截断，
    // 误把 `"emoji` 当作 id，把 `中文"` 当作 label（丢 emoji、多余引号）。
    if ((body.startsWith('"') && body.endsWith('"') && body.length >= 2) ||
        (body.startsWith("'") && body.endsWith("'") && body.length >= 2)) {
      final label = _normalizeFlowSubgraphLabel(body);
      return _ParsedFlowSubgraph(
        id: '_subgraph_$order',
        label: label.isEmpty ? '_subgraph_$order' : label,
        order: order,
        depth: depth,
      );
    }

    // Try id label (ASCII or Unicode ID followed by space and label text).
    final explicit = RegExp(r'^(\S+)\s+(.+)$').firstMatch(body);
    if (explicit != null) {
      final id = explicit.group(1)!;
      final label = _normalizeFlowSubgraphLabel(explicit.group(2)!);
      return _ParsedFlowSubgraph(
        id: id,
        label: label.isEmpty ? id : label,
        order: order,
        depth: depth,
      );
    }

    final label = _normalizeFlowSubgraphLabel(body);
    return _ParsedFlowSubgraph(
      id: '_subgraph_$order',
      label: label,
      order: order,
      depth: depth,
    );
  }

  String _normalizeFlowSubgraphLabel(String input) {
    final trimmed = input.trim();
    if ((trimmed.startsWith('[') && trimmed.endsWith(']')) ||
        (trimmed.startsWith('{') && trimmed.endsWith('}')) ||
        (trimmed.startsWith('(') && trimmed.endsWith(')'))) {
      return _stripQuotedLabel(trimmed.substring(1, trimmed.length - 1));
    }
    return _stripQuotedLabel(trimmed);
  }

  String _stripQuotedLabel(String input) {
    final trimmed = input.trim();
    final unquoted = (trimmed.length >= 2 &&
            ((trimmed.startsWith('"') && trimmed.endsWith('"')) ||
                (trimmed.startsWith("'") && trimmed.endsWith("'"))))
        ? trimmed.substring(1, trimmed.length - 1)
        : trimmed;
    return _convertHtmlLineBreaks(unquoted);
  }

  ChatMermaidFlowDirection _parseDirection(String token) {
    switch (token.toUpperCase()) {
      case 'BT':
        return ChatMermaidFlowDirection.bottomTop;
      case 'LR':
        return ChatMermaidFlowDirection.leftRight;
      case 'RL':
        return ChatMermaidFlowDirection.rightLeft;
      case 'TD':
      case 'TB':
        return ChatMermaidFlowDirection.topDown;
    }
    return ChatMermaidFlowDirection.topDown;
  }

  ChatMermaidSequenceNotePosition _parseNotePosition(String token) {
    switch (token) {
      case 'left of':
        return ChatMermaidSequenceNotePosition.leftOf;
      case 'right of':
        return ChatMermaidSequenceNotePosition.rightOf;
      case 'over':
        return ChatMermaidSequenceNotePosition.over;
    }
    return ChatMermaidSequenceNotePosition.over;
  }

  ChatMermaidSequenceMessageStyle _parseSequenceMessageStyle(String token) {
    switch (token) {
      case '-->>':
        return ChatMermaidSequenceMessageStyle.dashedArrow;
      case '-->':
        return ChatMermaidSequenceMessageStyle.dashedLine;
      case '->':
        return ChatMermaidSequenceMessageStyle.solidLine;
      case '->>':
        return ChatMermaidSequenceMessageStyle.solidArrow;
    }
    return ChatMermaidSequenceMessageStyle.solidArrow;
  }
}

class _FlowchartBuilder {
  _FlowchartBuilder({
    required this.direction,
  });

  final ChatMermaidFlowDirection direction;
  final Map<String, ChatMermaidNode> nodes = <String, ChatMermaidNode>{};
  final List<ChatMermaidEdge> edges = <ChatMermaidEdge>[];
  final Map<String, _MutableFlowSubgraph> subgraphs =
      <String, _MutableFlowSubgraph>{};

  /// classDef className → 颜色样式
  final Map<String, _NodeColors> _classDefs = <String, _NodeColors>{};

  /// style nodeId → 颜色样式（直接指定）
  final Map<String, _NodeColors> _nodeStyles = <String, _NodeColors>{};

  /// class nodeId → className 映射
  final Map<String, String> _nodeClassAssignments = <String, String>{};

  /// :::className 语法收集的映射
  final Map<String, String> _inlineClassAssignments = <String, String>{};

  int _nextNodeOrder = 0;
  int _nextEdgeOrder = 0;
  int _nextSubgraphOrder = 0;

  int nextSubgraphOrder() => _nextSubgraphOrder++;

  void addClassDef(String className, _NodeColors colors) {
    _classDefs[className] = colors;
  }

  void addNodeStyle(String nodeId, _NodeColors colors) {
    _nodeStyles[nodeId] = colors;
  }

  void assignClass(String nodeId, String className) {
    _nodeClassAssignments[nodeId] = className;
  }

  void addSubgraph(_ParsedFlowSubgraph subgraph) {
    subgraphs[subgraph.id] = _MutableFlowSubgraph(
      id: subgraph.id,
      label: subgraph.label,
      order: subgraph.order,
      depth: subgraph.depth,
    );
  }

  bool hasSubgraph(String id) => subgraphs.containsKey(id);

  void upsertNode(_ParsedNode node) {
    final existing = nodes[node.id];
    if (node.className != null) {
      _inlineClassAssignments[node.id] = node.className!;
    }
    if (existing == null) {
      nodes[node.id] = ChatMermaidNode(
        id: node.id,
        label: node.label ?? node.id,
        shape: node.shape ?? ChatMermaidNodeShape.rectangle,
        order: _nextNodeOrder++,
      );
      return;
    }

    nodes[node.id] = ChatMermaidNode(
      id: existing.id,
      label: node.label ?? existing.label,
      shape: node.shape ?? existing.shape,
      order: existing.order,
    );
  }

  _ResolvedFlowReference resolveReference(_ParsedNode node) {
    final isSubgraphReference = node.isBareReference &&
        hasSubgraph(node.id) &&
        !nodes.containsKey(node.id);
    if (isSubgraphReference) {
      return _ResolvedFlowReference(referenceId: node.id, isSubgraph: true);
    }
    upsertNode(node);
    return _ResolvedFlowReference(referenceId: node.id, isSubgraph: false);
  }

  void registerReferenceInActiveSubgraphs(
    _ResolvedFlowReference reference,
    List<String> activeSubgraphIds,
  ) {
    if (reference.isSubgraph) {
      return;
    }
    for (final subgraphId in activeSubgraphIds) {
      subgraphs[subgraphId]?.nodeIds.add(reference.referenceId);
    }
  }

  void addEdge({
    required String sourceId,
    required String targetId,
    required String? label,
    required ChatMermaidEdgeStyle style,
  }) {
    edges.add(
      ChatMermaidEdge(
        sourceId: sourceId,
        targetId: targetId,
        label: label,
        style: style,
        order: _nextEdgeOrder++,
      ),
    );
  }

  /// 解析节点的最终颜色。优先级: style > class/:::className > classDef default。
  _NodeColors? _resolveNodeColors(String nodeId) {
    // style 直接指定最高优先级
    final direct = _nodeStyles[nodeId];
    if (direct != null) return direct;
    // class 语句 > :::className 内联
    final className =
        _nodeClassAssignments[nodeId] ?? _inlineClassAssignments[nodeId];
    if (className != null) {
      return _classDefs[className];
    }
    // 检查 default classDef
    return _classDefs['default'];
  }

  ChatMermaidFlowchart build() {
    // 将颜色应用到节点
    final coloredNodes = nodes.map((id, node) {
      final colors = _resolveNodeColors(id);
      if (colors == null) return MapEntry(id, node);
      return MapEntry(
        id,
        ChatMermaidNode(
          id: node.id,
          label: node.label,
          shape: node.shape,
          order: node.order,
          fillColor: colors.fill,
          strokeColor: colors.stroke,
          textColor: colors.color,
        ),
      );
    });

    final sortedNodes = coloredNodes.values.toList()
      ..sort((left, right) => left.order.compareTo(right.order));
    final sortedSubgraphs = subgraphs.values.toList()
      ..sort((left, right) => left.order.compareTo(right.order));
    return ChatMermaidFlowchart(
      direction: direction,
      nodes: List.unmodifiable(sortedNodes),
      edges: List.unmodifiable(edges),
      subgraphs: List.unmodifiable(
        sortedSubgraphs.map((subgraph) {
          final nodeIds = subgraph.nodeIds.toList(growable: false);
          return ChatMermaidFlowSubgraph(
            id: subgraph.id,
            label: subgraph.label,
            order: subgraph.order,
            depth: subgraph.depth,
            nodeIds: nodeIds,
          );
        }),
      ),
    );
  }
}

class _SequenceBuilder {
  final Map<String, ChatMermaidSequenceParticipant> participants =
      <String, ChatMermaidSequenceParticipant>{};
  final List<ChatMermaidSequenceEvent> events = <ChatMermaidSequenceEvent>[];
  int _nextParticipantOrder = 0;
  int _nextEventOrder = 0;

  int nextOrder() => _nextEventOrder++;

  void upsertParticipant(_ParsedSequenceParticipant participant) {
    final existing = participants[participant.id];
    if (existing == null) {
      participants[participant.id] = ChatMermaidSequenceParticipant(
        id: participant.id,
        label: participant.label,
        order: _nextParticipantOrder++,
        isActor: participant.isActor,
      );
      return;
    }

    participants[participant.id] = ChatMermaidSequenceParticipant(
      id: existing.id,
      label: participant.label.isEmpty ? existing.label : participant.label,
      order: existing.order,
      isActor: existing.isActor || participant.isActor,
    );
  }

  void ensureParticipant(String id) {
    final existing = participants[id];
    if (existing != null) {
      return;
    }
    participants[id] = ChatMermaidSequenceParticipant(
      id: id,
      label: id,
      order: _nextParticipantOrder++,
    );
  }

  void addEvent(ChatMermaidSequenceEvent event) {
    switch (event) {
      case ChatMermaidSequenceMessage():
        events.add(
          ChatMermaidSequenceMessage(
            order: nextOrder(),
            fromId: event.fromId,
            toId: event.toId,
            label: event.label,
            style: event.style,
          ),
        );
        break;
      case ChatMermaidSequenceNote():
        events.add(
          ChatMermaidSequenceNote(
            order: nextOrder(),
            position: event.position,
            targetIds: event.targetIds,
            text: event.text,
          ),
        );
        break;
      case ChatMermaidSequenceGroupStart():
        events.add(
          ChatMermaidSequenceGroupStart(
            order: nextOrder(),
            kind: event.kind,
            label: event.label,
          ),
        );
        break;
      case ChatMermaidSequenceGroupDivider():
        events.add(
          ChatMermaidSequenceGroupDivider(
            order: nextOrder(),
            label: event.label,
          ),
        );
        break;
      case ChatMermaidSequenceGroupEnd():
        events.add(ChatMermaidSequenceGroupEnd(order: nextOrder()));
        break;
    }
  }

  ChatMermaidSequenceDiagram build() {
    final sortedParticipants = participants.values.toList()
      ..sort((left, right) => left.order.compareTo(right.order));
    return ChatMermaidSequenceDiagram(
      participants: List.unmodifiable(sortedParticipants),
      events: List.unmodifiable(events),
    );
  }
}

class _StateBuilder {
  final Map<String, ChatMermaidStateNode> nodes =
      <String, ChatMermaidStateNode>{};
  final List<ChatMermaidStateTransition> transitions =
      <ChatMermaidStateTransition>[];
  int _nextNodeOrder = 0;
  int _nextTransitionOrder = 0;

  void upsertNode(_ParsedStateNode node) {
    final existing = nodes[node.id];
    if (existing == null) {
      nodes[node.id] = ChatMermaidStateNode(
        id: node.id,
        label: node.label,
        kind: node.kind,
        order: _nextNodeOrder++,
      );
      return;
    }

    nodes[node.id] = ChatMermaidStateNode(
      id: existing.id,
      label: node.label.isEmpty ? existing.label : node.label,
      kind: node.kind == ChatMermaidStateNodeKind.regular
          ? existing.kind
          : node.kind,
      order: existing.order,
    );
  }

  void addTransition(_ParsedStateTransition transition) {
    upsertNode(transition.source);
    upsertNode(transition.target);
    transitions.add(
      ChatMermaidStateTransition(
        sourceId: transition.source.id,
        targetId: transition.target.id,
        label: transition.label,
        order: _nextTransitionOrder++,
      ),
    );
  }

  ChatMermaidStateDiagram build() {
    final sortedNodes = nodes.values.toList()
      ..sort((left, right) => left.order.compareTo(right.order));
    return ChatMermaidStateDiagram(
      nodes: List.unmodifiable(sortedNodes),
      transitions: List.unmodifiable(transitions),
    );
  }
}

class _FlowchartStatementParser {
  _FlowchartStatementParser(this.input);

  final String input;
  int _index = 0;

  bool get isDone {
    _skipWhitespace();
    return _index >= input.length;
  }

  bool get hasTrailingGarbage => _index < input.length;

  _ParsedNode? parseNode() {
    final node = _parseNodeCore();
    if (node == null) {
      return null;
    }
    // 解析 :::className 类简写，将类名记录到节点上。
    if (_peek(':::')) {
      _index += 3;
      final className = _readIdentifier();
      if (className != null && className.isNotEmpty) {
        return _ParsedNode(
          id: node.id,
          label: node.label,
          shape: node.shape,
          isBareReference: node.isBareReference,
          className: className,
        );
      }
    }
    return node;
  }

  _ParsedNode? _parseNodeCore() {
    _skipWhitespace();
    final id = _readIdentifier();
    if (id == null) {
      return null;
    }

    _skipWhitespace();
    if (_index >= input.length) {
      return _ParsedNode(id: id, isBareReference: true);
    }

    final char = input[_index];
    if (char == '@' && _peek('@{')) {
      // A@{ shape: rect, label: "x" } → mermaid v11 新形状语法。
      _index += 1; // 跳过 '@'
      final body = _readEnclosed('{', '}');
      if (body == null) {
        return null;
      }
      return _ParsedNode(
        id: id,
        label: _normalizeLabel(_parseAtNodeLabel(body) ?? id),
        shape: _parseAtNodeShape(body),
      );
    }
    if (char == '[') {
      // [[text]] → subroutine
      if (_peek('[[')) {
        final label = _readDoubleEnclosed('[', ']');
        if (label == null) {
          return null;
        }
        return _ParsedNode(
          id: id,
          label: _normalizeLabel(label),
          shape: ChatMermaidNodeShape.subroutine,
        );
      }
      // [(text)] → cylindrical/database
      if (_peek('[(')) {
        _index += 1; // skip '['
        final label = _readEnclosed('(', ')');
        if (label == null) {
          return null;
        }
        if (_index < input.length && input[_index] == ']') {
          _index += 1;
        }
        return _ParsedNode(
          id: id,
          label: _normalizeLabel(label),
          shape: ChatMermaidNodeShape.cylindrical,
        );
      }
      // [text] → rectangle
      final label = _readEnclosed('[', ']');
      if (label == null) {
        return null;
      }
      return _ParsedNode(
        id: id,
        label: _normalizeLabel(label),
        shape: ChatMermaidNodeShape.rectangle,
      );
    }
    if (char == '{') {
      // {{text}} → hexagon
      if (_peek('{{')) {
        final label = _readDoubleEnclosed('{', '}');
        if (label == null) {
          return null;
        }
        return _ParsedNode(
          id: id,
          label: _normalizeLabel(label),
          shape: ChatMermaidNodeShape.hexagon,
        );
      }
      // {text} → diamond
      final label = _readEnclosed('{', '}');
      if (label == null) {
        return null;
      }
      return _ParsedNode(
        id: id,
        label: _normalizeLabel(label),
        shape: ChatMermaidNodeShape.diamond,
      );
    }
    if (char == '(') {
      // ([text]) → stadium
      if (_peek('([')) {
        _index += 1; // skip '('
        final label = _readEnclosed('[', ']');
        if (label == null) {
          return null;
        }
        if (_index < input.length && input[_index] == ')') {
          _index += 1;
        }
        return _ParsedNode(
          id: id,
          label: _normalizeLabel(label),
          shape: ChatMermaidNodeShape.stadium,
        );
      }
      // (((text))) → double circle(模型无双圆形,映射为 circle)
      if (_peek('(((')) {
        final label = _readTripleEnclosed('(', ')');
        if (label == null) {
          return null;
        }
        return _ParsedNode(
          id: id,
          label: _normalizeLabel(label),
          shape: ChatMermaidNodeShape.circle,
        );
      }
      // ((text)) → circle
      if (_peek('((')) {
        final label = _readDoubleEnclosed('(', ')');
        if (label == null) {
          return null;
        }
        return _ParsedNode(
          id: id,
          label: _normalizeLabel(label),
          shape: ChatMermaidNodeShape.circle,
        );
      }
      // (text) → rounded
      final label = _readEnclosed('(', ')');
      if (label == null) {
        return null;
      }
      return _ParsedNode(
        id: id,
        label: _normalizeLabel(label),
        shape: ChatMermaidNodeShape.rounded,
      );
    }
    if (char == '>') {
      // >text] → asymmetric (render as rectangle)
      _index += 1;
      final start = _index;
      while (_index < input.length && input[_index] != ']') {
        _index += 1;
      }
      if (_index >= input.length) {
        return null;
      }
      final label = input.substring(start, _index).trim();
      _index += 1;
      return _ParsedNode(
        id: id,
        label: _normalizeLabel(label),
        shape: ChatMermaidNodeShape.rectangle,
      );
    }

    return _ParsedNode(id: id, isBareReference: true);
  }

  _ParsedEdge? parseEdge() {
    _skipWhitespace();

    // 去除边 ID 前缀(e1@-->):边的样式/动画不在解析层处理,仅丢弃 id。
    final edgeId =
        RegExp(r'^[A-Za-z0-9_]+@(?=[-=<])').firstMatch(input.substring(_index));
    if (edgeId != null) {
      _index += edgeId.group(0)!.length;
      _skipWhitespace();
    }

    // 纯操作符(连接符中不夹文字)优先匹配,长度容忍(额外的 -、=、. 表示更长的边);
    // 必须先于内联文字边,否则 A-->B-->C 会被误当成带标签 ">B" 的单条边。
    // 匹配成功后再尝试紧随其后的 |label| 管道标签。
    final rest = input.substring(_index);
    for (final entry in _plainEdgePatterns) {
      final match = entry.pattern.firstMatch(rest);
      if (match == null) {
        continue;
      }
      _index += match.group(0)!.length;
      _skipWhitespace();
      String? label;
      if (_index < input.length && input[_index] == '|') {
        label = _readPipeLabel();
        if (label == null) {
          return null;
        }
        _skipWhitespace();
      }
      return _ParsedEdge(label: label, style: entry.style);
    }

    // 内联文字边(文字嵌在连接符中间):-- t -->、-. t .->、== t ==>
    final edgeWithTextMatch = RegExp(
      r'^--\s*(?:\|([^|]+)\||([^|]+?))\s*(-{2,}>|-{2,}o|-{2,}x|-{3,})',
    ).firstMatch(input.substring(_index));
    if (edgeWithTextMatch != null) {
      final pipeLabel = edgeWithTextMatch.group(1);
      final spaceLabel = edgeWithTextMatch.group(2);
      final labelText = _normalizeLabel(pipeLabel ?? spaceLabel ?? '');
      final operator = edgeWithTextMatch.group(3)!;
      _index += edgeWithTextMatch.group(0)!.length;

      final ChatMermaidEdgeStyle style;
      if (operator.endsWith('>')) {
        style = ChatMermaidEdgeStyle.solidArrow;
      } else if (operator.endsWith('o')) {
        style = ChatMermaidEdgeStyle.circle;
      } else if (operator.endsWith('x')) {
        style = ChatMermaidEdgeStyle.cross;
      } else {
        style = ChatMermaidEdgeStyle.solidLine;
      }

      _skipWhitespace();
      return _ParsedEdge(
        label: labelText.isNotEmpty ? labelText : null,
        style: style,
      );
    }

    // Try dotted edge with inline text label: -. text .-> or -.|text|.->
    // 对齐 mermaid 官方 "Dotted link with text" 语法,映射为带文字的虚线箭头。
    final dottedWithTextMatch = RegExp(
      r'^-\.\s*(?:\|([^|]+)\||([^|]+?))\s*\.-*->',
    ).firstMatch(input.substring(_index));
    if (dottedWithTextMatch != null) {
      final labelText = _normalizeLabel(
        dottedWithTextMatch.group(1) ?? dottedWithTextMatch.group(2) ?? '',
      );
      _index += dottedWithTextMatch.group(0)!.length;
      _skipWhitespace();
      return _ParsedEdge(
        label: labelText.isNotEmpty ? labelText : null,
        style: ChatMermaidEdgeStyle.dashedArrow,
      );
    }

    // Try thick edge with inline text label: == text ==> or ==|text|==>
    // 对齐 mermaid 官方 "Thick link with text" 语法,映射为带文字的粗线箭头。
    final thickWithTextMatch = RegExp(
      r'^==\s*(?:\|([^|]+)\||([^|]+?))\s*=+>',
    ).firstMatch(input.substring(_index));
    if (thickWithTextMatch != null) {
      final labelText = _normalizeLabel(
        thickWithTextMatch.group(1) ?? thickWithTextMatch.group(2) ?? '',
      );
      _index += thickWithTextMatch.group(0)!.length;
      _skipWhitespace();
      return _ParsedEdge(
        label: labelText.isNotEmpty ? labelText : null,
        style: ChatMermaidEdgeStyle.thickArrow,
      );
    }

    // Fallback: treat bare `--` as solidLine (lenient parsing for `---` shorthand)
    if (_peek('--')) {
      _index += 2;
      _skipWhitespace();
      return const _ParsedEdge(
          label: null, style: ChatMermaidEdgeStyle.solidLine);
    }

    return null;
  }

  String? _readIdentifier() {
    if (_index >= input.length) {
      return null;
    }
    final start = _index;
    final first = input.codeUnitAt(_index);
    if (!_isIdentifierStart(first)) {
      return null;
    }
    _index += 1;
    while (_index < input.length) {
      final codeUnit = input.codeUnitAt(_index);
      if (_isIdentifierWordChar(codeUnit)) {
        _index += 1;
        continue;
      }
      // '-' 与 '.' 仅在两侧都是单词字符时才算 id 内部(如 my-node、a.b);
      // 若其后不是单词字符,则它是边操作符的起始(如 -->、---、-.->),应停止。
      if (codeUnit == 0x2D || codeUnit == 0x2E) {
        if (_index + 1 < input.length &&
            _isIdentifierWordChar(input.codeUnitAt(_index + 1))) {
          _index += 1;
          continue;
        }
      }
      break;
    }
    return input.substring(start, _index);
  }

  String? _readEnclosed(String open, String close) {
    if (_index >= input.length || input[_index] != open) {
      return null;
    }
    _index += 1;
    final start = _index;
    var depth = 1;

    while (_index < input.length) {
      final char = input[_index];
      if (char == open) {
        depth += 1;
      } else if (char == close) {
        depth -= 1;
        if (depth == 0) {
          final text = input.substring(start, _index);
          _index += 1;
          return text;
        }
      }
      _index += 1;
    }
    return null;
  }

  String? _readDoubleEnclosed(String open, String close) {
    if (!_peek('$open$open')) {
      return null;
    }
    _index += 2;
    final start = _index;

    while (_index + 1 < input.length) {
      final pair = input.substring(_index, _index + 2);
      if (pair == '$close$close') {
        final text = input.substring(start, _index);
        _index += 2;
        return text;
      }
      _index += 1;
    }
    return null;
  }

  String? _readTripleEnclosed(String open, String close) {
    final opener = '$open$open$open';
    final closer = '$close$close$close';
    if (!_peek(opener)) {
      return null;
    }
    _index += 3;
    final start = _index;

    while (_index + 2 < input.length) {
      if (input.substring(_index, _index + 3) == closer) {
        final text = input.substring(start, _index);
        _index += 3;
        return text;
      }
      _index += 1;
    }
    return null;
  }

  String? _readPipeLabel() {
    if (_index >= input.length || input[_index] != '|') {
      return null;
    }
    _index += 1;
    final start = _index;
    while (_index < input.length) {
      if (input[_index] == '|') {
        final text = input.substring(start, _index).trim();
        _index += 1;
        return text.isEmpty ? null : _normalizeLabel(text);
      }
      _index += 1;
    }
    return null;
  }

  bool consumeAnd() {
    _skipWhitespace();
    if (_index < input.length && input[_index] == '&') {
      _index += 1;
      return true;
    }
    return false;
  }

  bool _peek(String value) => input.startsWith(value, _index);

  void _skipWhitespace() {
    while (_index < input.length) {
      final char = input[_index];
      if (char != ' ' && char != '\t') {
        break;
      }
      _index += 1;
    }
  }

  // 允许 ASCII 字母/数字/下划线 以及非 ASCII Unicode 字符(中文、日文等)作为 ID。
  bool _isIdentifierStart(int codeUnit) {
    return (codeUnit >= 0x30 && codeUnit <= 0x39) || // 0-9
        (codeUnit >= 0x41 && codeUnit <= 0x5A) || // A-Z
        (codeUnit >= 0x61 && codeUnit <= 0x7A) || // a-z
        codeUnit == 0x5F || // _
        codeUnit >= 0x80; // 非 ASCII Unicode(中文、日韩、阿拉伯文等)
  }

  bool _isIdentifierWordChar(int codeUnit) => _isIdentifierStart(codeUnit);

  // 解析 @{ shape: x } 中的形状名并映射到已支持形状(其余归一为 rectangle)。
  ChatMermaidNodeShape _parseAtNodeShape(String body) {
    final match = RegExp(r'shape\s*:\s*([A-Za-z0-9_-]+)').firstMatch(body);
    final name = match?.group(1)?.toLowerCase() ?? '';
    switch (name) {
      case 'rounded':
      case 'event':
        return ChatMermaidNodeShape.rounded;
      case 'stadium':
      case 'pill':
      case 'terminal':
        return ChatMermaidNodeShape.stadium;
      case 'circle':
      case 'circ':
      case 'sm-circ':
      case 'dbl-circ':
      case 'fr-circ':
        return ChatMermaidNodeShape.circle;
      case 'diam':
      case 'diamond':
      case 'decision':
      case 'question':
        return ChatMermaidNodeShape.diamond;
      case 'hex':
      case 'hexagon':
      case 'prepare':
        return ChatMermaidNodeShape.hexagon;
      case 'cyl':
      case 'cylinder':
      case 'db':
      case 'database':
        return ChatMermaidNodeShape.cylindrical;
      case 'subproc':
      case 'subprocess':
      case 'subroutine':
      case 'fr-rect':
        return ChatMermaidNodeShape.subroutine;
      default:
        return ChatMermaidNodeShape.rectangle;
    }
  }

  // 解析 @{ label: "x" } 或 @{ label: x } 中的标签。
  String? _parseAtNodeLabel(String body) {
    final quoted =
        RegExp('''label\\s*:\\s*(?:"([^"]*)"|'([^']*)')''').firstMatch(body);
    if (quoted != null) {
      return quoted.group(1) ?? quoted.group(2);
    }
    final bare = RegExp(r'label\s*:\s*([^,}]+)').firstMatch(body);
    return bare?.group(1)?.trim();
  }

  String _normalizeLabel(String input) {
    final trimmed = input.trim();
    final unquoted = (trimmed.length >= 2 &&
            ((trimmed.startsWith('"') && trimmed.endsWith('"')) ||
                (trimmed.startsWith("'") && trimmed.endsWith("'"))))
        ? trimmed.substring(1, trimmed.length - 1)
        : trimmed;
    return ChatMermaidParser._convertHtmlLineBreaks(unquoted);
  }
}

class _ParsedNode {
  const _ParsedNode({
    required this.id,
    this.label,
    this.shape,
    this.isBareReference = false,
    this.className,
  });

  final String id;
  final String? label;
  final ChatMermaidNodeShape? shape;
  final bool isBareReference;
  final String? className;
}

class _ParsedEdge {
  const _ParsedEdge({
    required this.label,
    required this.style,
  });

  final String? label;
  final ChatMermaidEdgeStyle style;
}

/// 节点颜色样式对，来自 classDef/style 指令。
class _NodeColors {
  const _NodeColors({this.fill, this.stroke, this.color});
  final int? fill;
  final int? stroke;
  final int? color;
}

class _PlainEdgePattern {
  const _PlainEdgePattern(this.pattern, this.style);

  final RegExp pattern;
  final ChatMermaidEdgeStyle style;
}

class _ParsedSequenceParticipant {
  const _ParsedSequenceParticipant({
    required this.id,
    required this.label,
    required this.isActor,
  });

  final String id;
  final String label;
  final bool isActor;
}

class _ParsedFlowSubgraph {
  const _ParsedFlowSubgraph({
    required this.id,
    required this.label,
    required this.order,
    required this.depth,
  });

  final String id;
  final String label;
  final int order;
  final int depth;
}

class _ParsedStateNode {
  const _ParsedStateNode({
    required this.id,
    required this.label,
    this.kind = ChatMermaidStateNodeKind.regular,
  });

  final String id;
  final String label;
  final ChatMermaidStateNodeKind kind;
}

class _ParsedStateTransition {
  const _ParsedStateTransition({
    required this.source,
    required this.target,
    required this.label,
  });

  final _ParsedStateNode source;
  final _ParsedStateNode target;
  final String? label;
}

class _ParsedGanttTask {
  const _ParsedGanttTask.absolute({
    required this.id,
    required this.label,
    required this.startDate,
    required this.durationDays,
  }) : dependencyId = null;

  const _ParsedGanttTask.afterDependency({
    required this.id,
    required this.label,
    required this.dependencyId,
    required this.durationDays,
  }) : startDate = null;

  final String? id;
  final String label;
  final DateTime? startDate;
  final String? dependencyId;
  final int durationDays;

  String get startToken =>
      startDate?.toIso8601String() ?? 'after $dependencyId';
}

class _MutableGanttSection {
  _MutableGanttSection({
    required this.title,
  });

  final String title;
  final List<_ParsedGanttTask> tasks = <_ParsedGanttTask>[];
}

class _MutableFlowSubgraph {
  _MutableFlowSubgraph({
    required this.id,
    required this.label,
    required this.order,
    required this.depth,
  });

  final String id;
  final String label;
  final int order;
  final int depth;
  final Set<String> nodeIds = <String>{};
}

class _ResolvedFlowReference {
  const _ResolvedFlowReference({
    required this.referenceId,
    required this.isSubgraph,
  });

  final String referenceId;
  final bool isSubgraph;
}

// 纯边操作符的长度容忍匹配,按从最特定到最一般的顺序尝试。
// 双向/双端箭头(<-->、o--o、x--x 等)模型无专用样式,
// 映射到最接近的单端样式(渲染正常,仅缺少回指箭头);
// 这些前导带 <、o、x 的形式需先于普通 - 形式匹配。
// 点线(-.+)需先于实线(-),箭头形式需先于无箭头线形式。
final List<_PlainEdgePattern> _plainEdgePatterns = <_PlainEdgePattern>[
  // 双向/双端(前导 <、o、x)
  _PlainEdgePattern(RegExp(r'^<-\.+->'), ChatMermaidEdgeStyle.dashedArrow),
  _PlainEdgePattern(RegExp(r'^<={2,}>'), ChatMermaidEdgeStyle.thickArrow),
  _PlainEdgePattern(RegExp(r'^<-{2,}>'), ChatMermaidEdgeStyle.solidArrow),
  _PlainEdgePattern(RegExp(r'^o-{2,}o'), ChatMermaidEdgeStyle.circle),
  _PlainEdgePattern(RegExp(r'^x-{2,}x'), ChatMermaidEdgeStyle.cross),
  _PlainEdgePattern(RegExp(r'^o-{2,}>'), ChatMermaidEdgeStyle.circle),
  _PlainEdgePattern(RegExp(r'^x-{2,}>'), ChatMermaidEdgeStyle.cross),
  // 单向(长度容忍)
  _PlainEdgePattern(RegExp(r'^-\.+->'), ChatMermaidEdgeStyle.dashedArrow),
  _PlainEdgePattern(RegExp(r'^-\.+-'), ChatMermaidEdgeStyle.solidLine),
  _PlainEdgePattern(RegExp(r'^={2,}>'), ChatMermaidEdgeStyle.thickArrow),
  _PlainEdgePattern(RegExp(r'^={3,}'), ChatMermaidEdgeStyle.solidLine),
  _PlainEdgePattern(RegExp(r'^-{2,}>'), ChatMermaidEdgeStyle.solidArrow),
  _PlainEdgePattern(RegExp(r'^-{2,}o'), ChatMermaidEdgeStyle.circle),
  _PlainEdgePattern(RegExp(r'^-{2,}x'), ChatMermaidEdgeStyle.cross),
  _PlainEdgePattern(RegExp(r'^-{3,}'), ChatMermaidEdgeStyle.solidLine),
];

const Map<String, ChatMermaidSequenceGroupKind> _sequenceGroupPrefixes =
    <String, ChatMermaidSequenceGroupKind>{
  'loop': ChatMermaidSequenceGroupKind.loop,
  'alt': ChatMermaidSequenceGroupKind.alt,
  'opt': ChatMermaidSequenceGroupKind.opt,
  'par': ChatMermaidSequenceGroupKind.par,
  'critical': ChatMermaidSequenceGroupKind.critical,
  'break': ChatMermaidSequenceGroupKind.breakBlock,
};

const String _stateStartId = '__state_start__';
const String _stateEndId = '__state_end__';
final RegExp _ganttIdPattern = RegExp(r'^[A-Za-z0-9_.-]+$');
/// Known Gantt directives that do not affect our rendering and should be
/// silently skipped during parsing.
const Set<String> _ignoredGanttDirectives = <String>{
  'excludes',
  'todayMarker',
  'tickInterval',
  'weekday',
  'weekend',
  'displayMode',
  'compact',
};

class _MutableClassItem {
  _MutableClassItem({required this.id, String? label})
      : label = label ?? id;

  final String id;
  String label;
  final List<String> members = <String>[];
}

class _MindmapLine {
  const _MindmapLine({required this.indent, required this.text});

  final int indent;
  final String text;
}

class _ParsedMindmapNode {
  const _ParsedMindmapNode({required this.label, required this.shape});

  final String label;
  final ChatMermaidNodeShape shape;
}

class _MutableJourneySection {
  _MutableJourneySection({required this.title, required this.order});

  final String title;
  final int order;
  final List<ChatMermaidJourneyTask> tasks = <ChatMermaidJourneyTask>[];
}

class _RawRadarCurve {  _RawRadarCurve({
    required this.id,
    required this.label,
    required this.order,
    this.positional,
    this.keyed,
  });

  final String id;
  final String label;
  final int order;
  final List<double>? positional;
  final Map<String, double>? keyed;
}

class _MutableKanbanColumn {
  _MutableKanbanColumn({
    required this.id,
    required this.title,
    required this.order,
  });

  final String id;
  final String title;
  final int order;
  final List<ChatMermaidKanbanItem> items = <ChatMermaidKanbanItem>[];
}

class _MutableBlockItem {
  _MutableBlockItem({
    required this.id,
    required this.label,
    required this.shape,
    required this.width,
    required this.isSpace,
    required this.isComposite,
    required this.explicitColumns,
    required this.order,
  });

  final String id;
  final String label;
  final ChatMermaidNodeShape shape;
  final int width;
  final bool isSpace;
  final bool isComposite;
  int? explicitColumns;
  int firstRowCount = 0;
  final int order;
  final List<_MutableBlockItem> children = <_MutableBlockItem>[];
}

class _RawTreemapLine {  const _RawTreemapLine({
    required this.indent,
    required this.label,
    required this.value,
  });

  final int indent;
  final String label;
  final double? value;
}

class _MutableTreemapNode {
  _MutableTreemapNode({
    required this.label,
    required this.explicitValue,
    required this.order,
  });

  final String label;
  final double? explicitValue;
  final int order;
  int indent = 0;
  final List<_MutableTreemapNode> children = <_MutableTreemapNode>[];
}

class _MutableTimelineSection {  _MutableTimelineSection({required this.title, required this.order});

  final String title;
  final int order;
  final List<_MutableTimelinePeriod> periods = <_MutableTimelinePeriod>[];
}

class _MutableTimelinePeriod {
  _MutableTimelinePeriod({
    required this.label,
    required this.events,
    required this.order,
  });

  final String label;
  final List<String> events;
  final int order;
}

  ChatMermaidParseResult _parseXyChart(List<String> lines) {
    String title = '';
    String xAxisTitle = '';
    List<String> xAxisLabels = [];
    String yAxisTitle = '';
    double yAxisMax = 0;
    double yAxisMin = 0;
    bool yAxisMinSet = false;
    List<List<double>> barSeries = [];
    List<List<double>> lineSeries = [];

    // Parse horizontal from first line
    final firstLine = lines.first.trim();
    final horizontal = firstLine.endsWith('horizontal');

    for (final rawLine in lines.skip(1)) {
      final line = rawLine.trim();
      if (line.isEmpty || line.startsWith('%%')) continue;

      if (line.startsWith('title')) {
        title = _extractQuoted(line.substring(5));
        continue;
      }

      if (line.startsWith('x-axis')) {
        final rest = line.substring(6).trim();
        // Pattern: "title" [label1, label2, ...]
        final bracketStart = rest.indexOf('[');
        if (bracketStart >= 0) {
          xAxisTitle = _extractQuoted(rest.substring(0, bracketStart).trim());
          final bracketEnd = rest.indexOf(']', bracketStart);
          if (bracketEnd > bracketStart) {
            final inner = rest.substring(bracketStart + 1, bracketEnd);
            xAxisLabels = inner
                .split(',')
                .map((s) => _extractQuoted(s.trim()))
                .where((s) => s.isNotEmpty)
                .toList();
          }
        } else {
          // Check for numeric range: "title" min --> max
          final arrowIdx = rest.indexOf('-->');
          if (arrowIdx >= 0) {
            final beforeArrow = rest.substring(0, arrowIdx).trim();
            final afterArrow = rest.substring(arrowIdx + 3).trim();
            // Extract title and min from beforeArrow
            final lastSpace = beforeArrow.lastIndexOf(RegExp(r'\s'));
            if (lastSpace >= 0) {
              xAxisTitle =
                  _extractQuoted(beforeArrow.substring(0, lastSpace).trim());
              final rangeMin =
                  double.tryParse(beforeArrow.substring(lastSpace + 1)) ?? 0;
              final rangeMax = double.tryParse(afterArrow) ?? 0;
              xAxisLabels = _generateNumericLabels(rangeMin, rangeMax);
            }
          } else {
            xAxisTitle = _extractQuoted(rest);
          }
        }
        continue;
      }

      if (line.startsWith('y-axis')) {
        final rest = line.substring(6).trim();
        // Check for range: "title" min --> max
        final arrowIdx = rest.indexOf('-->');
        if (arrowIdx >= 0) {
          final beforeArrow = rest.substring(0, arrowIdx).trim();
          final afterArrow = rest.substring(arrowIdx + 3).trim();
          // Extract title and min from beforeArrow
          final lastSpace = beforeArrow.lastIndexOf(RegExp(r'\s'));
          if (lastSpace >= 0) {
            yAxisTitle =
                _extractQuoted(beforeArrow.substring(0, lastSpace).trim());
            yAxisMin =
                double.tryParse(beforeArrow.substring(lastSpace + 1)) ?? 0;
            yAxisMinSet = true;
          } else {
            // No title, just min value
            yAxisMin = double.tryParse(beforeArrow) ?? 0;
            yAxisMinSet = true;
          }
          yAxisMax = double.tryParse(afterArrow) ?? 0;
        } else {
          // Pattern: "title" maxValue
          final quotedEnd = rest.indexOf('"', 1);
          if (quotedEnd > 0) {
            final afterQuoted = rest.substring(quotedEnd + 1).trim();
            yAxisTitle = _extractQuoted(rest.substring(0, quotedEnd + 1));
            if (afterQuoted.isNotEmpty) {
              yAxisMax = double.tryParse(afterQuoted) ?? 0;
            }
          } else {
            // Try "title" without quotes or just a number
            final parts = rest.split(RegExp(r'\s+'));
            if (parts.isNotEmpty) {
              yAxisTitle = parts.first;
              if (parts.length > 1) {
                yAxisMax = double.tryParse(parts.last) ?? 0;
              }
            }
          }
        }
        continue;
      }

      if (line.startsWith('bar')) {
        final rest = line.substring(3).trim();
        barSeries.add(_parseNumberList(rest));
        continue;
      }

      if (line.startsWith('line')) {
        final rest = line.substring(4).trim();
        lineSeries.add(_parseNumberList(rest));
        continue;
      }
    }

    if (xAxisLabels.isEmpty && barSeries.isEmpty && lineSeries.isEmpty) {
      return ChatMermaidParseResult.unsupported(
        error: 'xychart: no data found',
      );
    }

    // Auto-calculate y-axis max if not explicitly set
    if (yAxisMax <= 0) {
      final allValues = [
        for (final s in barSeries) ...s,
        for (final s in lineSeries) ...s,
      ];
      if (allValues.isNotEmpty) {
        yAxisMax = allValues.reduce((a, b) => a > b ? a : b) * 1.15;
      }
    }

    return ChatMermaidParseResult.supported(
      diagram: ChatMermaidXyChartDiagram(
        title: title,
        xAxisTitle: xAxisTitle,
        xAxisLabels: xAxisLabels,
        yAxisTitle: yAxisTitle,
        yAxisMax: yAxisMax,
        yAxisMin: yAxisMinSet ? yAxisMin : 0,
        horizontal: horizontal,
        barSeries: barSeries,
        lineSeries: lineSeries,
      ),
    );
  }

  /// Generate numeric labels for x-axis range (e.g. 2000 --> 2010).
  List<String> _generateNumericLabels(double min, double max) {
    if (max <= min) return [min.toInt().toString()];
    final range = max - min;
    // Pick a step that gives roughly 5-10 labels
    double step;
    if (range <= 10) {
      step = 1;
    } else if (range <= 50) {
      step = 5;
    } else if (range <= 100) {
      step = 10;
    } else {
      step = (range / 10).ceilToDouble();
    }
    final labels = <String>[];
    for (var v = min; v <= max; v += step) {
      labels.add(v == v.roundToDouble() ? v.toInt().toString() : v.toString());
    }
    // Always include max if not already there
    final maxLabel =
        max == max.roundToDouble() ? max.toInt().toString() : max.toString();
    if (labels.isEmpty || labels.last != maxLabel) {
      labels.add(maxLabel);
    }
    return labels;
  }

  String _extractQuoted(String s) {
    s = s.trim();
    if (s.startsWith('"') && s.endsWith('"') && s.length >= 2) {
      return s.substring(1, s.length - 1);
    }
    return s;
  }

  List<double> _parseNumberList(String s) {
    s = s.trim();
    if (s.startsWith('[')) s = s.substring(1);
    if (s.endsWith(']')) s = s.substring(0, s.length - 1);
    return s
        .split(',')
        .map((v) => v.trim())
        .where((v) => v.isNotEmpty)
        .map((v) => double.tryParse(v) ?? 0)
        .toList();
  }
