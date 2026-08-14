class MermaidImageExportResult {
  const MermaidImageExportResult({
    required this.location,
    required this.isDownload,
    this.isGallery = false,
  });

  final String location;
  final bool isDownload;
  final bool isGallery;
}
