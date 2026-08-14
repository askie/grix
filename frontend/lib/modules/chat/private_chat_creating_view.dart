import 'package:flutter/material.dart';
import 'package:get/get.dart';

import 'private_chat_creating_status.dart';

class PrivateChatCreationDraft {
  String text = '';
}

class PrivateChatCreatingView extends StatefulWidget {
  const PrivateChatCreatingView({super.key});

  @override
  State<PrivateChatCreatingView> createState() =>
      _PrivateChatCreatingViewState();
}

class _PrivateChatCreatingViewState extends State<PrivateChatCreatingView> {
  PrivateChatCreationDraft _draft = PrivateChatCreationDraft();
  late final TextEditingController _inputController;
  final FocusNode _focusNode = FocusNode();
  String _title = '';
  var _didResolveArguments = false;

  @override
  void initState() {
    super.initState();
    // Controllers must exist before the first build; arguments are finalized in
    // didChangeDependencies (ModalRoute settings are authoritative for the
    // custom non-snapshot route, and Get.arguments can lag observer.didPush).
    _inputController = TextEditingController()..addListener(_syncDraft);
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (_didResolveArguments) {
      return;
    }
    _didResolveArguments = true;
    final arguments = _resolveArguments();
    _title = arguments['title']?.toString().trim() ?? '';
    if (arguments['creation_draft'] is PrivateChatCreationDraft) {
      _draft = arguments['creation_draft'] as PrivateChatCreationDraft;
      if (_inputController.text != _draft.text) {
        _inputController.text = _draft.text;
      }
    }
  }

  Map<String, dynamic> _resolveArguments() {
    final routeArguments = ModalRoute.of(context)?.settings.arguments;
    if (routeArguments is Map<String, dynamic>) {
      return routeArguments;
    }
    final rawArguments = Get.arguments;
    if (rawArguments is Map<String, dynamic>) {
      return rawArguments;
    }
    return const <String, dynamic>{};
  }

  void _syncDraft() {
    _draft.text = _inputController.text;
  }

  @override
  void dispose() {
    _syncDraft();
    _inputController
      ..removeListener(_syncDraft)
      ..dispose();
    _focusNode.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      backgroundColor: theme.scaffoldBackgroundColor,
      appBar: AppBar(title: Text(_title)),
      body: Column(
        children: [
          const Expanded(
            child: Center(
              child: PrivateChatCreatingStatus(),
            ),
          ),
          SafeArea(
            top: false,
            child: Container(
              key: const Key('private_chat_creating_input_area'),
              padding: const EdgeInsets.fromLTRB(8, 8, 8, 8),
              decoration: BoxDecoration(
                color: theme.colorScheme.surface,
                boxShadow: [
                  BoxShadow(
                    color: Colors.black.withValues(alpha: 0.04),
                    blurRadius: 8,
                    offset: const Offset(0, -2),
                  ),
                ],
              ),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.end,
                children: [
                  IconButton(
                    onPressed: null,
                    icon: Icon(
                      Icons.add_rounded,
                      color: theme.colorScheme.secondary.withValues(
                        alpha: 0.35,
                      ),
                      size: 28,
                    ),
                  ),
                  Expanded(
                    child: TextField(
                      key: const Key('private_chat_creating_input'),
                      controller: _inputController,
                      focusNode: _focusNode,
                      minLines: 1,
                      maxLines: 5,
                      keyboardType: TextInputType.multiline,
                      textCapitalization: TextCapitalization.sentences,
                      textInputAction: TextInputAction.newline,
                      decoration: InputDecoration(
                        hintText: 'chat_send_placeholder'.tr,
                        contentPadding: const EdgeInsets.symmetric(
                          horizontal: 16,
                          vertical: 10,
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(width: 4),
                  Container(
                    width: 40,
                    height: 40,
                    margin: const EdgeInsets.only(bottom: 2),
                    alignment: Alignment.center,
                    decoration: BoxDecoration(
                      color: theme.primaryColor.withValues(alpha: 0.4),
                      borderRadius: BorderRadius.circular(20),
                    ),
                    child: const Icon(
                      Icons.arrow_upward_rounded,
                      color: Colors.white,
                      size: 22,
                    ),
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}
