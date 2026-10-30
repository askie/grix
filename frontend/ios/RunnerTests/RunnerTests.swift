import Flutter
import UIKit
import XCTest
@testable import Grix

class RunnerTests: XCTestCase {

  func testLiveActivitySessionURLReadsSessionID() {
    let url = URL(string: "grix://session/2116093616195108864")!
    XCTAssertEqual(
      AppDelegate.liveActivitySessionID(from: url),
      "2116093616195108864"
    )
  }

  func testLiveActivitySessionURLDecodesPercentEncoding() {
    let url = URL(string: "grix://session/sess%2Fid")!
    XCTAssertEqual(AppDelegate.liveActivitySessionID(from: url), "sess/id")
  }

  func testLiveActivitySessionURLIgnoresOtherGrixLinks() {
    XCTAssertNil(AppDelegate.liveActivitySessionID(from: URL(string: "grix://share/abc")!))
    XCTAssertNil(AppDelegate.liveActivitySessionID(from: URL(string: "https://grix.im/session/1")!))
    XCTAssertNil(AppDelegate.liveActivitySessionID(from: URL(string: "grix://session/")!))
  }

}
