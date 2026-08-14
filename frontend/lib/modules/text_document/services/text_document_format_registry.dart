class TextDocumentFormatRegistry {
  const TextDocumentFormatRegistry._();

  static const Set<String> markdownExtensions = {'md', 'markdown', 'mdx'};

  static const Set<String> textExtensions = {
    'txt',
    'text',
    'log',
    'js',
    'jsx',
    'ts',
    'tsx',
    'css',
    'scss',
    'less',
    'html',
    'htm',
    'vue',
    'svelte',
    'dart',
    'java',
    'kt',
    'kts',
    'swift',
    'm',
    'mm',
    'go',
    'rs',
    'c',
    'h',
    'cc',
    'cpp',
    'hpp',
    'cs',
    'py',
    'rb',
    'php',
    'sh',
    'bash',
    'zsh',
    'fish',
    'json',
    'jsonl',
    'xml',
    'yaml',
    'yml',
    'toml',
    'ini',
    'conf',
    'properties',
    'env',
    'csv',
    'sql',
    'graphql',
    'gql',
    'gradle',
  };

  static const Set<String> supportedExtensions = {
    ...markdownExtensions,
    ...textExtensions,
  };

  static bool isMarkdown(String fileName) =>
      markdownExtensions.contains(extensionOf(fileName));

  static bool isSupported({required String fileName, String mimeType = ''}) {
    if (supportedExtensions.contains(extensionOf(fileName))) return true;
    final mime = mimeType.trim().toLowerCase();
    return mime.startsWith('text/') ||
        mime == 'application/json' ||
        mime == 'application/xml' ||
        mime == 'application/javascript' ||
        mime == 'application/x-yaml';
  }

  static String extensionOf(String fileName) {
    final cleanName =
        Uri.tryParse(fileName)?.pathSegments.lastOrNull ?? fileName;
    final dot = cleanName.lastIndexOf('.');
    if (dot < 0 || dot == cleanName.length - 1) return '';
    return cleanName.substring(dot + 1).toLowerCase();
  }
}

extension<T> on List<T> {
  T? get lastOrNull => isEmpty ? null : last;
}
