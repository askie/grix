enum ChatMarkdownSegmentType {
  text,
  fencedCode,
  inlineCode,
  linkDestination,
  imageDestination,
  referenceDefinition,
  htmlLike,
  escaped,
}

class ChatMarkdownSegment {
  const ChatMarkdownSegment({
    required this.type,
    required this.text,
    required this.start,
    required this.end,
    this.language,
    this.infoString,
    this.content,
    this.label,
    this.destination,
    this.fenceMarker,
    this.closed = true,
  });

  final ChatMarkdownSegmentType type;
  final String text;
  final int start;
  final int end;
  final String? language;
  final String? infoString;
  final String? content;
  final String? label;
  final String? destination;
  final String? fenceMarker;
  final bool closed;

  bool get isProtected => type != ChatMarkdownSegmentType.text;
}
