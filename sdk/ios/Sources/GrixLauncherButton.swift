#if canImport(UIKit)
import UIKit

/// Floating launcher button that overlays the host application's key window.
final class GrixLauncherButton: UIButton {
    private let badgeLabel = UILabel()

    override init(frame: CGRect) {
        super.init(frame: frame)
        setupUI()
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) {
        fatalError("init(coder:) has not been implemented")
    }

    private func setupUI() {
        layer.cornerRadius = 28
        layer.shadowColor = UIColor.black.cgColor
        layer.shadowOpacity = 0.22
        layer.shadowOffset = CGSize(width: 0, height: 4)
        layer.shadowRadius = 8

        let config = UIImage.SymbolConfiguration(pointSize: 22, weight: .semibold)
        let image = UIImage(systemName: "bubble.left.and.bubble.right.fill", withConfiguration: config)
        setImage(image, for: .normal)
        tintColor = .white

        badgeLabel.backgroundColor = .systemRed
        badgeLabel.textColor = .white
        badgeLabel.font = .systemFont(ofSize: 11, weight: .bold)
        badgeLabel.textAlignment = .center
        badgeLabel.layer.cornerRadius = 10
        badgeLabel.layer.masksToBounds = true
        badgeLabel.isHidden = true
        badgeLabel.translatesAutoresizingMaskIntoConstraints = false
        addSubview(badgeLabel)

        NSLayoutConstraint.activate([
            badgeLabel.topAnchor.constraint(equalTo: topAnchor, constant: -4),
            badgeLabel.trailingAnchor.constraint(equalTo: trailingAnchor, constant: 4),
            badgeLabel.heightAnchor.constraint(equalToConstant: 20),
            badgeLabel.widthAnchor.constraint(greaterThanOrEqualToConstant: 20),
        ])
    }

    func updateThemeColor(_ hexColor: String?) {
        backgroundColor = UIColor.grixColor(fromHex: hexColor) ?? UIColor(red: 15.0 / 255.0, green: 118.0 / 255.0, blue: 110.0 / 255.0, alpha: 1.0)
    }

    func updateUnreadCount(_ count: Int) {
        if count <= 0 {
            badgeLabel.isHidden = true
            badgeLabel.text = nil
        } else {
            badgeLabel.isHidden = false
            badgeLabel.text = count > 99 ? "99+" : " \(count) "
        }
    }
}

extension UIColor {
    static func grixColor(fromHex hex: String?) -> UIColor? {
        guard var cleaned = hex?.trimmingCharacters(in: .whitespacesAndNewlines), !cleaned.isEmpty else {
            return nil
        }
        if cleaned.hasPrefix("#") {
            cleaned.removeFirst()
        }
        guard cleaned.count == 6, let rgb = UInt64(cleaned, radix: 16) else {
            return nil
        }
        let r = CGFloat((rgb & 0xFF0000) >> 16) / 255.0
        let g = CGFloat((rgb & 0x00FF00) >> 8) / 255.0
        let b = CGFloat(rgb & 0x0000FF) / 255.0
        return UIColor(red: r, green: g, blue: b, alpha: 1.0)
    }
}
#endif
