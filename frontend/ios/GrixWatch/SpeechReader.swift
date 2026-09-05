import AVFoundation
import Foundation

/// 把 agent 的回复读出来。
///
/// 只在用户点「朗读」时才发声：手表贴着手腕，自动播报会替用户在会议室或地铁里
/// 做决定。语言跟系统走，不做单独设置。
@MainActor
final class SpeechReader: NSObject, ObservableObject {
  @Published private(set) var isSpeaking = false

  private let synthesizer = AVSpeechSynthesizer()

  override init() {
    super.init()
    synthesizer.delegate = self
  }

  func toggle(_ text: String) {
    if isSpeaking {
      stop()
      return
    }
    speak(text)
  }

  func speak(_ text: String) {
    let content = text.trimmingCharacters(in: .whitespacesAndNewlines)
    guard !content.isEmpty else { return }

    // watchOS 上必须先把音频会话切到播放并激活，否则合成器会静默地什么都不发生。
    let session = AVAudioSession.sharedInstance()
    try? session.setCategory(.playback, mode: .spokenAudio)
    try? session.setActive(true)

    let utterance = AVSpeechUtterance(string: content)
    utterance.voice = Self.systemVoice()
    synthesizer.speak(utterance)
    isSpeaking = true
  }

  func stop() {
    synthesizer.stopSpeaking(at: .immediate)
    finish()
  }

  fileprivate func finish() {
    isSpeaking = false
    // 放完就把音频会话还回去，否则手表会一直显示在播放。
    try? AVAudioSession.sharedInstance().setActive(
      false,
      options: .notifyOthersOnDeactivation
    )
  }

  /// 跟随系统语言。取不到对应嗓音就交给系统默认值，而不是硬写一种语言。
  private static func systemVoice() -> AVSpeechSynthesisVoice? {
    if let voice = AVSpeechSynthesisVoice(language: AVSpeechSynthesisVoice.currentLanguageCode()) {
      return voice
    }
    for language in Locale.preferredLanguages {
      if let voice = AVSpeechSynthesisVoice(language: language) {
        return voice
      }
    }
    return nil
  }
}

extension SpeechReader: AVSpeechSynthesizerDelegate {
  nonisolated func speechSynthesizer(
    _ synthesizer: AVSpeechSynthesizer,
    didFinish utterance: AVSpeechUtterance
  ) {
    Task { @MainActor in self.finish() }
  }

  nonisolated func speechSynthesizer(
    _ synthesizer: AVSpeechSynthesizer,
    didCancel utterance: AVSpeechUtterance
  ) {
    Task { @MainActor in self.finish() }
  }
}
