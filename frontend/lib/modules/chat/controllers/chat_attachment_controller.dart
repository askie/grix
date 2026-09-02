part of 'chat_controller.dart';

class PendingAttachmentUpload {
  const PendingAttachmentUpload({
    required this.type,
    required this.fileName,
    required this.contentType,
    this.bytes,
    this.stream,
    this.contentLength,
  });

  final ChatAttachmentType type;
  final String fileName;
  final String contentType;
  final Uint8List? bytes;
  final Stream<List<int>>? stream;
  final int? contentLength;

  bool get hasData => (bytes != null && bytes!.isNotEmpty) || stream != null;
  bool get isImage => type == ChatAttachmentType.image;
  bool get isVideo => type == ChatAttachmentType.video;
}

class _UploadedAttachmentResult {
  const _UploadedAttachmentResult({
    required this.objectKey,
    required this.attachment,
  });

  final String objectKey;
  final ChatMessageAttachment attachment;
}

class _ChatAttachmentController {
  const _ChatAttachmentController(this.owner);

  final ChatController owner;

  void toggleAttachmentMenu() {
    if (owner.isUploadingAttachment) {
      return;
    }
    owner.isAttachmentMenuOpen.toggle();
    if (owner.isAttachmentMenuOpen.value) {
      if (owner.focusNode.hasFocus) {
        owner._suppressMenuCloseOnFocusLoss = true;
      }
      FocusManager.instance.primaryFocus?.unfocus();
    }
  }

  void closeAttachmentMenu() {
    if (!owner.isAttachmentMenuOpen.value) {
      return;
    }
    owner.isAttachmentMenuOpen.value = false;
  }

  Future<void> pickRemoteFiles() async {
    closeAttachmentMenu();

    final session = owner.imService.findSessionById(owner.sessionId);
    if (session == null) return;

    String agentId;
    if (owner.isGroupChat) {
      agentId = owner.groupToolbarTargetAgentId.trim();
    } else {
      if (session.peerType != 2) return;
      agentId = session.peerId.trim();
    }
    if (agentId.isEmpty || agentId == '0') return;

    final ctx = Get.context;
    if (ctx == null) return;

    Future<RemoteFileListResult> listProvider(
      String? parentId,
      RemoteFileListQuery query,
    ) async {
      final resp = await owner.imService.requestAgentFileList(
        agentId: agentId,
        sessionId: owner.sessionId,
        parentId: parentId,
        showHidden: query.showHidden,
        allowedExtensions: query.allowedExtensions,
      );
      final nodes = resp.files.map((m) => mapAgentRemoteFileNode(m)).toList();
      return RemoteFileListResult(
        files: nodes,
        currentPath: resp.currentPath,
        machineName: resp.machineName,
      );
    }

    Future<RemoteFileNode> createFolderProvider(
      String? parentId,
      String name,
    ) async {
      final folder = await owner.imService.requestAgentCreateFolder(
        agentId: agentId,
        sessionId: owner.sessionId,
        parentId: parentId,
        name: name,
      );
      return RemoteFileNode(
        id: folder['id']?.toString() ?? '',
        name: folder['name']?.toString() ?? '',
        isDirectory: true,
      );
    }

    final agent = owner.agentService.agents.firstWhereOrNull(
      (a) => a.id == agentId,
    );
    final uploadBaseUrl = agent?.tailnetUploadBaseUrl;

    final result = await RemoteFilePicker.show(
      ctx,
      listProvider: listProvider,
      createFolderProvider: createFolderProvider,
      favoriteApi: UserFavoritePathService(),
      pickTarget: RemoteFilePickTarget.both,
      selectionMode: RemoteFileSelectionMode.multiple,
      // 记忆路径按 agent 区分，避免不同机器的 agent 共用同一 key 串台：
      // 否则切到另一台机器的 agent 时会用上一台的路径起始加载，列目录失败/
      // 取不到机器名，收藏夹便无法默认过滤到当前机器。
      storageKey: 'remote_file_picker_last_path_attach_$agentId',
      uploadBaseUrl: uploadBaseUrl?.isNotEmpty == true ? uploadBaseUrl : null,
    );
    if (result == null || result.selectedFiles.isEmpty) return;

    final paths = result.selectedFiles
        .map((f) => f.id.trim())
        .where((path) => path.isNotEmpty)
        .join(',');
    if (paths.isEmpty) return;
    owner.insertText(paths);
    owner.focusNode.requestFocus();
  }

