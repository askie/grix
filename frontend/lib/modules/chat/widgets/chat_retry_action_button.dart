import 'package:flutter/material.dart';

class ChatRetryActionButton extends StatefulWidget {
  const ChatRetryActionButton({
    super.key,
    required this.label,
    required this.onTap,
    required this.color,
    required this.fontSize,
  });

  final String label;
  final VoidCallback onTap;
  final Color color;
  final double fontSize;

  @override
  State<ChatRetryActionButton> createState() => _ChatRetryActionButtonState();
}

class _ChatRetryActionButtonState extends State<ChatRetryActionButton> {
  bool _hovered = false;
  bool _pressed = false;

  void _setPressed(bool value) {
    if (_pressed == value) {
      return;
    }
    setState(() {
      _pressed = value;
    });
  }

  @override
  Widget build(BuildContext context) {
    final backgroundAlpha = _pressed
        ? 0.12
        : _hovered
        ? 0.06
        : 0.0;

    return MouseRegion(
      cursor: SystemMouseCursors.click,
      onEnter: (_) => setState(() {
        _hovered = true;
      }),
      onExit: (_) => setState(() {
        _hovered = false;
        _pressed = false;
      }),
      child: GestureDetector(
        behavior: HitTestBehavior.opaque,
        onTapDown: (_) => _setPressed(true),
        onTapUp: (_) => _setPressed(false),
        onTapCancel: () => _setPressed(false),
        onTap: widget.onTap,
        child: AnimatedScale(
          scale: _pressed ? 0.94 : 1,
          duration: Duration(milliseconds: _pressed ? 90 : 160),
          curve: _pressed ? Curves.easeOutCubic : Curves.easeOutBack,
          child: AnimatedContainer(
            duration: const Duration(milliseconds: 140),
            curve: Curves.easeOutCubic,
            padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
            decoration: BoxDecoration(
              color: widget.color.withValues(alpha: backgroundAlpha),
              borderRadius: BorderRadius.circular(999),
            ),
            child: Text(
              widget.label,
              style: TextStyle(
                fontSize: widget.fontSize,
                color: widget.color,
                fontWeight: FontWeight.w600,
              ),
            ),
          ),
        ),
      ),
    );
  }
}
