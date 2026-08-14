import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:get/get.dart';

import '../../../data/providers/agent_service.dart';

class AgentProfileDraft {
  AgentProfileDraft()
    : nameController = TextEditingController(),
      introductionController = TextEditingController();

  final TextEditingController nameController;
  final TextEditingController introductionController;
  final RxString avatarUrl = ''.obs;
  final Rxn<Uint8List> pendingAvatarBytes = Rxn<Uint8List>();
  final RxString pendingAvatarFilename = ''.obs;

  bool get hasPendingAvatarUpload =>
      pendingAvatarBytes.value != null &&
      pendingAvatarFilename.value.trim().isNotEmpty;

  String get name => nameController.text.trim();
  String get introduction => introductionController.text
      .replaceAll('\r\n', '\n')
      .replaceAll('\r', '\n')
      .trim();

  void applyAgent(AgentModel agent) {
    nameController.value = TextEditingValue(
      text: agent.agentName,
      selection: TextSelection.collapsed(offset: agent.agentName.length),
    );
    introductionController.value = TextEditingValue(
      text: agent.introduction,
      selection: TextSelection.collapsed(offset: agent.introduction.length),
    );
    avatarUrl.value = agent.avatarUrl.trim();
    clearPendingAvatar();
  }

  void setPendingAvatar({required Uint8List bytes, required String filename}) {
    pendingAvatarBytes.value = bytes;
    pendingAvatarFilename.value = filename.trim();
  }

  void commitAvatarUrl(String nextAvatarUrl) {
    avatarUrl.value = nextAvatarUrl.trim();
    clearPendingAvatar();
  }

  void clearPendingAvatar() {
    pendingAvatarBytes.value = null;
    pendingAvatarFilename.value = '';
  }

  void dispose() {
    nameController.dispose();
    introductionController.dispose();
  }
}