  Future<void> pickAndSendImage() {
    return _pickImagesInternal(fromCamera: false);
  }

  Future<void> pickAndSendImageFromCamera() {
    return _pickImagesInternal(fromCamera: true);
  }

  Future<void> pickAndSendVideo() {
    return _pickVideosInternal(fromCamera: false);
  }

  Future<void> pickAndSendVideoFromCamera() {
    return _pickVideosInternal(fromCamera: true);
  }

  Future<void> pickAndSendFile() async {
    if (!_canStartPick()) {
      return;
    }

    try {
      // 按平台选筛选方式：Web 端用 FileType.any（file_picker 的 Web 自定义后缀
      // accept 串不可靠会静默吞掉 txt 等文件），选完再由 stageFileFromBytes 统一
      // 分流校验；原生端用自定义后缀获得更好的系统弹窗体验。
      final pickerConfig = ChatAttachmentPayloadBuilder.filePickerConfig(
        isWeb: kIsWeb,
      );
      final result = await FilePicker.platform.pickFiles(
        allowMultiple: true,
        type: pickerConfig.type,
        allowedExtensions: pickerConfig.allowedExtensions,
        withData: true,
      );
      if (result == null || result.files.isEmpty) {
        return;
      }

      for (final file in result.files) {
        final bytes = file.bytes;
        if (bytes == null || bytes.isEmpty) {
          // 选到的文件没读到字节（极少数情况），明确提示而不是静默吞掉。
          _showEmptyFileToast();
          continue;
        }
        // 走与粘贴/拖拽一致的统一入口：按文件名/MIME 自动分流成图片 / 视频 /
        // 文件，并复用各自的预处理（图片压缩、视频大小校验）与可见提示，
        // 避免"选完文件却毫无反馈"。
        await stageFileFromBytes(
          bytes: bytes,
          fileName: file.name,
          contentType: '',
        );
      }
    } catch (e) {
      debugPrint('pickAndSendFile error: $e');
      CustomToast.show('oss_upload_error'.tr, isError: true);
    }
  }

  Future<void> _pickImagesInternal({required bool fromCamera}) async {
    if (!_canStartPick()) {
      return;
    }

    try {
      final images = await HardwareFacade.pickImages(fromCamera: fromCamera);
      if (images.isEmpty) {
        return;
      }

      for (final image in images) {
        final upload = await _prepareImageUpload(image);
        if (upload == null) {
          return;
        }
        owner.stagedAttachments.add(upload);
      }
    } catch (e) {
      debugPrint('pickAndSendImage error: $e');
      CustomToast.show('oss_upload_error'.tr, isError: true);
    }
  }

  Future<void> _pickVideosInternal({required bool fromCamera}) async {
    if (!_canStartPick()) {
      return;
    }

    try {
      // 从相册选视频与拍摄一样走系统相册（image_picker gallery），
      // 不再用 FilePicker——后者在 iOS 上打开的是"文件 App"，进不了真正的相册。
      final video = await HardwareFacade.pickVideo(fromCamera: fromCamera);
      if (video == null) {
        return;
      }
      final upload = await _prepareVideoUpload(video);
      if (upload == null) {
        return;
      }
      owner.stagedAttachments.add(upload);
    } catch (e) {
      debugPrint('pickAndSendVideo error: $e');
      CustomToast.show('oss_upload_error'.tr, isError: true);
    }
  }

  bool _canStartPick() {
    if (owner.sessionId.isEmpty) {
      CustomToast.show('chat_attachment_no_session'.tr, isError: true);
      return false;
    }
    if (owner.isUploadingAttachment) {
      CustomToast.show('chat_attachment_uploading'.tr, isError: false);
      return false;
    }
    if (!owner._chatInputController.ensureCurrentUserCanSpeak()) {
      CustomToast.show('chat_attachment_cannot_speak'.tr, isError: true);
      return false;
    }
    closeAttachmentMenu();
    return true;
  }

  void removeStagedAttachment(int index) {
    if (index >= 0 && index < owner.stagedAttachments.length) {
      owner.stagedAttachments.removeAt(index);
    }
  }

  void clearStagedAttachments() {
    owner.stagedAttachments.clear();
  }

