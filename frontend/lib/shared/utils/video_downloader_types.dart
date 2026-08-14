/// 视频下载结果。
///
/// - [isGallery] 为真：已保存进系统相册（移动端），[location] 为占位标识。
/// - [isDownload] 为真：通过浏览器/系统下载器触发（Web），[location] 为文件名。
/// - 两者都为假：写入了具体文件路径（桌面端），[location] 为绝对路径。
class VideoDownloadResult {
  const VideoDownloadResult({
    required this.location,
    this.isDownload = false,
    this.isGallery = false,
  });

  final String location;
  final bool isDownload;
  final bool isGallery;
}
