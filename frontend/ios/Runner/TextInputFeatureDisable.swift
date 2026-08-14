import ObjectiveC
import UIKit

final class TextInputFeatureSwizzler {
  static let shared = TextInputFeatureSwizzler()

  private var swizzled = false

  func apply() {
    guard !swizzled else { return }
    swizzled = true
    swizzleTextField()
    swizzleTextView()
  }

  private func swizzleTextField() {
    let cls: AnyClass = UITextField.self
    guard
      let original = class_getInstanceMethod(cls, #selector(UIResponder.canPerformAction(_:withSender:))),
      let replacement = class_getInstanceMethod(
        cls, #selector(UITextField.swizzled_canPerformAction(_:withSender:)))
    else { return }
    method_exchangeImplementations(original, replacement)
  }

  private func swizzleTextView() {
    let cls: AnyClass = UITextView.self
    guard
      let original = class_getInstanceMethod(cls, #selector(UIResponder.canPerformAction(_:withSender:))),
      let replacement = class_getInstanceMethod(
        cls, #selector(UITextView.swizzled_canPerformAction_textView(_:withSender:)))
    else { return }
    method_exchangeImplementations(original, replacement)
  }
}

// MARK: - UITextField swizzling

extension UITextField {
  @objc func swizzled_canPerformAction(
    _ action: Selector, withSender sender: Any?
  ) -> Bool {
    if action == Selector(("_captureTextFromCamera:")) {
      return false
    }
    return swizzled_canPerformAction(action, withSender: sender)
  }
}

// MARK: - UITextView swizzling

extension UITextView {
  @objc func swizzled_canPerformAction_textView(
    _ action: Selector, withSender sender: Any?
  ) -> Bool {
    if action == Selector(("_captureTextFromCamera:")) {
      return false
    }
    return swizzled_canPerformAction_textView(action, withSender: sender)
  }
}