  Future<void> editStagedImage(int index) async {
    if (index < 0 || index >= owner.stagedAttachments.length) {
      return;
    }
    final staged = owner.stagedAttachments[index];
    if (!staged.isImage || staged.bytes == null) {
      return;
    }

    try {
      final edited = await ChatImageEditorPage.open(
        imageBytes: staged.bytes!,
        fileName: staged.fileName,
        contentType: staged.contentType,
      );
      if (edited == null) {
        return;
      }

      final prepared = await owner._imageCompressionService.prepareForUpload(
        bytes: edited.bytes,
        fileName: edited.fileName,
        contentType: edited.contentType,
      );
      if (prepared == null) {
        CustomToast.show('chat_attachment_image_too_large'.tr, isError: true);
        return;
      }

      owner.stagedAttachments[index] = PendingAttachmentUpload(
        type: ChatAttachmentType.image,
        fileName: prepared.fileName,
        contentType: prepared.contentType,
        bytes: prepared.bytes,
      );
    } catch (e) {
      debugPrint('editStagedImage error: $e');
      CustomToast.show('oss_upload_error'.tr, isError: true);
    }
  }

  Future<List<ChatMessageAttachment>> _uploadRawAttachments(
    List<PendingAttachmentUpload> uploads,
  ) async {
    if (uploads.isEmpty) {
      return const [];
    }

    try {
      final results = await Future.wait<_UploadedAttachmentResult?>(
        uploads.map(_uploadSingleAttachment),
      );

      if (results.any((item) => item == null)) {
        await _rollbackUploadedObjects(results);
        return const [];
      }

      return results
          .whereType<_UploadedAttachmentResult>()
          .map((item) => item.attachment)
          .toList(growable: false);
    } catch (e) {
      debugPrint('_uploadRawAttachments error: $e');
      return const [];
    }
  }

  Future<List<ChatMessageAttachment>> uploadAttachmentsFromList(
    List<PendingAttachmentUpload> uploads,
  ) async {
    if (uploads.isEmpty) {
      return const [];
    }

    owner._isUploadingAttachment.value = true;
    try {
      final results = await _uploadRawAttachments(uploads);
      if (results.isEmpty && uploads.isNotEmpty) {
        CustomToast.show('oss_upload_failed'.tr, isError: true);
      }
      return results;
    } finally {
      owner._isUploadingAttachment.value = false;
    }
  }

  Future<List<ChatMessageAttachment>> uploadStagedAttachments() {
    return uploadAttachmentsFromList(
      List<PendingAttachmentUpload>.from(owner.stagedAttachments),
    );
  }

  Future<void> uploadAttachmentsForTest(
    List<ChatPreparedAttachmentUpload> uploads,
  ) {
    return uploadAttachments(
      uploads.map(_pendingUploadFromPrepared).toList(growable: false),
    );
  }

  Future<void> uploadAttachments(List<PendingAttachmentUpload> uploads) async {
    owner._isUploadingAttachment.value = true;
    try {
      final results = await Future.wait<_UploadedAttachmentResult?>(
        uploads.map(_uploadSingleAttachment),
      );
      if (results.any((item) => item == null)) {
        await _rollbackUploadedObjects(results);
        CustomToast.show('oss_upload_failed'.tr, isError: true);
        return;
      }
      final uploaded = results
          .whereType<_UploadedAttachmentResult>()
          .map((item) => item.attachment)
          .toList(growable: false);
      owner.stagedAttachments.addAll(
        uploaded.map(
          (a) => PendingAttachmentUpload(
            type: a.isImage
                ? ChatAttachmentType.image
                : a.isVideo
                ? ChatAttachmentType.video
                : ChatAttachmentType.file,
            fileName: a.fileName,
            contentType: a.contentType,
          ),
        ),
      );
    } catch (e) {
      debugPrint('uploadAttachments error: $e');
      CustomToast.show('oss_upload_error'.tr, isError: true);
    } finally {
      owner._isUploadingAttachment.value = false;
    }
  }

