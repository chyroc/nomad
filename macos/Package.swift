// swift-tools-version:5.9
import PackageDescription

let package = Package(
    name: "NomadApp",
    platforms: [.macOS(.v14)],
    products: [
        .executable(name: "Nomad", targets: ["Nomad"]),
    ],
    targets: [
        .target(name: "NomadUI", path: "Sources/NomadUI"),
        .executableTarget(name: "Nomad", dependencies: ["NomadUI"], path: "Sources/Nomad"),
        .testTarget(name: "NomadUITests", dependencies: ["NomadUI"], path: "Tests/NomadUITests"),
    ]
)
