import Foundation
import Darwin

/// Handling for cookie text the user pastes in.
///
/// The text goes into a file in a per-run 0700 temp directory, is handed to
/// yt-dlp by path, and is unlinked as soon as the download finishes, is
/// cancelled, or the app quits.
///
/// Note on `shred`: on APFS an in-place overwrite does *not* scrub the original
/// blocks — the filesystem is copy-on-write, so the write lands on freshly
/// allocated extents. The real protection here is the 0700 parent directory
/// plus prompt unlinking; the overwrite is belt-and-braces for non-CoW volumes
/// and costs nothing. Do not describe this to the user as unrecoverable erasure.
enum Cookies {

    static let netscapeHeader = "# Netscape HTTP Cookie File"

    /// Prefix Chromium-family exporters put on HttpOnly cookies. yt-dlp strips
    /// it on load, so these lines are real cookies — and for YouTube they are
    /// usually *the* session cookies — not comments.
    private static let httpOnlyPrefix = "#HttpOnly_"

    /// First lines Python's MozillaCookieJar will accept as the magic header.
    private static let magicHeaders = ["# Netscape HTTP Cookie File", "# HTTP Cookie File"]

    /// Strips the HttpOnly marker so the line can be inspected as a normal record.
    private static func withoutHttpOnlyPrefix(_ line: String) -> String {
        line.hasPrefix(httpOnlyPrefix) ? String(line.dropFirst(httpOnlyPrefix.count)) : line
    }

    /// Number of usable cookie lines, ignoring comments and blanks.
    static func parsedCount(in text: String) -> Int {
        text.split(separator: "\n").filter { line in
            let raw = line.trimmingCharacters(in: .whitespaces)
            guard !raw.isEmpty else { return false }
            // Check for the HttpOnly marker *before* dismissing '#' as a comment.
            let l = withoutHttpOnlyPrefix(raw)
            guard !l.hasPrefix("#") else { return false }
            // Netscape format: domain, flag, path, secure, expiry, name, value
            return l.split(separator: "\t", omittingEmptySubsequences: false).count >= 6
        }.count
    }

    /// yt-dlp rejects a cookie jar without the magic first line, and browser
    /// extensions frequently omit it — so put it back if it is missing.
    ///
    /// Testing for the magic header specifically (rather than for any leading
    /// '#') matters: a jar whose first line is `#HttpOnly_…` starts with '#'
    /// but is not a header, and yt-dlp would reject the file outright.
    static func normalized(_ text: String) -> String {
        var body = text.replacingOccurrences(of: "\r\n", with: "\n")
        body = body.trimmingCharacters(in: .whitespacesAndNewlines)
        let firstLine = body.split(separator: "\n", maxSplits: 1).first.map(String.init) ?? ""
        let hasMagic = magicHeaders.contains { firstLine.hasPrefix($0) }
        if !hasMagic {
            body = netscapeHeader + "\n" + body
        }
        return body + "\n"
    }

    /// Writes the jar to a private temp file. Returns nil if there is nothing usable.
    static func writeTemporaryJar(_ text: String) throws -> URL? {
        guard parsedCount(in: text) > 0 else { return nil }
        let dir = URL(fileURLWithPath: NSTemporaryDirectory())
            .appendingPathComponent(
                "YTGrab-\(ProcessInfo.processInfo.processIdentifier)-\(UUID().uuidString)",
                isDirectory: true)
        try FileManager.default.createDirectory(at: dir,
                                                withIntermediateDirectories: true,
                                                attributes: [.posixPermissions: 0o700])
        let file = dir.appendingPathComponent("cookies.txt")
        // Create with 0600 up front so the contents are never briefly world-readable.
        guard FileManager.default.createFile(atPath: file.path,
                                             contents: Data(normalized(text).utf8),
                                             attributes: [.posixPermissions: 0o600]) else {
            throw CocoaError(.fileWriteUnknown)
        }
        return file
    }

    /// Overwrites the jar before unlinking it, then removes its directory.
    static func shred(_ file: URL?) {
        guard let file else { return }
        if let handle = try? FileHandle(forWritingTo: file),
           let size = (try? FileManager.default.attributesOfItem(atPath: file.path)[.size]) as? Int,
           size > 0 {
            try? handle.write(contentsOf: Data(repeating: 0, count: size))
            try? handle.synchronize()
            try? handle.close()
        }
        try? FileManager.default.removeItem(at: file.deletingLastPathComponent())
    }

    /// Removes jars orphaned by a crash, force-quit, or logout, none of which
    /// run `applicationWillTerminate`.
    static func sweepStaleJars() {
        let tmp = URL(fileURLWithPath: NSTemporaryDirectory())
        guard let entries = try? FileManager.default.contentsOfDirectory(
            at: tmp, includingPropertiesForKeys: nil) else { return }
        for entry in entries where entry.lastPathComponent.hasPrefix("YTGrab-") {
            let parts = entry.lastPathComponent.split(separator: "-", maxSplits: 2)
            if parts.count == 3, let pid = Int32(parts[1]), processIsRunning(pid) {
                continue
            }
            shred(entry.appendingPathComponent("cookies.txt"))
            try? FileManager.default.removeItem(at: entry)
        }
    }

    private static func processIsRunning(_ pid: Int32) -> Bool {
        guard pid > 0 else { return false }
        if kill(pid, 0) == 0 { return true }
        return errno == EPERM
    }
}
