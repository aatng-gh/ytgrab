import Foundation

/// Finds `yt-dlp` and `ffmpeg`.
///
/// An app launched from Finder inherits a bare PATH (`/usr/bin:/bin:/usr/sbin:/sbin`),
/// so Homebrew and friends have to be looked up explicitly.
enum ToolLocator {

    static let searchDirectories: [String] = [
        "/opt/homebrew/bin",
        "/usr/local/bin",
        "/opt/local/bin",
        NSHomeDirectory() + "/.local/bin",
        NSHomeDirectory() + "/bin",
        "/usr/bin",
        "/bin",
    ]

    /// `Contents/Resources/bin` inside the app bundle, if the build embedded
    /// standalone tools there. A copy of the .app that carries this folder
    /// needs nothing installed on the machine it lands on.
    static var bundledDirectory: URL? {
        guard let resources = Bundle.main.resourceURL else { return nil }
        let dir = resources.appendingPathComponent("bin", isDirectory: true)
        return FileManager.default.fileExists(atPath: dir.path) ? dir : nil
    }

    static func find(_ name: String, override: String = "") -> String? {
        let custom = override.trimmingCharacters(in: .whitespacesAndNewlines)
        if !custom.isEmpty, isUsable(custom) { return custom }

        // The bundled copy is preferred, but only if it can actually run here —
        // the embedded ffmpeg/ffprobe are arm64-only, and silently shadowing a
        // working Homebrew build on an Intel Mac would fail late, at the merge
        // step, after the whole download had already completed.
        if let bundled = bundledDirectory {
            let candidate = bundled.appendingPathComponent(name).path
            if isUsable(candidate) { return candidate }
        }

        for dir in searchDirectories {
            let candidate = dir + "/" + name
            if isUsable(candidate) { return candidate }
        }
        return findViaLoginShell(name)
    }

    /// Last resort: ask the user's login shell, which sources their profile and
    /// therefore knows about pyenv/asdf/conda style installs.
    ///
    /// Bounded, because a profile that is slow (nvm, conda init) or that blocks
    /// on a prompt would otherwise hang whoever called us.
    private static func findViaLoginShell(_ name: String) -> String? {
        let shell = ProcessInfo.processInfo.environment["SHELL"] ?? "/bin/zsh"
        let process = Process()
        process.executableURL = URL(fileURLWithPath: shell)
        process.arguments = ["-lc", "command -v \(name)"]
        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = FileHandle.nullDevice
        process.standardInput = FileHandle.nullDevice
        do {
            try process.run()
        } catch {
            return nil
        }

        let deadline = Date().addingTimeInterval(3)
        while process.isRunning && Date() < deadline {
            usleep(20_000)
        }
        if process.isRunning {
            process.terminate()
            return nil
        }

        let data = pipe.fileHandleForReading.readDataToEndOfFile()
        let path = String(decoding: data, as: UTF8.self)
            .trimmingCharacters(in: .whitespacesAndNewlines)
        return isUsable(path) ? path : nil
    }

    /// Executable, and runnable on *this* CPU.
    static func isUsable(_ path: String) -> Bool {
        isExecutable(path) && isArchitectureCompatible(path)
    }

    static func isExecutable(_ path: String) -> Bool {
        guard !path.isEmpty else { return false }
        var isDir: ObjCBool = false
        return FileManager.default.fileExists(atPath: path, isDirectory: &isDir)
            && !isDir.boolValue
            && FileManager.default.isExecutableFile(atPath: path)
    }

    // MARK: - Architecture

    private static let cpuTypeX86_64: UInt32 = 0x0100_0007
    private static let cpuTypeARM64: UInt32  = 0x0100_000C

    private static var hostCPUType: UInt32 {
        #if arch(arm64)
        return cpuTypeARM64
        #else
        return cpuTypeX86_64
        #endif
    }

    /// True if `path` can run on this CPU.
    ///
    /// Scripts (`#!/usr/bin/env python3`) and anything that is not a Mach-O are
    /// treated as compatible — only a Mach-O that lacks our slice is rejected.
    /// Rosetta means an x86_64 binary is still usable on Apple Silicon, so only
    /// the reverse (arm64-only on Intel) is a hard no.
    static func isArchitectureCompatible(_ path: String) -> Bool {
        guard let handle = FileHandle(forReadingAtPath: path) else { return false }
        defer { try? handle.close() }
        guard let head = try? handle.read(upToCount: 4096), head.count >= 8 else { return true }

        func be32(_ offset: Int) -> UInt32 {
            (UInt32(head[offset]) << 24) | (UInt32(head[offset + 1]) << 16)
                | (UInt32(head[offset + 2]) << 8) | UInt32(head[offset + 3])
        }
        func le32(_ offset: Int) -> UInt32 {
            (UInt32(head[offset + 3]) << 24) | (UInt32(head[offset + 2]) << 16)
                | (UInt32(head[offset + 1]) << 8) | UInt32(head[offset])
        }

        let magic = be32(0)

        // Universal binary: walk the slice table (big-endian on disk).
        if magic == 0xCAFE_BABE || magic == 0xCAFE_BABF {
            let count = Int(be32(4))
            let entrySize = (magic == 0xCAFE_BABF) ? 32 : 20
            var slices: [UInt32] = []
            for i in 0..<min(count, 16) {
                let offset = 8 + i * entrySize
                guard offset + 4 <= head.count else { break }
                slices.append(be32(offset))
            }
            guard !slices.isEmpty else { return true }
            if slices.contains(hostCPUType) { return true }
            #if arch(arm64)
            return slices.contains(cpuTypeX86_64)   // Rosetta can run it
            #else
            return false
            #endif
        }

        // Thin Mach-O: cputype is the word after the magic.
        if magic == 0xFEED_FACF || magic == 0xFEED_FACE {          // big-endian file
            let cpu = be32(4)
            return cpu == hostCPUType || (hostCPUType == cpuTypeARM64 && cpu == cpuTypeX86_64)
        }
        if le32(0) == 0xFEED_FACF || le32(0) == 0xFEED_FACE {      // little-endian file
            let cpu = le32(4)
            return cpu == hostCPUType || (hostCPUType == cpuTypeARM64 && cpu == cpuTypeX86_64)
        }

        return true     // not Mach-O (script, wrapper, …)
    }

    /// PATH handed to yt-dlp so it can locate ffmpeg, ffprobe and friends itself.
    static var childPATH: String {
        let existing = ProcessInfo.processInfo.environment["PATH"] ?? ""
        var dirs = searchDirectories
        // Only put the bundle first if its tools can actually run here.
        if let bundled = bundledDirectory,
           isUsable(bundled.appendingPathComponent("ffmpeg").path) {
            dirs = [bundled.path] + dirs
        }
        return (dirs + existing.split(separator: ":").map(String.init))
            .reduce(into: [String]()) { acc, dir in if !acc.contains(dir) { acc.append(dir) } }
            .joined(separator: ":")
    }
}