  Future<void> stageFileFromBytes({
    required Uint8List bytes,
    required String fileName,
    required String contentType,
  }) async {
    if (!_canStartPick()) {
      return;
    }

    final type = _resolveAttachmentType(fileName, contentType);

    switch (type) {
      case ChatAttachmentType.image:
        final prepared = await _prepareImageUploadFromBytes(
          bytes: bytes,
          fileName: fileName,
          contentType: contentType,
        );
        if (prepared != null) {
          owner.stagedAttachments.add(prepared);
        }
      case ChatAttachmentType.video:
        if (!ChatAttachmentLimitPolicy.isVideoWithinLimit(bytes.length)) {
          CustomToast.show('chat_attachment_video_too_large'.tr, isError: true);
          return;
        }
        final resolvedName = ChatAttachmentPayloadBuilder.resolveFileName(
          fileName,
          type: ChatAttachmentType.video,
        );
        final resolvedContentType =
            ChatAttachmentPayloadBuilder.resolveContentType(
              resolvedName,
              type: ChatAttachmentType.video,
            );
        owner.stagedAttachments.add(
          PendingAttachmentUpload(
            type: ChatAttachmentType.video,
            fileName: resolvedName,
            contentType: resolvedContentType,
            bytes: bytes,
            contentLength: bytes.length,
          ),
        );
      case ChatAttachmentType.file:
        final resolvedName = ChatAttachmentPayloadBuilder.resolveFileName(
          fileName,
          type: ChatAttachmentType.file,
        );
        if (!ChatAttachmentPayloadBuilder.isSupportedFile(resolvedName)) {
          _showUnsupportedFileToast();
          return;
        }
        if (bytes.isEmpty) {
          _showEmptyFileToast();
          return;
        }
        final resolvedContentType =
            ChatAttachmentPayloadBuilder.resolveContentType(
              resolvedName,
              type: ChatAttachmentType.file,
            );
        owner.stagedAttachments.add(
          PendingAttachmentUpload(
            type: ChatAttachmentType.file,
            fileName: resolvedName,
            contentType: resolvedContentType,
            bytes: bytes,
            contentLength: bytes.length,
          ),
        );
    }
  }

  static const _imageExtensions = <String>[
    'jpg',
    'jpeg',
    'png',
    'webp',
    'gif',
    'bmp',
    'heic',
    'heif',
    'svg',
  ];

  ChatAttachmentType _resolveAttachmentType(String name, String mime) {
    final lowerMime = mime.toLowerCase();
    if (lowerMime.startsWith('image/')) {
      return ChatAttachmentType.image;
    }
    if (lowerMime.startsWith('video/')) {
      return ChatAttachmentType.video;
    }

    final ext = _extensionOf(name);
    if (_imageExtensions.contains(ext)) {
      return ChatAttachmentType.image;
    }
    if (ChatAttachmentPayloadBuilder.uploadableVideoExtensions.contains(ext)) {
      return ChatAttachmentType.video;
    }
    return ChatAttachmentType.file;
  }

  static String _extensionOf(String fileName) {
    final dot = fileName.lastIndexOf('.');
    if (dot < 0 || dot == fileName.length - 1) return '';
    return fileName.substring(dot + 1).toLowerCase();
  }

  Future<PendingAttachmentUpload?> _prepareImageUploadFromBytes({
    required Uint8List bytes,
    required String fileName,
    required String contentType,
  }) async {
    final resolvedName = ChatAttachmentPayloadBuilder.resolveFileName(
      fileName,
      type: ChatAttachmentType.image,
    );
    final resolvedContentType = ChatAttachmentPayloadBuilder.resolveContentType(
      resolvedName,
      type: ChatAttachmentType.image,
    );

    final preparedImage = await owner._imageCompressionService.prepareForUpload(
      bytes: bytes,
      fileName: resolvedName,
      contentType: resolvedContentType,
    );
    if (preparedImage == null) {
      CustomToast.show('chat_attachment_image_too_large'.tr, isError: true);
      return null;
    }

    return PendingAttachmentUpload(
      type: ChatAttachmentType.image,
      fileName: preparedImage.fileName,
      contentType: preparedImage.contentType,
      bytes: preparedImage.bytes,
    );
  }

  Future<PendingAttachmentUpload?> _prepareImageUpload(dynamic image) async {
    final bytes = await image.readAsBytes();
    final fileName = ChatAttachmentPayloadBuilder.resolveFileName(
      image.name,
      type: ChatAttachmentType.image,
    );
    final contentType = ChatAttachmentPayloadBuilder.resolveContentType(
      fileName,
      type: ChatAttachmentType.image,
    );

    final preparedImage = await owner._imageCompressionService.prepareForUpload(
      bytes: bytes,
      fileName: fileName,
      contentType: contentType,
    );
    if (preparedImage == null) {
      CustomToast.show('chat_attachment_image_too_large'.tr, isError: true);
      return null;
    }

    return PendingAttachmentUpload(
      type: ChatAttachmentType.image,
      fileName: preparedImage.fileName,
      contentType: preparedImage.contentType,
      bytes: preparedImage.bytes,
    );
  }

