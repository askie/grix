// swift-tools-version: 5.7
import PackageDescription

let package = Package(
    name: "GrixWidget",
    platforms: [
        .iOS(.v14),
        .macOS(.v11),
    ],
    products: [
        .library(
            name: "GrixWidget",
            targets: ["GrixWidget"]
        ),
    ],
    targets: [
        .target(
            name: "GrixWidget",
            path: "sdk/ios/Sources"
        ),
        .testTarget(
            name: "GrixWidgetTests",
            dependencies: ["GrixWidget"],
            path: "sdk/ios/Tests"
        ),
    ]
)
