import SwiftUI
import AppKit

@main
struct YTGrabApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate
    @StateObject private var downloader = Downloader()

    var body: some Scene {
        WindowGroup("YTGrab") {
            ContentView()
                .environmentObject(downloader)
                .onAppear { delegate.downloader = downloader }
        }
        .windowResizability(.contentSize)
        .commands {
            CommandGroup(replacing: .newItem) { }
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
