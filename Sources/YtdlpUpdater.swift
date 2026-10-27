import Foundation

/// Keeps yt-dlp current without rebuilding the app.
///
/// Sites change constantly, so the yt-dlp pinned at build time stops working
/// within weeks. The signed bundle must not be modified, so the bundled copy
/// is seeded once into `ToolLocator.managedDirectory` and refreshed there by
/// yt-dlp's own updater, which checks the release's SHA2-256SUMS and honours
/// upstream's update lock for older systems.
///
/// Only that managed copy is ever touched: a manual override or a Homebrew
/// install is the user's to update.
@MainActor
final class YtdlpUpdater: ObservableObject {

    @Published private(set) var isUpdating = false
    /// One-line outcome of the last update attempt, empty until there is one.
    @Published private(set) var message = ""
    @Published private(set) var failed = false

    /// The yt-dlp downloads run with, as resolved by `ToolLocator`.
    var activePath: String?

    private static let lastCheckKey = "ytdlpLastUpdateCheck"
    private static let checkInterval: TimeInterval = 24 * 60 * 60

    nonisolated static var managedPath: String {
        ToolLocator.managedDirectory.appendingPathComponent("yt-dlp").path
    }

    /// Copies the bundled yt-dlp into the managed directory if no usable copy
    /// is there yet. Cheap when the copy exists; safe to call off the main actor.
    nonisolated static func seedManagedCopy() {
        let fm = FileManager.default
        guard !ToolLocator.isUsable(managedPath),
              let bundled = ToolLocator.bundledDirectory?.appendingPathComponent("yt-dlp"),
              ToolLocator.isUsable(bundled.path) else { return }

        let dir = ToolLocator.managedDirectory
        // Copy beside the destination, then rename, so a half-written file is
        // never picked up as the tool.
        let staging = dir.appendingPathComponent(".yt-dlp.seed-\(UUID().uuidString)")
        do {
            try fm.createDirectory(at: dir, withIntermediateDirectories: true)
            try fm.copyItem(at: bundled, to: staging)
            try fm.setAttributes([.posixPermissions: 0o755], ofItemAtPath: staging.path)
            guard rename(staging.path, managedPath) == 0 else {
                throw CocoaError(.fileWriteUnknown)
            }
        } catch {
            try? fm.removeItem(at: staging)
        }
    }

    /// Checks for an update at most once a day, and only if the active yt-dlp
    /// is the managed copy.
    func updateIfDue() {
        let last = UserDefaults.standard.object(forKey: Self.lastCheckKey) as? Date ?? .distantPast
        guard activePath == Self.managedPath,
              Date().timeIntervalSince(last) >= Self.checkInterval else { return }
        update()
    }

    /// Runs `yt-dlp --update-to stable` on the managed copy.
    func update() {
        guard !isUpdating else { return }
        guard activePath == Self.managedPath else {
            failed = true
            message = "This yt-dlp is managed outside YTGrab — update it there (e.g. `brew upgrade yt-dlp`)."
            return
        }

        isUpdating = true
        failed = false
        message = "Checking for a yt-dlp update…"
        Task {
            let result = await Task.detached(priority: .utility) {
                Self.runUpdater(at: Self.managedPath)
            }.value
            isUpdating = false
            failed = !result.ok
            message = result.message
            if result.ok {
                UserDefaults.standard.set(Date(), forKey: Self.lastCheckKey)
            }
        }
    }

    /// Bounded, so an unreachable GitHub cannot leave the updater spinning.
    nonisolated private static func runUpdater(at path: String) -> (ok: Bool, message: String) {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: path)
        process.arguments = ["--no-config", "--update-to", "stable"]
        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = pipe
        process.standardInput = FileHandle.nullDevice
        do {
            try process.run()
        } catch {
            return (false, "Could not run yt-dlp: \(error.localizedDescription)")
        }

        // Drain while waiting: a full pipe would otherwise stall the child.
        var output = Data()
        let reader = DispatchQueue(label: "ytgrab.updater.read")
        let drained = DispatchSemaphore(value: 0)
        reader.async {
            output = pipe.fileHandleForReading.readDataToEndOfFile()
            drained.signal()
        }

        let deadline = Date().addingTimeInterval(180)
        while process.isRunning && Date() < deadline {
            usleep(100_000)
        }
        if process.isRunning {
            process.terminate()
            process.waitUntilExit()
            drained.wait()
            return (false, "yt-dlp update timed out.")
        }
        drained.wait()

        let text = String(decoding: output, as: UTF8.self)
        return summarize(output: text, exitCode: process.terminationStatus)
    }

    /// Picks the line that says what happened out of the updater's chatter.
    nonisolated static func summarize(output: String, exitCode: Int32) -> (ok: Bool, message: String) {
        let lines = output.split(whereSeparator: \.isNewline)
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
        if exitCode == 0,
           let line = lines.last(where: { $0.hasPrefix("Updated yt-dlp") || $0.hasPrefix("yt-dlp is up to date") }) {
            return (true, line)
        }
        if let error = lines.last(where: { $0.hasPrefix("ERROR:") }) {
            return (false, "yt-dlp update failed: " + error.dropFirst("ERROR:".count)
                .trimmingCharacters(in: .whitespaces))
        }
        return (false, "yt-dlp update failed (exit code \(exitCode)).")
    }
}
