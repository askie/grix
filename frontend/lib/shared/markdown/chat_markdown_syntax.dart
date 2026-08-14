import 'package:markdown/markdown.dart' as md;

class IndentedTableSyntax extends md.TableSyntax {
  static final RegExp _indentedHeaderPattern = RegExp(r'^ {4,}.*\|.*$');
  static final RegExp _indentedDelimiterPattern = RegExp(
    r'^ {4,}\|?([ \t]*:?\-+:?[ \t]*\|[ \t]*)+([ \t]|[ \t]*:?\-+:?[ \t]*)?$',
  );

  const IndentedTableSyntax();

  @override
  bool canParse(md.BlockParser parser) {
    return _indentedHeaderPattern.hasMatch(parser.current.content) &&
        parser.matchesNext(_indentedDelimiterPattern);
  }
}
