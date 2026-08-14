const double _desktopQrLoginMinWidth = 720;

bool shouldShowDesktopQrLogin({
  required bool isWeb,
  required bool isMobile,
  required bool isDesktop,
  required double width,
}) {
  if (width < _desktopQrLoginMinWidth) {
    return false;
  }
  if (isWeb) {
    return !isMobile;
  }
  return isDesktop;
}
