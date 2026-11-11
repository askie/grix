Pod::Spec.new do |s|
  s.name             = 'GrixWidget'
  s.version          = '1.0.0'
  s.summary          = 'Grix Website Widget iOS SDK for embedding live chat and AI voice support in iOS apps.'
  s.description      = <<-DESC
    GrixWidget is a lightweight iOS SDK wrapper around WKWebView that loads the hosted
    Grix customer support widget (/public/widget/app.html), manages visitor_key persistence
    in Keychain, handles unread badges, external URL opening, and microphone permission
    delegation for live voice calls.
  DESC
  s.homepage         = 'https://github.com/askie/grix'
  s.license          = { :type => 'Elastic-2.0', :file => 'LICENSE' }
  s.author           = { 'Grix Team' => 'support@grix.im' }
  s.source           = { :git => 'https://github.com/askie/grix.git', :tag => s.version.to_s }
  s.ios.deployment_target = '14.0'
  s.swift_version    = '5.7'
  s.source_files     = 'sdk/ios/Sources/**/*.swift'
  s.frameworks       = 'Foundation', 'WebKit', 'Security', 'AVFoundation'
end
