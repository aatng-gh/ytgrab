import SwiftUI
import AppKit

@main
struct YTGrabApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @StateObject private var downloader = Downloader()
    @StateObject private var updater = YtdlpUpdater()

    var body: some Scene {
        WindowGroup("YTGrab") {
            ContentView()
                .environmentObject(downloader)
                .environmentObject(updater)
                .onAppear { delegate.downloader = downloader }
        }
        .windowResizability(.contentSize)
        .commands {
            CommandGroup(replacing: .newItem) { }
            CommandGroup(after: .appInfo) {
                Button("Update yt-dlp") { updater.update() }
                    .disabled(updater.isUpdating || downloader.isRunning)
            }
        }

        Window("YTGrab Log", id: "log") {
            LogView(log: downloader.log)
        }
        .windowResizability(.contentMinSize)
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    var downloader: Downloader?

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.regular)
        NSApp.activate(ignoringOtherApps: true)
        // A crash, force-quit, or logout skips applicationWillTerminate, so a
        // previous run's cookie jar can still be sitting in the temp directory.
        Cookies.sweepStaleJars()
    }

    func applicationWillTerminate(_ notification: Notification) {
        MainActor.assumeIsolated { downloader?.cleanUp() }
    }
}
