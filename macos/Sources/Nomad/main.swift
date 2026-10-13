import NomadUI

let args = CommandLine.arguments
if let i = args.firstIndex(of: "--render-icon"), i + 1 < args.count {
    MainActor.assumeIsolated { Snapshot.renderIcon(to: args[i + 1]) }
} else if let i = args.firstIndex(of: "--snapshot") {
    let dir = i + 1 < args.count ? args[i + 1] : "snapshots"
    MainActor.assumeIsolated { Snapshot.run(outputDirectory: dir) }
} else {
    NomadApp.main()
}
