import Foundation
import AppKit

/// The run log, kept out of `Downloader` on purpose.
///
/// yt-dlp emits hundreds of lines per download. If these were `@Published` on
/// `Downloader` — which the main form observes — every line would invalidate
/// the entire form. Only the log window observes this object.
@MainActor
final class LogStore: ObservableObject {
    @Published private(set) var lines: [String] = []

    func append(_ line: String) {
        lines.append(line)
        if lines.count > 2000 { lines.removeFirst(lines.count - 2000) }
    }

    func clear() { lines.removeAll() }
}

@MainActor
final class Downloader: ObservableObject {

    /// Marker the progress template prints so progress lines can be told apart
    /// from ordinary yt-dlp chatter.
    nonisolated static let progressPrefix = "@@PROG@@"

    @Published private(set) var isRunning = false
    @Published private(set) var progress: Double = 0        // 0...1, -1 = indeterminate
    @Published private(set) var status = "Idle"
    @Published private(set) var title = ""
    @Published private(set) var speed = ""
    @Published private(set) var eta = ""
    @Published private(set) var producedFile: URL?
    @Published private(set) var failed = false
    /// Last error yt-dlp reported, surfaced under the status line so a failure
    /// is explicable without opening the log.
    @Published private(set) var errorDetail = ""
    @Published private(set) var cancelling = false

    /// Not `@Published`, so appending a line does not invalidate the main form.
    let log = LogStore()

    private var process: Process?
    private var outPipe: Pipe?
    private var errPipe: Pipe?
    private var cookieFile: URL?
    private var cancelledByUser = false
    private var killWorkItem: DispatchWorkItem?

    // MARK: - Lifecycle

    func start(_ request: DownloadRequest, ytdlp: String, ffmpeg: String?) {
        guard !isRunning else { return }

        log.clear()
        producedFile = nil
        failed = false
        cancelledByUser = false
        cancelling = false
        errorDetail = ""
        title = ""
        speed = ""
        eta = ""
        progress = -1
        status = "Starting…"

        // Pasted cookies live in a private temp file for the length of the run.
        do {
            if request.authMode == .pastedCookies {
                cookieFile = try Cookies.writeTemporaryJar(request.cookieText)
            }
        } catch {
            failLaunch("Could not stage cookies: \(error.localizedDescription)")
            return
        }

        let ffmpegDir = ffmpeg.map { URL(fileURLWithPath: $0).deletingLastPathComponent().path }
        let args = ArgumentBuilder.arguments(for: request,
                                             cookieFile: cookieFile,
                                             ffmpegDirectory: ffmpegDir)

        log.append("$ " + ArgumentBuilder.preview(tool: ytdlp, args: redact(args)))

        let process = Process()
        process.executableURL = URL(fileURLWithPath: ytdlp)
        process.arguments = args
        process.currentDirectoryURL = request.outputDir
        var env = ProcessInfo.processInfo.environment
        env["PATH"] = ToolLocator.childPATH
        process.environment = env

        let out = Pipe(), err = Pipe()
        process.standardOutput = out
        process.standardError = err
        process.standardInput = FileHandle.nullDevice   // never let yt-dlp block on a prompt
        outPipe = out
        errPipe = err

        attach(out)
        attach(err)

        process.terminationHandler = { [weak self] proc in
            let code = proc.terminationStatus
            let signalled = proc.terminationReason == .uncaughtSignal
            // Pipe callbacks and termination arrive on different queues. Give
            // the final stderr/stdout chunks a chance to reach the main actor
            // before marking the run finished and detaching the handlers.
            Task { @MainActor [weak self] in
                try? await Task.sleep(nanoseconds: 100_000_000)
                self?.processDidExit(code: code, signalled: signalled)
            }
        }

        do {
            try process.run()
            self.process = process
            isRunning = true
        } catch {
            // The child never started, so `processDidExit` will never run and
            // would otherwise leave the jar (real session cookies) on disk.
            detachHandlers()
            failLaunch("Could not launch yt-dlp: \(error.localizedDescription)")
        }
    }

    func cancel() {
        guard let process, isRunning, !cancelledByUser else { return }
        cancelledByUser = true
        cancelling = true
        status = "Cancelling…"
        process.terminate()
        // yt-dlp normally winds down on SIGTERM; insist if it does not.
        let pid = process.processIdentifier
        let work = DispatchWorkItem { [weak process] in
            guard let process, process.isRunning else { return }
            kill(pid, SIGKILL)
        }
        killWorkItem = work
        DispatchQueue.global().asyncAfter(deadline: .now() + 3, execute: work)
    }

    /// Called on quit so a cookie jar is never left behind.
    func cleanUp() {
        killWorkItem?.cancel()
        killWorkItem = nil
        if let process, process.isRunning {
            process.terminate()
            // Give the child a moment to let go of the cookie file before we
            // pull it out from under it. Bounded so quitting never hangs.
            let deadline = Date().addingTimeInterval(1.5)
            while process.isRunning && Date() < deadline {
                usleep(50_000)
            }
            if process.isRunning { kill(process.processIdentifier, SIGKILL) }
        }
        detachHandlers()
        Cookies.shred(cookieFile)
        cookieFile = nil
    }

    // MARK: - Output plumbing