  Future<PendingAttachmentUpload?> _prepareVideoUpload(dynamic video) async {
    final videoLength = await video.length();
    if (!ChatAttachmentLimitPolicy.isVideoWithinLimit(videoLength)) {
      CustomToast.show('chat_attachment_video_too_large'.tr, isError: true);
      return null;
    }

    final fileName = ChatAttachmentPayloadBuilder.resolveFileName(
      video.name,
      type: ChatAttachmentType.video,
    );
    final contentType = ChatAttachmentPayloadBuilder.resolveContentType(
      fileName,
      type: ChatAttachmentType.video,
    );

    final bytes = await video.readAsBytes();
    return PendingAttachmentUpload(
      type: ChatAttachmentType.video,
      fileName: fileName,
      contentType: contentType,
      bytes: bytes,
      contentLength: videoLength,
    );
  }

  /// 不支持的文件类型统一提示：带上具体支持的格式，避免"为什么传不了"的困惑。
  void _showUnsupportedFileToast() {
    final supported = ChatAttachmentPayloadBuilder.uploadableFileExtensions
        .join(', ');
    CustomToast.show(
      '${'chat_attachment_file_unsupported'.tr}（$supported）',
      isError: true,
    );
  }

  /// 空文件（0 字节）统一提示：明确告诉用户文件没有内容，
  /// 而不是笼统的"上传异常"，避免误以为是上传链路出了问题。
  void _showEmptyFileToast() {
    CustomToast.show('chat_attachment_file_empty'.tr, isError: true);
  }

  Future<_UploadedAttachmentResult?> _uploadSingleAttachment(
    PendingAttachmentUpload upload,
  ) async {
    final presignRes = await owner.ossService.getPresignedUrl(
      upload.fileName,
      upload.contentType,
    );
    if (presignRes == null) {
      return null;
    }

    final uploadUrl = presignRes['uploadUrl']?.trim() ?? '';
    final accessUrl = presignRes['accessUrl']?.trim() ?? '';
    final objectKey = presignRes['objectKey']?.trim() ?? '';
    if (uploadUrl.isEmpty || accessUrl.isEmpty || objectKey.isEmpty) {
      return null;
    }

    bool success;
    if (upload.stream != null && upload.contentLength != null) {
      success = await owner.ossService.uploadStreamToOss(
        uploadUrl,
        upload.stream!,
        contentLength: upload.contentLength!,
        contentType: upload.contentType,
      );
    } else {
      final bytes = upload.bytes;
      if (bytes == null || bytes.isEmpty) {
        return null;
      }
      success = await owner.ossService.uploadToOss(
        uploadUrl,
        bytes,
        contentType: upload.contentType,
      );
    }
    if (!success) {
      return null;
    }

    return _UploadedAttachmentResult(
      objectKey: objectKey,
      attachment: ChatMessageAttachment(
        url: accessUrl,
        type: upload.type.name,
        fileName: upload.fileName,
        contentType: upload.contentType,
      ),
    );
  }

  Future<void> _rollbackUploadedObjects(
    List<_UploadedAttachmentResult?> uploads,
  ) async {
    final objectKeys = uploads
        .whereType<_UploadedAttachmentResult>()
        .map((item) => item.objectKey)
        .where((item) => item.trim().isNotEmpty)
        .toList(growable: false);
    if (objectKeys.isEmpty) {
      return;
    }
    final deleted = await owner.ossService.deleteObjects(objectKeys);
    if (!deleted) {
      debugPrint('rollback uploaded objects failed: $objectKeys');
    }
  }

  PendingAttachmentUpload _pendingUploadFromPrepared(
    ChatPreparedAttachmentUpload upload,
  ) {
    return PendingAttachmentUpload(
      type: upload.type,
      fileName: upload.fileName,
      contentType: upload.contentType,
      bytes: upload.bytes,
      contentLength: upload.contentLength,
    );
  }
}
