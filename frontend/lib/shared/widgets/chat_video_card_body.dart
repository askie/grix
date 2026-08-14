import 'package:flutter/material.dart';

/// Shared visual shell for video cards — cover image/frame + play-button
/// overlay + camera badge. Both the Markdown `<video>` tag card and the
/// attachment-grid video tile use this widget so their appearance is identical.
///
/// [cover] fills the card background: pass a poster `Image`, a `VideoPlayer`
/// first-frame, or a plain `ColoredBox` fallback — this widget does not care.
class ChatVideoCardBody extends StatelessWidget {
  const ChatVideoCardBody({super.key, required this.cover});

  final Widget cover;

  @override
  Widget build(BuildContext context) {
    return Stack(
      fit: StackFit.expand,
      children: [
        cover,
        Center(
          child: Container(
            width: 56,
            height: 56,
            decoration: BoxDecoration(
              color: Colors.black.withValues(alpha: 0.55),
              shape: BoxShape.circle,
            ),
            child: const Icon(
              Icons.play_arrow_rounded,
              color: Colors.white,
              size: 34,
            ),
          ),
        ),
        const Positioned(
          left: 8,
          bottom: 8,
          child: Icon(Icons.videocam_rounded, color: Colors.white70, size: 18),
        ),
      ],
    );
  }
}