    /// Reassembles whole lines out of pipe chunks.
    ///
    /// A reference type because the readability handler runs off the main
    /// thread. `@unchecked Sendable` is sound only because each pipe gets its
    /// **own** buffer and Foundation invokes a given handle's readability
    /// handler on a single private serial queue — so `data` is never touched
    /// concurrently. Do not share one buffer across both pipes.
    private final class LineBuffer: @unchecked Sendable {
        private var data = Data()
        func take(_ chunk: Data) -> [String] {
            data.append(chunk)
            var lines: [String] = []
            while let newline = data.firstIndex(of: UInt8(ascii: "\n")) {
                let lineData = data[data.startIndex..<newline]
                data.removeSubrange(data.startIndex...newline)
                let line = String(decoding: lineData, as: UTF8.self)
                    .trimmingCharacters(in: .whitespacesAndNewlines)
                if !line.isEmpty { lines.append(line) }
            }
            return lines
        }
    }

    private func attach(_ pipe: Pipe) {
        let buffer = LineBuffer()
        pipe.fileHandleForReading.readabilityHandler = { [weak self] handle in
            let chunk = handle.availableData
            guard !chunk.isEmpty else {
                handle.readabilityHandler = nil
                return
            }
            let lines = buffer.take(chunk)
            guard !lines.isEmpty else { return }
            Task { @MainActor in
                for line in lines { self?.handle(line: line) }
            }
        }
    }

    /// yt-dlp spawns ffmpeg as a grandchild which inherits the pipe write ends,
    /// so EOF is not guaranteed to arrive — clear the handlers explicitly
    /// rather than relying on the zero-length read.
    private func detachHandlers() {
        outPipe?.fileHandleForReading.readabilityHandler = nil
        errPipe?.fileHandleForReading.readabilityHandler = nil
        outPipe = nil
        errPipe = nil
    }

    private func handle(line: String) {
        if line.hasPrefix(Self.progressPrefix) {
            if isRunning {
                applyProgress(String(line.dropFirst(Self.progressPrefix.count)))
            }
            return
        }

        log.append(line)

        if line.hasPrefix("ERROR:") || line.contains("Errno") {
            errorDetail = line
        }

        // Track the file yt-dlp ends up writing, so "Reveal in Finder" is accurate.
        for marker in ["[download] Destination: ", "[ExtractAudio] Destination: ",
                       "[Merger] Merging formats into \""] {
            if line.hasPrefix(marker) {
                var path = String(line.dropFirst(marker.count))
                if path.hasSuffix("\"") { path.removeLast() }
                if path.hasPrefix("/") { producedFile = URL(fileURLWithPath: path) }
            }
        }
        if line.contains("has already been downloaded") {
            let trimmed = line.replacingOccurrences(of: "[download] ", with: "")
                .replacingOccurrences(of: " has already been downloaded", with: "")
            if trimmed.hasPrefix("/") { producedFile = URL(fileURLWithPath: trimmed) }
        }

        // Late output remains useful in the log and for file/error discovery,
        // but must not replace the final status.
        guard isRunning else { return }

        // Phase labels worth surfacing above the progress bar.
        if line.hasPrefix("[Merger]") {
            progress = -1; status = "Merging video and audio…"
        } else if line.hasPrefix("[ExtractAudio]") {
            progress = -1; status = "Extracting audio…"
        } else if line.hasPrefix("[EmbedThumbnail]") || line.hasPrefix("[Metadata]") {
            progress = -1; status = "Writing metadata…"
        } else if line.hasPrefix("[info] Downloading subtitles") {
            status = "Fetching subtitles…"
        }
    }

    private func applyProgress(_ payload: String) {
        let parts = payload.split(separator: "|", maxSplits: 3, omittingEmptySubsequences: false)
        guard parts.count >= 3 else { return }

        let percent = parts[0].trimmingCharacters(in: .whitespaces).replacingOccurrences(of: "%", with: "")
        if let value = Double(percent) {
            progress = min(max(value / 100, 0), 1)
        }
        speed = normalize(parts[1])
        eta = normalize(parts[2])
        if parts.count == 4 { title = normalize(parts[3]) }
        if !cancelling { status = "Downloading" }
    }

    private func normalize(_ value: Substring) -> String {
        let v = value.trimmingCharacters(in: .whitespaces)
        return (v == "N/A" || v == "NA" || v == "none") ? "" : v
    }

    // MARK: - Completion

    private func processDidExit(code: Int32, signalled: Bool) {
        process = nil
        killWorkItem?.cancel()
        killWorkItem = nil
        detachHandlers()
        Cookies.shred(cookieFile)
        cookieFile = nil

        if cancelledByUser || signalled {
            finish(message: "Cancelled", ok: false)
        } else if code == 0 {
            progress = 1
            finish(message: "Done", ok: true)
        } else {
            finish(message: "yt-dlp exited with code \(code)", ok: false)
        }
    }

    /// Failure before the child ever ran: no termination handler will fire, so
    /// tear down here instead.
    private func failLaunch(_ message: String) {
        Cookies.shred(cookieFile)
        cookieFile = nil
        finish(message: message, ok: false)
    }

    private func finish(message: String, ok: Bool) {
        isRunning = false
        cancelling = false
        failed = !ok
        status = message
        speed = ""
        eta = ""
        if !ok { progress = 0 }
        log.append("— \(message) —")
    }

    /// Keeps the cookie path out of the visible log.
    private func redact(_ args: [String]) -> [String] {
        var out = args
        if let i = out.firstIndex(of: "--cookies"), i + 1 < out.count {
            out[i + 1] = "<pasted cookies>"
        }
        return out
    }
}
