const double _desktopQrLoginMinWidth = 720;

/// Shortest side, in logical pixels, at which a native device is treated as a
/// tablet that can host the desktop-style QR login flow.
const double desktopQrLoginTabletMinShortestSide = 600;

bool shouldShowDesktopQrLogin({
  required bool isWeb,
  required bool isMobile,
  required bool isDesktop,
  required bool isTablet,
  required double width,
}) {
  if (width < _desktopQrLoginMinWidth) {
    return false;
  }
  if (isWeb) {
    return !isMobile;
  }
  return isDesktop || isTablet;
}
